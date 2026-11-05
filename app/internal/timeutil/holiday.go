package timeutil

import "time"

// fixedHolidays are the Polish statutory days off with a fixed date ("MM-DD").
var fixedHolidays = map[string]string{
	"01-01": "Nowy Rok",
	"01-06": "Trzech Króli",
	"05-01": "Święto Pracy",
	"05-03": "Święto Konstytucji 3 Maja",
	"08-15": "Wniebowzięcie NMP",
	"11-01": "Wszystkich Świętych",
	"11-11": "Święto Niepodległości",
	"12-24": "Wigilia",
	"12-25": "Boże Narodzenie",
	"12-26": "Drugi dzień Bożego Narodzenia",
}

// easterHolidays are the movable days off, as days after Easter Sunday.
var easterHolidays = map[int]string{
	0:  "Wielkanoc",
	1:  "Poniedziałek Wielkanocny",
	49: "Zielone Świątki",
	60: "Boże Ciało",
}

// Holiday returns the name of the Polish public holiday on day, or "" for a
// regular day (or an invalid one).
func Holiday(day string) string {
	t, err := parseDay(day)
	if err != nil {
		return ""
	}
	if name, ok := fixedHolidays[day[5:]]; ok {
		// Wigilia became a statutory day off in 2025.
		if day[5:] == "12-24" && t.Year() < 2025 {
			return ""
		}
		return name
	}
	e := easter(t.Year())
	offset := int(t.Sub(e).Hours()+12) / 24 // +12h absorbs DST shifts
	return easterHolidays[offset]
}

// DayOff reports whether day is a weekend or a public holiday, with the
// holiday name ("" for a plain weekend).
func DayOff(day string) (bool, string) {
	name := Holiday(day)
	return name != "" || Weekday(day) >= 6, name
}

// easter returns Easter Sunday (Gregorian) as Warsaw midnight, using the
// anonymous Gregorian (Meeus/Jones/Butcher) algorithm.
func easter(year int) time.Time {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	dom := (h+l-7*m+114)%31 + 1
	return time.Date(year, time.Month(month), dom, 0, 0, 0, 0, Warsaw)
}
