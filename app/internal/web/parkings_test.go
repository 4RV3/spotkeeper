package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

const (
	guidA = "aaaaaaaa-1111-2222-3333-444444444444"
	guidB = "bbbbbbbb-1111-2222-3333-444444444444"
)

var testParkings = []model.Parking{
	{ID: guidB, Name: "Zielony Parking", City: "Testowo", Street: "Probna 2"},
	{ID: guidA, Name: "Parking Testowy", City: "Przykladowo", Street: "Wzorcowa 10"},
}

// newPEnv is a dashboard env with the parking and level sources wired in.
func newPEnv(t *testing.T, calls *[2]int, levels []string, levelsErr error) *dEnv {
	e := newDEnv(t)
	e.srv.d.Parkings = func(context.Context) ([]model.Parking, error) {
		calls[0]++
		return testParkings, nil
	}
	e.srv.d.Levels = func(_ context.Context, id string) ([]string, error) {
		calls[1]++
		return levels, levelsErr
	}
	return e
}

func TestParkingPickerRendersSortedSelect(t *testing.T) {
	var calls [2]int
	e := newPEnv(t, &calls, nil, nil)
	e.st.settings.ParkingID = guidB
	b := e.do("GET", "/", nil).Body.String()
	a := strings.Index(b, `<option value="`+guidA+`">Parking Testowy — Przykladowo, Wzorcowa 10</option>`)
	z := strings.Index(b, `<option value="`+guidB+`" selected>Zielony Parking — Testowo, Probna 2</option>`)
	if !strings.Contains(b, `<select id="parking_id" name="parking_id"`) || a < 0 || z < 0 || a > z {
		t.Fatalf("picker wrong (a=%d z=%d)", a, z)
	}
	if strings.Contains(b, "Nie udało się pobrać listy parkingów") {
		t.Fatal("unexpected error note")
	}
	e.do("GET", "/", nil)
	if calls[0] != 1 {
		t.Fatalf("parkings not cached: %d calls", calls[0])
	}
}

func TestParkingPickerCacheExpires(t *testing.T) {
	var calls [2]int
	e := newPEnv(t, &calls, nil, nil)
	now := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	e.srv.d.Now = func() time.Time { return now }
	e.do("GET", "/", nil)
	now = now.Add(11 * time.Minute)
	e.do("GET", "/", nil)
	if calls[0] != 2 {
		t.Fatalf("calls %d", calls[0])
	}
}

func TestParkingPickerErrorKeepsCurrent(t *testing.T) {
	e := newDEnv(t)
	e.srv.d.Parkings = func(context.Context) ([]model.Parking, error) { return nil, errors.New("down") }
	b := e.do("GET", "/", nil).Body.String()
	if !strings.Contains(b, "Nie udało się pobrać listy parkingów") ||
		!strings.Contains(b, `<option value="`+fixtureGUID+`" selected>`) || strings.Count(b, `<option value="0`) != 1 {
		t.Fatal("error fallback wrong")
	}
	if !strings.Contains(b, `name="parking_id_manual"`) {
		t.Fatal("manual fallback missing")
	}
}

func TestManualParkingOverridesPicker(t *testing.T) {
	e := newDEnv(t)
	w := e.do("POST", "/settings", settingsForm(url.Values{"parking_id": {guidA}, "parking_id_manual": {guidB}}))
	if !strings.Contains(w.Header().Get("Location"), "saved") || e.st.settings.ParkingID != guidB {
		t.Fatal(w.Header(), e.st.settings)
	}
	w = e.do("POST", "/settings", settingsForm(url.Values{"parking_id": {guidA}, "parking_id_manual": {"nope"}}))
	if !strings.Contains(w.Header().Get("Location"), "invalid_parking") || e.st.settings.ParkingID != guidB {
		t.Fatal(w.Header(), e.st.settings)
	}
	w = e.do("POST", "/settings", settingsForm(url.Values{"parking_id": {guidA}, "parking_id_manual": {""}}))
	if e.st.settings.ParkingID != guidA {
		t.Fatal(e.st.settings)
	}
}

func TestSettingsStoreExactLevel(t *testing.T) {
	e := newDEnv(t)
	w := e.do("POST", "/settings", settingsForm(url.Values{"level": {"Poziom -1 (garaż)"}}))
	if !strings.Contains(w.Header().Get("Location"), "saved") || e.st.users[testLogin].Level != "Poziom -1 (garaż)" {
		t.Fatal(w.Header(), e.st.users[testLogin])
	}
	b := e.do("GET", "/", nil).Body.String()
	if !strings.Contains(b, `<option value="Poziom -1 (garaż)" selected>`) {
		t.Fatal("stored level not preselected")
	}
}

func (e *dEnv) get(path string, withCookie bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	if withCookie {
		r.AddCookie(e.cookie)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func TestLevelsEndpoint(t *testing.T) {
	var calls [2]int
	e := newPEnv(t, &calls, []string{"Poziom A", "Poziom B"}, nil)
	if w := e.get("/settings/levels?parking="+guidA, false); w.Code != http.StatusSeeOther || calls[1] != 0 {
		t.Fatalf("unauthenticated: %d calls=%d", w.Code, calls[1])
	}
	w := e.get("/settings/levels?parking="+guidA, true)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") ||
		strings.TrimSpace(w.Body.String()) != `{"levels":["Poziom A","Poziom B"]}` {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	e.get("/settings/levels?parking="+guidA, true)
	if calls[1] != 1 {
		t.Fatalf("levels not cached: %d calls", calls[1])
	}
	e.get("/settings/levels?parking="+guidB, true)
	if calls[1] != 2 {
		t.Fatalf("cache must be per parking: %d calls", calls[1])
	}
}

func TestLevelsEndpointEmptyBadAndErrors(t *testing.T) {
	var calls [2]int
	e := newPEnv(t, &calls, nil, nil)
	if w := e.get("/settings/levels?parking="+guidA, true); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"levels":[]}` {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := e.get("/settings/levels?parking=nope", true); w.Code != 400 {
		t.Fatal(w.Code)
	}
	e2 := newPEnv(t, &calls, nil, errors.New("down"))
	if w := e2.get("/settings/levels?parking="+guidA, true); w.Code != http.StatusBadGateway {
		t.Fatal(w.Code)
	}
	if w := e2.get("/settings/levels?parking="+guidA, true); calls[1] != 3 || w.Code != http.StatusBadGateway {
		t.Fatalf("errors must not be cached: %d", calls[1])
	}
}
