package store

import "testing"

func TestDaysOff(t *testing.T) {
	s := open(t)
	s.UpsertUser(user("a", "1", true))
	s.UpsertUser(user("b", "1", true))
	for _, d := range []struct{ login, day string }{{"a", "2026-12-28"}, {"a", "2026-12-29"}, {"b", "2026-12-28"}, {"a", "2026-12-28"}} {
		if err := s.AddDayOff(d.login, d.day); err != nil {
			t.Fatal(err)
		}
	}
	days, err := s.ListDaysOff("a")
	if err != nil || len(days) != 2 || days[0] != "2026-12-28" || days[1] != "2026-12-29" {
		t.Fatalf("a days off = %v %v", days, err)
	}
	on, err := s.DaysOffOn("2026-12-28")
	if err != nil || len(on) != 2 || !on["a"] || !on["b"] {
		t.Fatalf("on 28th = %v %v", on, err)
	}
	if err := s.RemoveDayOff("a", "2026-12-28"); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.DaysOffOn("2026-12-28"); len(on) != 1 || !on["b"] {
		t.Fatalf("after remove = %v", on)
	}
	if err := s.DeleteUser("b"); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.DaysOffOn("2026-12-28"); len(on) != 0 {
		t.Fatalf("days off of a deleted user kept: %v", on)
	}
}

// New users skip holidays by default; the flag round-trips and is migrated as on.
func TestSkipHolidaysFlag(t *testing.T) {
	s := open(t)
	u := user("a", "1", true)
	u.SkipHolidays = true
	s.UpsertUser(u)
	if got, _ := s.GetUser("a"); !got.SkipHolidays {
		t.Fatal("flag lost")
	}
	u.SkipHolidays = false
	s.UpsertUser(u)
	if got, _ := s.GetUser("a"); got.SkipHolidays {
		t.Fatal("flag not cleared")
	}
	us, _ := s.UsersForWeekday(1)
	if len(us) != 1 || us[0].SkipHolidays {
		t.Fatalf("weekday users %+v", us)
	}
}
