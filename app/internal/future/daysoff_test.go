package future

import (
	"context"
	"testing"
	"time"

	"spotkeeper/internal/model"
	"spotkeeper/internal/timeutil"
)

// The 11:00 run skips a user's days off, and public holidays for users who opted in;
// a skipped day is recorded (status skipped, reason in the detail) without a notification.
func TestRunDaySkipsDaysOff(t *testing.T) {
	// release 2026-10-14 11:00 Warsaw -> target day 2026-11-11 (Święto Niepodległości)
	release, err := timeutil.At("2026-10-14", "11:00")
	if err != nil {
		t.Fatal(err)
	}
	us := []model.User{
		{Login: "vac", Active: true, SkipHolidays: false},
		{Login: "hol", Active: true, SkipHolidays: true},
		{Login: "work", Active: true, SkipHolidays: false},
	}
	st := &fakeStore{users: us, off: map[string]bool{"vac": true}}
	r, n := &fakeRun{}, &fakeNotify{}
	f := newF(st, fakePrep{}, r, n)
	f.Sleep = func(context.Context, time.Duration) error { return nil }
	if err := f.RunDay(context.Background(), release); err != nil {
		t.Fatal(err)
	}
	if len(r.jobs) != 1 || r.jobs[0].Login != "work" || r.jobs[0].Day != "2026-11-11" {
		t.Fatalf("jobs = %+v", r.jobs)
	}
	if tk, _ := st.last("vac"); tk.Status != model.StatusSkipped || tk.Detail != "urlop" || tk.Day != "2026-11-11" || tk.Kind != model.KindFuture {
		t.Fatalf("vacation task %+v", tk)
	}
	if tk, _ := st.last("hol"); tk.Status != model.StatusSkipped || tk.Detail != "święto: Święto Niepodległości" {
		t.Fatalf("holiday task %+v", tk)
	}
	if len(n.got) != 1 || n.got[0] != "work:"+model.StatusPaid {
		t.Fatalf("notifications %v", n.got)
	}
}
