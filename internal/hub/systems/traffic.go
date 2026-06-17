package systems

import (
	"fmt"
	"time"

	"github.com/henrygd/beszel/internal/alerts"
	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const bytesPerGiB = 1024 * 1024 * 1024

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

// sumNetBytes totals the cumulative sent/recv bytes across all reported network
// interfaces (Stats.NetworkInterfaces index 2 = sent, 3 = recv).
func sumNetBytes(stats *system.Stats) (sent, recv uint64) {
	for _, ni := range stats.NetworkInterfaces {
		sent += ni[2]
		recv += ni[3]
	}
	return sent, recv
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

	currentSent, currentRecv := sumNetBytes(&data.Stats)

	rec, err := hub.FindFirstRecordByFilter("traffic_monthly",
		"system = {:system} && period = {:period}",
		dbx.Params{"system": sys.Id, "period": period},
	)
	if err != nil {
		// no row yet for this period: seed the baseline without counting a delta
		// (avoids attributing the full since-boot cumulative total to the period)
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
		rec.Set("last_up", currentSent)
		rec.Set("last_down", currentRecv)
		rec.Set("notified", false)
		if saveErr := hub.SaveNoValidate(rec); saveErr != nil {
			hub.Logger().Error("traffic_monthly: seed save", "err", saveErr)
		}
		return
	}

	lastSent := uint64(rec.GetInt("last_up"))
	lastRecv := uint64(rec.GetInt("last_down"))

	// counter reset / reboot: if current < last, treat current as the delta
	var deltaSent, deltaRecv uint64
	if currentSent >= lastSent {
		deltaSent = currentSent - lastSent
	} else {
		deltaSent = currentSent
	}
	if currentRecv >= lastRecv {
		deltaRecv = currentRecv - lastRecv
	} else {
		deltaRecv = currentRecv
	}

	bytesUp := uint64(rec.GetInt("bytes_up")) + deltaSent
	bytesDown := uint64(rec.GetInt("bytes_down")) + deltaRecv
	notified := rec.GetBool("notified")

	rec.Set("bytes_up", bytesUp)
	rec.Set("bytes_down", bytesDown)
	rec.Set("last_up", currentSent)
	rec.Set("last_down", currentRecv)

	// quota check (GiB). 0 means unlimited.
	if quotaBytes := uint64(quotaGiB) * bytesPerGiB; quotaBytes > 0 && !notified && (bytesUp+bytesDown) >= quotaBytes {
		rec.Set("notified", true)
		sys.notifyQuotaExceeded(systemRecord, quotaGiB, bytesUp+bytesDown, period)
	}

	if saveErr := hub.SaveNoValidate(rec); saveErr != nil {
		hub.Logger().Error("traffic_monthly: save", "err", saveErr)
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
