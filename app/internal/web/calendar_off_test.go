package web

import (
	"regexp"
	"strings"
	"testing"
)

// Weekends and Polish public holidays get a coloured frame; holidays also show their name.
func TestCalendarMarksDaysOff(t *testing.T) {
	e := newDEnv(t) // now = 2026-10-24 (Saturday)
	body := e.do("GET", "/", nil).Body.String() + e.do("GET", "/?m=2026-11", nil).Body.String()
	open := func(day string) string {
		return regexp.MustCompile(`<div class="cell[^"]*" data-day="` + day + `"[^>]*>`).FindString(body)
	}
	cell := func(day string) string {
		re := regexp.MustCompile(`(?s)<div class="cell[^"]*" data-day="` + day + `"[^>]*>.*?<div class="badges">`)
		return re.FindString(body)
	}
	for _, day := range []string{"2026-10-24", "2026-10-25", "2026-11-01", "2026-11-11"} {
		if !strings.Contains(open(day), " off") {
			t.Errorf("%s should be marked off: %q", day, open(day))
		}
	}
	for _, day := range []string{"2026-10-26", "2026-11-02", "2026-11-10"} {
		if strings.Contains(open(day), " off") {
			t.Errorf("%s is a working day: %q", day, open(day))
		}
	}
	if c := cell("2026-11-11"); !strings.Contains(c, `class="hol">Święto Niepodległości<`) ||
		!strings.Contains(c, `title="11 listopada 2026 · Święto Niepodległości"`) {
		t.Fatalf("holiday cell lacks its name: %q", c)
	}
	if c := cell("2026-10-25"); strings.Contains(c, `class="hol"`) {
		t.Fatalf("plain weekend should not show a holiday name: %q", c)
	}
	if !strings.Contains(body, "legend-off") {
		t.Fatal("legend lacks the day-off frame")
	}
}
