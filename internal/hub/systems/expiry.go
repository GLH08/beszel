package systems

import (
	"errors"
	"time"

	"github.com/pocketbase/pocketbase/tools/types"
)

// computeRenewal returns the next expiry date after advancing one cycle.
// If end is already in the past, renewal starts from now (so the result is a
// future date). cycle "year" adds a year; anything else (incl. "") adds a month.
func computeRenewal(end time.Time, cycle string, now time.Time) time.Time {
	base := end
	if base.Before(now) {
		base = now
	}
	if cycle == "year" {
		return base.AddDate(1, 0, 0)
	}
	return base.AddDate(0, 1, 0)
}

// Renew advances the system's expire_end by one renewal cycle and persists it.
// Returns the new expiry time. Returns an error if the system has no fixed
// expiry (expire_type != "fixed" or no expire_end).
func (sys *System) Renew() (time.Time, error) {
	record, err := sys.getRecord(sys.manager.hub)
	if err != nil {
		return time.Time{}, err
	}
	if record.GetString("expire_type") != "fixed" {
		return time.Time{}, errors.New("system has no fixed expiry date")
	}
	end := record.GetDateTime("expire_end").Time()
	if end.IsZero() {
		return time.Time{}, errors.New("system has no expiry date")
	}
	cycle := record.GetString("renew_cycle")
	newEnd := computeRenewal(end, cycle, time.Now())
	dt, err := types.ParseDateTime(newEnd)
	if err != nil {
		return time.Time{}, err
	}
	record.Set("expire_end", dt)
	if err := sys.manager.hub.SaveNoValidate(record); err != nil {
		return time.Time{}, err
	}
	return newEnd, nil
}
