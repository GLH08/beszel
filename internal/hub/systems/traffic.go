package systems

import (
	"fmt"
	"time"

	"github.com/henrygd/beszel/internal/alerts"
	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/henrygd/beszel/internal/hub/utils"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const bytesPerGiB = 1024 * 1024 * 1024

// trafficWarnFraction is the quota-usage fraction that triggers an early
// warning (80%).
const trafficWarnFraction = 0.8

// shouldWarnQuota reports whether usage has crossed the early-warning
// threshold (80% of quota) and a warning is still owed this cycle.
func shouldWarnQuota(quotaBytes, usedBytes uint64, warned bool) bool {
	return quotaBytes > 0 && !warned && float64(usedBytes) >= float64(quotaBytes)*trafficWarnFraction
}

// TrafficSummary is the per-system current billing-cycle traffic snapshot
// returned by the /api/beszel/traffic endpoint.
type TrafficSummary struct {
	Period     string `json:"period"`      // billing-cycle start date, "2006-01-02"
	BytesUp    uint64 `json:"bytes_up"`    // cumulative upload bytes this period
	BytesDown  uint64 `json:"bytes_down"`  // cumulative download bytes this period
	QuotaGiB   int    `json:"quota_gib"`   // 0 = unlimited
	ResetDay   int    `json:"reset_day"`   // 1-28 (1 = natural month)
	HasHistory bool   `json:"has_history"` // any prior cycle accumulated bytes (keep card visible across resets)
}

// trafficPeriod returns the billing-cycle period key (start date "2006-01-02")
// for t given a reset day. resetDay 1 (or out of 1-28) means natural month.
// Days are clamped to 1-28 to avoid February edge cases.
func trafficPeriod(t time.Time, resetDay int) string {
	if resetDay < 1 || resetDay > 28 {
		resetDay = 1
	}
	y, m, d := t.Date()
	if d < resetDay {
		// before this month's reset day -> the period started in the previous month
		prev := t.AddDate(0, -1, 0)
		y, m, _ = prev.Date()
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, int(m), resetDay)
}

// computeNetDelta computes per-interface byte deltas from cumulative counters,
// handling per-interface counter resets and newly-appeared/removed interfaces.
// Returns the summed deltas and the updated baseline. A newly-appearing
// interface is seeded (no delta this cycle) so its full since-boot total is
// not attributed as fresh traffic. A disappeared interface is dropped from the
// baseline. On a per-interface counter reset (reboot), the new smaller value
// is taken as the delta.
func computeNetDelta(current map[string][4]uint64, last map[string][2]uint64) (deltaSent, deltaRecv uint64, updated map[string][2]uint64) {
	if last == nil {
		last = map[string][2]uint64{}
	}
	for name, ni := range current {
		curSent, curRecv := ni[2], ni[3]
		prev, ok := last[name]
		if !ok {
			// newly appeared: seed baseline, skip delta this cycle
			last[name] = [2]uint64{curSent, curRecv}
			continue
		}
		if curSent >= prev[0] {
			deltaSent += curSent - prev[0]
		} else {
			deltaSent += curSent // counter reset / reboot
		}
		if curRecv >= prev[1] {
			deltaRecv += curRecv - prev[1]
		} else {
			deltaRecv += curRecv
		}
		last[name] = [2]uint64{curSent, curRecv}
	}
	for name := range last {
		if _, ok := current[name]; !ok {
			delete(last, name) // interface disappeared
		}
	}
	return deltaSent, deltaRecv, last
}

// updateTrafficMonthly accumulates per-billing-cycle traffic for the system from
// the agent's per-interface cumulative byte counters and fires a one-shot
// notification when the configured quota is exceeded. It is called once per
// update cycle (60s) after the regular records are written.
func (sys *System) updateTrafficMonthly(systemRecord *core.Record, data *system.CombinedData) {
	hub := sys.manager.hub
	now := time.Now()

	resetDay := int(systemRecord.GetInt("traffic_reset_day"))
	quotaGiB := systemRecord.GetInt("traffic_quota")
	period := trafficPeriod(now, resetDay)

	// Per-interface delta from cumulative counters (in-memory baseline). This is
	// correct under per-interface counter resets, NIC add/remove — the previous
	// sum-then-compare approach overcounted in those cases.
	deltaSent, deltaRecv, lastIfaces := computeNetDelta(data.Stats.NetworkInterfaces, sys.lastNetIfaces)
	sys.lastNetIfaces = lastIfaces
	if deltaSent == 0 && deltaRecv == 0 {
		// nothing to accumulate this cycle (also covers the first call, where
		// every interface is seeded with no delta)
		return
	}

	rec, err := hub.FindFirstRecordByFilter("traffic_monthly",
		"system = {:system} && period = {:period}",
		dbx.Params{"system": sys.Id, "period": period},
	)
	if err != nil {
		// no row yet for this period: create one seeded at zero
		col, colErr := hub.FindCachedCollectionByNameOrId("traffic_monthly")
		if colErr != nil {
			hub.Logger().Error("traffic_monthly: find collection", "err", colErr)
			return
		}
		rec = core.NewRecord(col)
		rec.Set("system", sys.Id)
		rec.Set("period", period)
		rec.Set("bytes_up", 0)
		rec.Set("bytes_down", 0)
		rec.Set("notified", false)
	}

	bytesUp := uint64(rec.GetInt("bytes_up")) + deltaSent
	bytesDown := uint64(rec.GetInt("bytes_down")) + deltaRecv
	notified := rec.GetBool("notified")

	rec.Set("bytes_up", bytesUp)
	rec.Set("bytes_down", bytesDown)

	used := bytesUp + bytesDown
	quotaBytes := uint64(quotaGiB) * bytesPerGiB

	// 80% early warning: one-shot per cycle, tracked in memory on the system.
	// Reset when the billing period changes.
	if sys.trafficPeriod != period {
		sys.trafficPeriod = period
		sys.trafficWarned = false
	}
	if shouldWarnQuota(quotaBytes, used, sys.trafficWarned) {
		sys.trafficWarned = true
		sys.notifyQuotaWarning(systemRecord, quotaGiB, used, period)
	}

	// quota exceeded check (GiB). 0 means unlimited. `notified` is persisted so
	// it does not re-fire after a hub restart.
	if quotaBytes > 0 && !notified && used >= quotaBytes {
		rec.Set("notified", true)
		sys.notifyQuotaExceeded(systemRecord, quotaGiB, used, period)
	}

	if saveErr := hub.SaveNoValidate(rec); saveErr != nil {
		hub.Logger().Error("traffic_monthly: save", "err", saveErr)
	}
}

// notifyQuotaWarning sends a one-shot early-warning notification when usage
// approaches the quota (80%).
func (sys *System) notifyQuotaWarning(systemRecord *core.Record, quotaGiB int, usedBytes uint64, period string) {
	hub := sys.manager.hub
	systemName := systemRecord.GetString("name")
	usedGiB := float64(usedBytes) / bytesPerGiB
	link := hub.MakeLink("system", sys.Id)
	message := fmt.Sprintf(
		"System %q has reached 80%% of its monthly traffic quota: %.1f GiB used of %d GiB (cycle starting %s).",
		systemName, usedGiB, quotaGiB, period,
	)
	for _, userID := range systemRecord.GetStringSlice("users") {
		_ = hub.SendAlert(alerts.AlertMessageData{
			UserID:   userID,
			SystemID: sys.Id,
			Title:    "Traffic quota warning",
			Message:  message,
			Link:     link,
			LinkText: "View system",
		})
	}
}

// notifyQuotaExceeded sends a one-shot notification to every user of the system.
func (sys *System) notifyQuotaExceeded(systemRecord *core.Record, quotaGiB int, usedBytes uint64, period string) {
	hub := sys.manager.hub
	systemName := systemRecord.GetString("name")
	usedGiB := float64(usedBytes) / bytesPerGiB
	link := hub.MakeLink("system", sys.Id)
	message := fmt.Sprintf(
		"System %q has exceeded its monthly traffic quota: %.1f GiB used of %d GiB (cycle starting %s).",
		systemName, usedGiB, quotaGiB, period,
	)
	for _, userID := range systemRecord.GetStringSlice("users") {
		_ = hub.SendAlert(alerts.AlertMessageData{
			UserID:   userID,
			SystemID: sys.Id,
			Title:    "Traffic quota exceeded",
			Message:  message,
			Link:     link,
			LinkText: "View system",
		})
	}
}

// TrafficSummariesForUser returns a map of systemId -> TrafficSummary for every
// system the given user can see (honors SHARE_ALL_SYSTEMS). Used by the home
// page's Monthly Traffic column via /api/beszel/traffic/all so the client can
// fetch all summaries in one request instead of one per system.
func (sm *SystemManager) TrafficSummariesForUser(app core.App, user *core.Record) (map[string]*TrafficSummary, error) {
	hub := sm.hub
	shareAll := false
	if v, _ := utils.GetEnv("SHARE_ALL_SYSTEMS"); v == "true" {
		shareAll = true
	}
	var systemIDs []string
	if shareAll {
		records, err := app.FindRecordsByFilter("systems", "", "", 0, 0, nil)
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			systemIDs = append(systemIDs, r.Id)
		}
	} else {
		if user == nil {
			return map[string]*TrafficSummary{}, nil
		}
		// Select the system ids whose users array contains this user (same approach
		// as System.HasUser, avoiding PocketBase relation-filter syntax quirks).
		rows, err := app.DB().Select("id").From("systems").Where(dbx.Like("users", user.Id).Match(true, true)).Rows()
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			systemIDs = append(systemIDs, id)
		}
		rows.Close()
	}
	out := make(map[string]*TrafficSummary, len(systemIDs))
	for _, id := range systemIDs {
		sys := &System{Id: id, manager: sm}
		if summary, err := sys.TrafficSummary(); err == nil {
			out[id] = summary
		} else {
			hub.Logger().Debug("traffic summary for system", "system", id, "err", err)
		}
	}
	return out, nil
}

