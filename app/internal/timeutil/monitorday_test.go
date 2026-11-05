package timeutil

import (
	"testing"
	"time"
)

func TestLastMonitorDay(t *testing.T) {
	for _, tc := range []struct {
		now  time.Time
		want string
	}{
		{time.Date(2026, 10, 24, 8, 59, 0, 0, time.UTC), "2026-11-20"}, // 10:59 Warsaw: before the release
		{time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC), "2026-11-21"},  // 11:00 Warsaw
		{time.Date(2026, 10, 24, 21, 0, 0, 0, time.UTC), "2026-11-21"},
	} {
		if got := LastMonitorDay(tc.now, "11:00", 28); got != tc.want {
			t.Errorf("LastMonitorDay(%v) = %s, want %s", tc.now, got, tc.want)
		}
	}
}

func TestParsePLDate(t *testing.T) {
	for in, want := range map[string]string{
		"12.11.2026": "2026-11-12", "1.2.2027": "2027-02-01", "01.02.2027": "2027-02-01",
		"30.02.2026": "", "12.11.26": "", "2026-11-12": "", "12/11/2026": "", "": "", "12.13.2026": "",
	} {
		if got := ParsePLDate(in); got != want {
			t.Errorf("ParsePLDate(%q) = %q, want %q", in, got, want)
		}
	}
	if got := FormatPLDate("2026-11-02"); got != "02.11.2026" {
		t.Errorf("FormatPLDate = %q", got)
	}
}
