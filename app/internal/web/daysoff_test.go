package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"spotkeeper/internal/model"
)

// now = Sat 2026-10-24 12:00 Warsaw, release passed: furthest opened day 2026-11-21.
// jan: schedule Mon+Wed, active.

func TestDayOffToggle(t *testing.T) {
	e := newDEnv(t)
	b := get(e, "/?m=2026-11")
	c := cellHTML(b, "2026-11-23") // Monday beyond the horizon
	if !strings.Contains(c, `action="/daysoff"`) || !strings.Contains(c, `aria-label="Oznacz urlop 2026-11-23"`) || !strings.Contains(c, "badge auto") {
		t.Fatalf("far scheduled day should offer a day off: %q", c)
	}
	if c := cellHTML(b, "2026-11-20"); strings.Contains(c, "/daysoff") {
		t.Fatalf("days inside the booking horizon have no day-off form: %q", c)
	}
	if c := cellHTML(b, "2026-11-24"); strings.Contains(c, "/daysoff") { // Tuesday, not scheduled
		t.Fatalf("unscheduled day offers a day off: %q", c)
	}

	w := e.do("POST", "/daysoff", url.Values{"day": {"2026-11-23"}, "m": {"2026-11"}})
	if loc := w.Header().Get("Location"); loc != "/?msg=off_added&m=2026-11#calendar" || !e.st.off["jan@example.com"]["2026-11-23"] {
		t.Fatalf("add day off: %q %v", loc, e.st.off)
	}
	c = cellHTML(get(e, "/?m=2026-11"), "2026-11-23")
	if strings.Contains(c, "badge auto") || !strings.Contains(c, "badge vac") ||
		!strings.Contains(c, `action="/daysoff/delete"`) || !strings.Contains(c, `aria-label="Usuń urlop 2026-11-23"`) {
		t.Fatalf("day off cell: %q", c)
	}

	w = e.do("POST", "/daysoff/delete", url.Values{"day": {"2026-11-23"}, "m": {"2026-11"}})
	if loc := w.Header().Get("Location"); loc != "/?msg=off_removed&m=2026-11#calendar" || e.st.off["jan@example.com"]["2026-11-23"] {
		t.Fatalf("remove day off: %q", loc)
	}
}

func TestDayOffRange(t *testing.T) {
	e := newDEnv(t)
	for _, d := range []string{"2026-11-21", "2026-10-23", "2027-12-01", "nope"} {
		w := e.do("POST", "/daysoff", url.Values{"day": {d}})
		if loc := w.Header().Get("Location"); !strings.Contains(loc, "off_range") && !strings.Contains(loc, "off_bad") {
			t.Fatalf("%s accepted: %q", d, loc)
		}
		if e.st.off["jan@example.com"][d] {
			t.Fatalf("%s stored", d)
		}
	}
}

// Holidays lose the "auto" badge when the user skips them; the setting is a switch.
func TestSkipHolidaysSetting(t *testing.T) {
	e := newDEnv(t)
	u := e.st.users["jan@example.com"]
	u.SkipHolidays = true
	e.st.users["jan@example.com"] = u
	b := get(e, "/?m=2027-01")
	if c := cellHTML(b, "2027-01-06"); strings.Contains(c, "badge auto") { // Wednesday, Trzech Króli
		t.Fatalf("skipped holiday still shows auto: %q", c)
	}
	if c := cellHTML(b, "2027-01-13"); !strings.Contains(c, "badge auto") {
		t.Fatalf("ordinary Wednesday lost auto: %q", c)
	}
	if !regexp.MustCompile(`<input type="checkbox" name="skip_holidays" value="1" checked>`).MatchString(b) {
		t.Fatal("switch not checked")
	}

	form := url.Values{"plate": {"WX1"}, "level": {"podziemny"}, "ip": {"1.2.3.4"}, "day": {"1", "3"}, "active": {"1"}}
	e.do("POST", "/settings", form)
	if e.st.users["jan@example.com"].SkipHolidays {
		t.Fatal("unchecked switch kept the flag")
	}
	if c := cellHTML(get(e, "/?m=2027-01"), "2027-01-06"); !strings.Contains(c, "badge auto") {
		t.Fatalf("holiday should be booked when not skipped: %q", c)
	}
	form.Set("skip_holidays", "1")
	e.do("POST", "/settings", form)
	if !e.st.users["jan@example.com"].SkipHolidays {
		t.Fatal("checked switch did not set the flag")
	}
}

// The next-run card skips the user's days off.
func TestNextReleaseSkipsDayOff(t *testing.T) {
	e := newDEnv(t)
	if b := get(e, "/"); !strings.Contains(b, "<strong>2026-11-23</strong>") {
		t.Fatal("baseline next target")
	}
	e.st.AddDayOff("jan@example.com", "2026-11-23")
	if b := get(e, "/"); !strings.Contains(b, "<strong>2026-11-25</strong>") {
		t.Fatal("next target should skip the day off")
	}
}

func TestSkippedStatusShown(t *testing.T) {
	e := newDEnv(t)
	e.st.tasks = []model.Task{{Day: "2026-10-21", Kind: "future", Status: model.StatusSkipped, Detail: "urlop"}}
	if c := cellHTML(get(e, "/"), "2026-10-21"); !strings.Contains(c, `pill skipped`) || !strings.Contains(c, "pominięto") {
		t.Fatalf("skipped day: %q", c)
	}
}
