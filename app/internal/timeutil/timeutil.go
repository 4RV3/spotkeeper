// Package timeutil holds the Europe/Warsaw day logic shared by the app (spec §1).
package timeutil

import (
	"fmt"
	"time"
	_ "time/tzdata"
)

const (
	dayLayout = "2006-01-02"
	apiLayout = "2006-01-02T15:04:05.000Z"
)

// Warsaw is the Europe/Warsaw location used for all day logic.
var Warsaw = mustLoad("Europe/Warsaw")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// parseDay parses a YYYY-MM-DD string as a local Warsaw midnight and rejects
// dates that do not round-trip (for example 2026-02-30).
func parseDay(day string) (time.Time, error) {
	t, err := time.ParseInLocation(dayLayout, day, Warsaw)
	if err != nil {
		return time.Time{}, err
	}
	if t.Format(dayLayout) != day {
		return time.Time{}, fmt.Errorf("timeutil: invalid day %q", day)
	}
	return t, nil
}

// DayWindow returns the UTC instants of local Warsaw midnight at the start of
// day and at the start of the following day.
func DayWindow(day string) (time.Time, time.Time, error) {
	start, err := parseDay(day)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end := start.AddDate(0, 0, 1)
	return start.UTC(), end.UTC(), nil
}

// AddDays returns the Warsaw day n days after day. It returns "" if day is invalid.
func AddDays(day string, n int) string {
	t, err := parseDay(day)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, n).Format(dayLayout)
}

// Today returns the Warsaw calendar day for now.
func Today(now time.Time) string {
	return now.In(Warsaw).Format(dayLayout)
}

// Weekday returns the ISO weekday of day (Monday=1 ... Sunday=7).
// It returns 0 if day is invalid.
func Weekday(day string) int {
	t, err := parseDay(day)
	if err != nil {
		return 0
	}
	if wd := int(t.Weekday()); wd != 0 {
		return wd
	}
	return 7
}

// At returns the instant of hhmm (HH:MM, local Warsaw time) on day, in UTC.
func At(day string, hhmm string) (time.Time, error) {
	d, err := parseDay(day)
	if err != nil {
		return time.Time{}, err
	}
	hm, err := time.Parse("15:04", hhmm)
	if err != nil {
		return time.Time{}, err
	}
	t := time.Date(d.Year(), d.Month(), d.Day(), hm.Hour(), hm.Minute(), 0, 0, Warsaw)
	return t.UTC(), nil
}

// FormatAPI formats t as the API timestamp 2006-01-02T15:04:05.000Z in UTC.
func FormatAPI(t time.Time) string {
	return t.UTC().Format(apiLayout)
}