// TrafficSummary returns the current billing-cycle traffic snapshot for the system.
func (sys *System) TrafficSummary() (*TrafficSummary, error) {
	hub := sys.manager.hub
	rec, err := hub.FindRecordById("systems", sys.Id)
	if err != nil {
		return nil, err
	}
	resetDay := int(rec.GetInt("traffic_reset_day"))
	period := trafficPeriod(time.Now(), resetDay)
	s := &TrafficSummary{
		Period:   period,
		QuotaGiB: rec.GetInt("traffic_quota"),
		ResetDay: resetDay,
	}
	if trec, err := hub.FindFirstRecordByFilter("traffic_monthly",
		"system = {:system} && period = {:period}",
		dbx.Params{"system": sys.Id, "period": period}); err == nil {
		s.BytesUp = uint64(trec.GetInt("bytes_up"))
		s.BytesDown = uint64(trec.GetInt("bytes_down"))
	}
	// Keep the card visible across cycle resets for systems that have ever
	// transferred traffic: a freshly-rolled period has 0 bytes, which would
	// otherwise hide the card on unlimited-quota systems.
	if hrec, err := hub.FindFirstRecordByFilter("traffic_monthly",
		"system = {:system} && (bytes_up > 0 || bytes_down > 0)",
		dbx.Params{"system": sys.Id}); err == nil && hrec != nil {
		s.HasHistory = true
	}
	return s, nil
}
