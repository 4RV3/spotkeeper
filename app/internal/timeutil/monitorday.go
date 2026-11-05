package timeutil

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// LastMonitorDay is the furthest day a monitor may target at now: spots for
// today+ahead are only published at the release time (HH:MM, Warsaw), so before
// then the limit is one day earlier.
func LastMonitorDay(now time.Time, release string, ahead int) string {
	today := Today(now)
	if at, err := At(today, release); err == nil && now.Before(at) {
		return AddDays(today, ahead-1)
	}
	return AddDays(today, ahead)
}

var plDate = regexp.MustCompile(`^([0-9]{1,2})\.([0-9]{1,2})\.([0-9]{4})$`)

// ParsePLDate turns a Polish d.m.yyyy / dd.mm.yyyy date into YYYY-MM-DD, or "" if invalid.
func ParsePLDate(s string) string {
	m := plDate.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	d, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	day := fmt.Sprintf("%s-%02d-%02d", m[3], mo, d)
	if _, err := parseDay(day); err != nil {
		return ""
	}
	return day
}

// FormatPLDate formats YYYY-MM-DD as dd.mm.yyyy.
func FormatPLDate(day string) string {
	if len(day) != 10 {
		return day
	}
	return day[8:] + "." + day[5:7] + "." + day[:4]
}
