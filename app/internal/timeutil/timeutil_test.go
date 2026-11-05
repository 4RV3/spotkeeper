package timeutil

import (
	"testing"
	"time"
)

func mustUTC(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v.UTC()
}

func TestDayWindow(t *testing.T) {
	start, end, err := DayWindow("2026-10-25")
	if err != nil {
		t.Fatal(err)
	}
	if !start.Equal(mustUTC(t, "2026-10-24T22:00:00Z")) {
		t.Fatalf("start = %v", start)
	}
	if !end.Equal(mustUTC(t, "2026-10-25T23:00:00Z")) {
		t.Fatalf("end = %v", end)
	}
}

func TestDayWindowSpringDST(t *testing.T) {
	start, end, err := DayWindow("2026-03-29")
	if err != nil {
		t.Fatal(err)
	}
	if !start.Equal(mustUTC(t, "2026-03-28T23:00:00Z")) {
		t.Fatalf("start = %v", start)
	}
	if !end.Equal(mustUTC(t, "2026-03-29T22:00:00Z")) {
		t.Fatalf("end = %v", end)
	}
}

func TestBadDay(t *testing.T) {
	if _, _, err := DayWindow("2026-02-30"); err == nil {
		t.Fatal("expected error for 2026-02-30")
	}
}

func TestAddDays(t *testing.T) {
	if got := AddDays("2026-10-24", 28); got != "2026-11-21" {
		t.Fatalf("AddDays = %q", got)
	}
}

func TestWeekday(t *testing.T) {
	if got := Weekday("2026-10-26"); got != 1 {
		t.Fatalf("Weekday(Monday) = %d", got)
	}
	if got := Weekday("2026-10-25"); got != 7 {
		t.Fatalf("Weekday(Sunday) = %d", got)
	}
}

func TestToday(t *testing.T) {
	now := mustUTC(t, "2026-10-24T22:30:00Z")
	if got := Today(now); got != "2026-10-25" {
		t.Fatalf("Today = %q", got)
	}
}

func TestAt(t *testing.T) {
	got, err := At("2026-10-24", "11:00")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(mustUTC(t, "2026-10-24T09:00:00Z")) {
		t.Fatalf("At = %v", got)
	}
}

func TestFormatAPI(t *testing.T) {
	got := FormatAPI(mustUTC(t, "2026-10-24T22:00:00Z"))
	if got != "2026-10-24T22:00:00.000Z" {
		t.Fatalf("FormatAPI = %q", got)
	}
}
