package future

import (
	"context"
	"strings"
	"testing"
	"time"

	"spotkeeper/internal/model"
	"spotkeeper/internal/timeutil"
)

// After each run with scheduled users the admin summary gets the counts and one line per
// user who did not end up paid (sorted).
func TestRunDaySummary(t *testing.T) {
	release, _ := timeutil.At("2026-10-14", "11:00") // target 2026-11-11, a holiday
	us := []model.User{
		{Login: "ok", Active: true},
		{Login: "bad", Active: true},
		{Login: "vac", Active: true},
		{Login: "hol", Active: true, SkipHolidays: true},
	}
	st := &fakeStore{users: us, off: map[string]bool{"vac": true}}
	f := newF(st, fakePrep{fail: "bad"}, &fakeRun{}, &fakeNotify{})
	f.Sleep = func(context.Context, time.Duration) error { return nil }
	var got []RunSummary
	f.Summary = func(_ context.Context, s RunSummary) { got = append(got, s) }
	if err := f.RunDay(context.Background(), release); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("summaries %d", len(got))
	}
	s := got[0]
	if s.Day != "2026-11-11" || s.Paid != 1 || s.Failed != 1 || s.Skipped != 2 || s.Unconfirmed != 0 {
		t.Fatalf("summary %+v", s)
	}
	want := []string{"bad — nieudana: boom", "hol — pominięto: święto: Święto Niepodległości", "vac — pominięto: urlop"}
	if strings.Join(s.Problems, "|") != strings.Join(want, "|") {
		t.Fatalf("problems %q", s.Problems)
	}

	// nobody scheduled: no report
	got = nil
	f = newF(&fakeStore{}, fakePrep{}, &fakeRun{}, &fakeNotify{})
	f.Summary = func(_ context.Context, s RunSummary) { got = append(got, s) }
	f.Sleep = func(context.Context, time.Duration) error { return nil }
	f.RunDay(context.Background(), release)
	if len(got) != 0 {
		t.Fatalf("summary without users: %+v", got)
	}
}
