package web

import (
	"strings"
	"testing"

	"spotkeeper/internal/model"
)

func TestLevelLegacyValueMapsToFetched(t *testing.T) {
	fetched := []string{"nadziemny (0,1)", "podziemny (-1,-2)"}
	opts := levelOptions(fetched, "podziemny")
	if len(opts) != 3 {
		t.Fatalf("duplicates or missing options: %+v", opts)
	}
	for _, o := range opts {
		if o.Selected != (o.Value == "podziemny (-1,-2)") {
			t.Fatalf("wrong selection: %+v", opts)
		}
	}
	// exact value stays selected
	if o := levelOptions(fetched, "nadziemny (0,1)"); !o[1].Selected || len(o) != 3 {
		t.Fatalf("exact: %+v", o)
	}
	// rendered dashboard (levels cached by the endpoint) shows the level once
	var calls [2]int
	e := newPEnv(t, &calls, fetched, nil)
	e.st.settings.ParkingID = guidA
	e.st.users[testLogin] = model.User{Login: testLogin, Plate: "X", Level: "podziemny", Schedule: "1", Active: true}
	e.get("/settings/levels?parking="+guidA, true)
	b := e.do("GET", "/", nil).Body.String()
	sel := b[strings.Index(b, `<select id="level"`):]
	sel = sel[:strings.Index(sel, "</select>")]
	if strings.Count(sel, "<option") != 3 || !strings.Contains(sel, `<option value="podziemny (-1,-2)" selected>`) {
		t.Fatal("legacy level not mapped: " + sel)
	}
}

func TestLevelUnknownStoredValueLabelled(t *testing.T) {
	fetched := []string{"nadziemny (0,1)", "podziemny (-1,-2)"}
	opts := levelOptions(fetched, "garaż")
	last := opts[len(opts)-1]
	if len(opts) != 4 || !last.Selected || last.Value != "garaż" || last.Label != "garaż (zapisany, nie występuje na parkingu)" {
		t.Fatalf("%+v", opts)
	}
	// ambiguous substring is not guessed
	if o := levelOptions([]string{"Poziom -1", "Poziom -2"}, "poziom"); len(o) != 4 || !o[3].Selected {
		t.Fatalf("ambiguous: %+v", o)
	}
	// no fetched list: the stored value is shown as is
	if o := levelOptions(nil, "garaż"); len(o) != 2 || o[1].Label != "garaż" || !o[1].Selected {
		t.Fatalf("no list: %+v", o)
	}
	// JS mirrors the same labels
	js := string(assets["app.js"].body)
	if !strings.Contains(js, "(zapisany, nie występuje na parkingu)") || !strings.Contains(js, "hits.length === 1") {
		t.Fatal("app.js does not mirror the level logic")
	}
}

// Every marker used in calendar tiles has a legend sample with the same classes.
func TestLegendCoversMarkers(t *testing.T) {
	e := newDEnv(t)
	b := get(e, "/")
	i := strings.Index(b, `<div class="legend">`)
	if i < 0 {
		t.Fatal("no legend")
	}
	legend := b[i:]
	legend = legend[:strings.Index(legend, "</details>")]
	for _, cls := range []string{"today", "past", "off"} {
		if !strings.Contains(legend, `class="cell sample-tile `+cls+" ") {
			t.Errorf("legend lacks a %s tile sample", cls)
		}
	}
	for _, cls := range []string{"badge auto", "badge vac"} {
		if !strings.Contains(legend, `<span class="`+cls+`">`) {
			t.Errorf("legend lacks %s", cls)
		}
	}
	for _, cls := range statusClass {
		if !strings.Contains(legend, `<span class="pill `+cls+`">`) {
			t.Errorf("legend lacks the %s status dot", cls)
		}
	}
	// samples are tiles (same visual as the calendar), not chips
	if strings.Contains(legend, `class="legend-today" aria-hidden`) && !strings.Contains(legend, "sample-tile") {
		t.Error("legend uses a different marker style")
	}
	// tile CSS and sample CSS agree on the dot look
	css := string(assets["app.css"].body)
	for _, sel := range []string{".sample-tile .pill.failed", ".sample-tile .badge.auto", ".cell .pill.failed", ".cell .badge.auto"} {
		if !strings.Contains(css, sel) {
			t.Errorf("css lacks %s", sel)
		}
	}
}
