package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"spotkeeper/internal/model"
)

// cellHTML returns the markup of one calendar cell (up to the next cell or the legend).
func cellHTML(body, day string) string {
	i := strings.Index(body, `data-day="`+day+`"`)
	if i < 0 {
		return ""
	}
	rest := body[i+1:]
	j := len(rest)
	for _, stop := range []string{"data-day=", `<div class="legend">`} {
		if k := strings.Index(rest, stop); k >= 0 && k < j {
			j = k
		}
	}
	return body[i : i+1+j]
}

func cellDays(body string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`<div class="cell[^"]*" data-day="([0-9-]+)"`).FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// The calendar shows one month at a time, Monday-first, defaulting to the current month.
func TestCalendarDefaultsToCurrentMonth(t *testing.T) {
	e := newDEnv(t) // now = 2026-10-24
	b := get(e, "/")
	if !strings.Contains(b, `class="cal-title">październik 2026<`) {
		t.Fatal("missing month title")
	}
	days := cellDays(b)
	if len(days) != 31 || days[0] != "2026-10-01" || days[30] != "2026-10-31" {
		t.Fatalf("october cells = %v", days)
	}
	if strings.Count(b, `class="cell blank"`) != 3 { // 1 Oct 2026 is a Thursday
		t.Fatal("blanks")
	}
	if !strings.Contains(b, `href="/?m=2026-11#calendar"`) {
		t.Fatal("no link to next month")
	}
	// no history yet: nothing before the current month
	if strings.Contains(b, `href="/?m=2026-09#calendar"`) || !strings.Contains(b, `class="cal-nav-btn" aria-disabled="true"`) {
		t.Fatal("previous month should be disabled without history")
	}
	if strings.Contains(b, `>Dziś<`) {
		t.Fatal("today link shown on the current month")
	}
}

// Past months are browsable back to the first task; they show history but no actions.
func TestCalendarHistoryMonth(t *testing.T) {
	e := newDEnv(t)
	e.st.tasks = []model.Task{
		{Day: "2026-08-12", Kind: "future", Status: "paid"},
		{Day: "2026-09-07", Kind: "future", Status: "paid"},
		{Day: "2026-09-09", Kind: "past", Status: "failed"},
	}
	cur := get(e, "/")
	if !strings.Contains(cur, `href="/?m=2026-09#calendar"`) {
		t.Fatal("previous month should be reachable with history")
	}
	b := get(e, "/?m=2026-09")
	if !strings.Contains(b, `class="cal-title">wrzesień 2026<`) || !strings.Contains(b, `href="/?m=2026-08#calendar"`) ||
		!strings.Contains(b, `href="/?m=2026-10#calendar"`) || !strings.Contains(b, `href="/#calendar"`) {
		t.Fatal("september navigation wrong")
	}
	if c := cellHTML(b, "2026-09-07"); !strings.Contains(c, "pill paid") || strings.Contains(c, "badge auto") ||
		strings.Contains(c, "<form") || !regexp.MustCompile(`class="cell[^"]* past[^"]*" data-day="2026-09-07"`).MatchString(b) {
		t.Fatalf("past paid day: %q", c)
	}
	if c := cellHTML(b, "2026-09-09"); !strings.Contains(c, "pill failed") || strings.Contains(c, "<form") {
		t.Fatalf("past failed day: %q", c)
	}
	// the earliest month has no further "previous"
	if a := get(e, "/?m=2026-08"); strings.Contains(a, `href="/?m=2026-07#calendar"`) || !strings.Contains(a, "sierpień 2026") {
		t.Fatal("august should be the first month")
	}
	// before the first task: clamped to the first month
	if a := get(e, "/?m=2020-01"); !strings.Contains(a, `class="cal-title">sierpień 2026<`) {
		t.Fatal("not clamped to the first month")
	}
}

// Next month: the schedule and day-off buttons beyond the furthest opened day.
func TestCalendarNextMonth(t *testing.T) {
	e := newDEnv(t) // 12:00 Warsaw, release passed: furthest day 2026-11-21
	b := get(e, "/?m=2026-11")
	if !strings.Contains(b, `class="cal-title">listopad 2026<`) {
		t.Fatal("title")
	}
	if c := cellHTML(b, "2026-11-23"); !strings.Contains(c, "badge auto") || !strings.Contains(c, `<input type="hidden" name="m" value="2026-11">`) { // Monday beyond the horizon
		t.Fatalf("day beyond horizon: %q", c)
	}
	if c := cellHTML(b, "2026-11-24"); strings.Contains(c, "badge auto") || strings.Contains(c, "<form") {
		t.Fatalf("unscheduled day beyond horizon: %q", c)
	}
	if strings.Contains(b, `class="cell today`) {
		t.Fatal("today marked outside its month")
	}
}

// Navigation is bounded to 12 months ahead; junk falls back to the current month.
func TestCalendarMonthBounds(t *testing.T) {
	e := newDEnv(t)
	last := get(e, "/?m=2027-10")
	if !strings.Contains(last, `class="cal-title">październik 2027<`) || strings.Contains(last, `href="/?m=2027-11#calendar"`) {
		t.Fatal("last month")
	}
	if b := get(e, "/?m=2031-01"); !strings.Contains(b, `class="cal-title">październik 2027<`) {
		t.Fatal("not clamped to 12 months ahead")
	}
	for _, m := range []string{"nope", "2026-13", "2026-1", "2026-10-01"} {
		if b := get(e, "/?m="+url.QueryEscape(m)); !strings.Contains(b, `class="cal-title">październik 2026<`) {
			t.Fatalf("m=%s did not fall back to the current month", m)
		}
	}
}

// Marking or clearing a day off returns to the month it was done from.
func TestDayOffRedirectKeepsMonth(t *testing.T) {
	e := newDEnv(t)
	w := e.do("POST", "/daysoff", url.Values{"day": {"2026-11-23"}, "m": {"2026-11"}})
	if loc := w.Header().Get("Location"); loc != "/?msg=off_added&m=2026-11#calendar" {
		t.Fatalf("add redirect = %q", loc)
	}
	w = e.do("POST", "/daysoff/delete", url.Values{"day": {"2026-11-23"}, "m": {"2026-11"}})
	if loc := w.Header().Get("Location"); !strings.HasSuffix(loc, "&m=2026-11#calendar") {
		t.Fatalf("delete redirect = %q", loc)
	}
	w = e.do("POST", "/daysoff", url.Values{"day": {"2026-11-24"}, "m": {"evil#x"}})
	if loc := w.Header().Get("Location"); loc != "/?msg=off_added" {
		t.Fatalf("bad m leaked into redirect: %q", loc)
	}
}

func TestMonthNav(t *testing.T) {
	for _, tc := range []struct{ m, prev, next string }{
		{"2026-10", "2026-09", "2026-11"},
		{"2026-01", "2025-12", "2026-02"},
		{"2026-12", "2026-11", "2027-01"},
	} {
		if p, n := addMonths(tc.m, -1), addMonths(tc.m, 1); p != tc.prev || n != tc.next {
			t.Errorf("%s: %s %s", tc.m, p, n)
		}
	}
}
