package web

import (
	"strings"
	"testing"

	"spotkeeper/internal/model"
)

// Badges and status pills carry an icon (phones show only the icon); the legend is
// grouped into day markers and statuses and shows every icon with its meaning.
func TestCalendarIconsAndLegend(t *testing.T) {
	e := newDEnv(t) // now = 2026-10-24; jan: Mon+Wed, active
	e.st.tasks = []model.Task{{Day: "2026-10-26", Kind: "future", Status: model.StatusPaid}}
	b := get(e, "/")
	if c := cellHTML(b, "2026-10-26"); !strings.Contains(c, `<span class="badge auto" title="auto"><svg class="mk"`) ||
		!strings.Contains(c, `<span class="pill paid" title="opłacono"><svg class="mk"`) {
		t.Fatalf("cell without icons: %q", c)
	}
	if c := cellHTML(get(e, "/?m=2026-11"), "2026-11-23"); !strings.Contains(c, `<span class="mini-ic" aria-hidden="true"><svg class="mk"`) {
		t.Fatalf("day-off button without icon: %q", c)
	}

	i := strings.Index(b, `<div class="legend">`)
	if i < 0 {
		t.Fatal("no legend")
	}
	legend := b[i:]
	legend = legend[:strings.Index(legend, "</section>")]
	groups := []string{"<h3>Oznaczenia dni</h3>", "<h3>Status rezerwacji</h3>"}
	last := -1
	for _, g := range groups {
		j := strings.Index(legend, g)
		if j <= last {
			t.Fatalf("legend group %q missing or out of order", g)
		}
		last = j
	}
	for _, cls := range []string{"badge auto", "badge vac", "pill paid", "pill running", "pill unconfirmed", "pill failed", "pill skipped"} {
		if !strings.Contains(legend, `<span class="`+cls+`"><svg class="mk"`) {
			t.Errorf("legend lacks the %s icon", cls)
		}
	}
	for _, w := range []string{"legend-today", "legend-off", "rezerwacja automatyczna"} {
		if !strings.Contains(legend, w) {
			t.Errorf("legend lacks %q", w)
		}
	}
}
