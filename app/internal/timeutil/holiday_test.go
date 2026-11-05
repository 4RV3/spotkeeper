package timeutil

import "testing"

func TestHoliday(t *testing.T) {
	for _, tc := range []struct{ day, want string }{
		{"2026-01-01", "Nowy Rok"},
		{"2026-01-06", "Trzech Króli"},
		{"2026-04-05", "Wielkanoc"},
		{"2026-04-06", "Poniedziałek Wielkanocny"},
		{"2026-05-01", "Święto Pracy"},
		{"2026-05-03", "Święto Konstytucji 3 Maja"},
		{"2026-05-24", "Zielone Świątki"},
		{"2026-06-04", "Boże Ciało"},
		{"2026-08-15", "Wniebowzięcie NMP"},
		{"2026-11-01", "Wszystkich Świętych"},
		{"2026-11-11", "Święto Niepodległości"},
		{"2026-12-24", "Wigilia"},
		{"2026-12-25", "Boże Narodzenie"},
		{"2026-12-26", "Drugi dzień Bożego Narodzenia"},
		{"2027-03-28", "Wielkanoc"},
		{"2027-03-29", "Poniedziałek Wielkanocny"},
		{"2027-05-16", "Zielone Świątki"},
		{"2027-05-27", "Boże Ciało"},
		{"2024-12-24", ""}, // Wigilia is a day off only from 2025
		{"2026-11-02", ""},
		{"2026-04-07", ""},
		{"2026-02-30", ""},
		{"bad", ""},
	} {
		if got := Holiday(tc.day); got != tc.want {
			t.Errorf("Holiday(%s) = %q, want %q", tc.day, got, tc.want)
		}
	}
}

func TestEaster(t *testing.T) {
	for year, want := range map[int]string{
		2024: "2024-03-31", 2025: "2025-04-20", 2026: "2026-04-05",
		2027: "2027-03-28", 2038: "2038-04-25", 2285: "2285-03-22",
	} {
		if got := easter(year).Format(dayLayout); got != want {
			t.Errorf("easter(%d) = %s, want %s", year, got, want)
		}
	}
}

func TestDayOff(t *testing.T) {
	for _, tc := range []struct {
		day  string
		off  bool
		name string
	}{
		{"2026-10-24", true, ""},                    // Saturday
		{"2026-10-25", true, ""},                    // Sunday
		{"2026-10-26", false, ""},                   // Monday
		{"2026-11-01", true, "Wszystkich Świętych"}, // Sunday and a holiday
		{"2026-11-11", true, "Święto Niepodległości"},
		{"bad", false, ""},
	} {
		off, name := DayOff(tc.day)
		if off != tc.off || name != tc.name {
			t.Errorf("DayOff(%s) = %v,%q, want %v,%q", tc.day, off, name, tc.off, tc.name)
		}
	}
}
