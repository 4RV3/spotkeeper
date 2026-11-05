package store

import (
	"testing"
	"time"

	"spotkeeper/internal/model"
)

func TestListTasksInRangeAndFirstTaskDay(t *testing.T) {
	s := open(t)
	s.UpsertUser(user("a", "1", true))
	s.UpsertUser(user("b", "1", true))
	if d, err := s.FirstTaskDay("a"); err != nil || d != "" {
		t.Fatalf("no tasks: %q %v", d, err)
	}
	now := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	for _, tk := range []model.Task{
		{Login: "a", Day: "2026-08-31", Kind: model.KindFuture, Status: model.StatusPaid},
		{Login: "a", Day: "2026-09-01", Kind: model.KindFuture, Status: model.StatusPaid},
		{Login: "a", Day: "2026-09-15", Kind: model.KindPast, Status: model.StatusFailed},
		{Login: "a", Day: "2026-09-30", Kind: model.KindFuture, Status: model.StatusPaid},
		{Login: "a", Day: "2026-10-01", Kind: model.KindFuture, Status: model.StatusPaid},
		{Login: "b", Day: "2026-07-10", Kind: model.KindFuture, Status: model.StatusPaid},
		{Login: "b", Day: "2026-09-10", Kind: model.KindFuture, Status: model.StatusPaid},
	} {
		tk.UpdatedAt = now
		if err := s.UpsertTask(tk); err != nil {
			t.Fatal(err)
		}
	}
	ts, err := s.ListTasksInRange("a", "2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	var days []string
	for _, x := range ts {
		days = append(days, x.Day)
	}
	if got := len(days); got != 3 || days[0] != "2026-09-01" || days[1] != "2026-09-15" || days[2] != "2026-09-30" {
		t.Fatalf("range = %v", days)
	}
	if d, _ := s.FirstTaskDay("a"); d != "2026-08-31" {
		t.Fatalf("first a = %q", d)
	}
	if d, _ := s.FirstTaskDay("b"); d != "2026-07-10" {
		t.Fatalf("first b = %q", d)
	}
}
