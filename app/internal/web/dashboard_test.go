package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"spotkeeper/internal/model"
	"spotkeeper/internal/store"
)

type dStore struct {
	users    map[string]model.User
	tasks    []model.Task
	off      map[string]map[string]bool
	settings store.AppSettings
}

func (f *dStore) UpsertUser(u model.User) error { f.users[u.Login] = u; return nil }
func (f *dStore) GetUser(l string) (model.User, error) {
	u, ok := f.users[l]
	if !ok {
		return u, model.ErrNotFound
	}
	return u, nil
}
func (f *dStore) ListTasks(string, int) ([]model.Task, error) { return f.tasks, nil }
func (f *dStore) ListTasksInRange(_, from, to string) ([]model.Task, error) {
	var out []model.Task
	for _, t := range f.tasks {
		if t.Day >= from && t.Day <= to {
			out = append(out, t)
		}
	}
	return out, nil
}
func (f *dStore) AddDayOff(l, d string) error {
	if f.off == nil {
		f.off = map[string]map[string]bool{}
	}
	if f.off[l] == nil {
		f.off[l] = map[string]bool{}
	}
	f.off[l][d] = true
	return nil
}
func (f *dStore) RemoveDayOff(l, d string) error { delete(f.off[l], d); return nil }
func (f *dStore) ListDaysOff(l string) ([]string, error) {
	var out []string
	for d := range f.off[l] {
		out = append(out, d)
	}
	return out, nil
}
func (f *dStore) FirstTaskDay(string) (string, error) {
	first := ""
	for _, t := range f.tasks {
		if first == "" || t.Day < first {
			first = t.Day
		}
	}
	return first, nil
}
func (f *dStore) AppSettings() (store.AppSettings, error) { return f.settings, nil }
func (f *dStore) SaveAppSettings(a store.AppSettings) error {
	f.settings = a
	return nil
}

const testLogin = "jan@example.com"
const fixtureGUID = "00000000-0000-0000-0000-000000000000"

type dEnv struct {
	h      http.Handler
	srv    *Server
	st     *dStore
	cookie *http.Cookie
}

func newDEnv(t *testing.T) *dEnv {
	now := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	e := &dEnv{st: &dStore{users: map[string]model.User{},
		settings: store.AppSettings{ParkingID: fixtureGUID, ReleaseTime: "11:00", PaymentAmount: 15}}}
	e.st.users[testLogin] = model.User{Login: testLogin, Plate: "WX1", Level: "podziemny", IP: "1.2.3.4", Schedule: "1,3", Active: true}
	h := NewServer(Deps{
		Store: e.st, Login: testLogin, AppPassword: "sesame123",
		SessionSecret: []byte("0123456789abcdef0123456789abcdef"),
		Now:           func() time.Time { return now },
	})
	e.h = h
	e.srv = h.(*Server)
	w := httptest.NewRecorder()
	e.srv.setSession(w, testLogin)
	e.cookie = w.Result().Cookies()[0]
	return e
}

func (e *dEnv) do(method, path string, v url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(e.cookie)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func TestDashboardRender(t *testing.T) {
	e := newDEnv(t)
	e.st.users[testLogin] = model.User{Login: testLogin, Plate: "<B>X", Level: "podziemny", Schedule: "1,3", Active: true}
	e.st.tasks = []model.Task{{Day: "2026-10-26", Kind: "future", Status: "paid", Detail: "<i>d"}}
	w := e.do("GET", "/?msg=%3Cscript%3E", nil)
	b := w.Body.String()
	if w.Code != 200 || strings.Contains(b, "<B>X") || !strings.Contains(b, "&lt;B&gt;X") {
		t.Fatal(w.Code, b)
	}
	for _, want := range []string{"opłacono", "przyszły", "&lt;i&gt;d", `data-day="2026-10-24"`, `data-day="2026-10-31"`} {
		if !strings.Contains(b, want) {
			t.Fatal("missing", want)
		}
	}
	if strings.Contains(b, "<script>") {
		t.Fatal("echoed msg")
	}
}

func TestSettingsUpdate(t *testing.T) {
	e := newDEnv(t)
	w := e.do("POST", "/settings", url.Values{"plate": {" ab12 "}, "level": {"nadziemny"}, "ip": {"5.6.7.8"}, "day": {"2", "5", "x"}, "active": {"1"}})
	if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "msg=saved") {
		t.Fatal(w.Code, w.Header())
	}
	u := e.st.users[testLogin]
	if u.Plate != "AB12" || u.Level != "nadziemny" || u.Schedule != "2,5" || !u.Active || u.IP != "5.6.7.8" {
		t.Fatalf("%+v", u)
	}
	e.do("POST", "/settings", url.Values{"plate": {"AB12"}, "level": {"nadziemny"}})
	if e.st.users[testLogin].Active {
		t.Fatal("active should be off")
	}
	for _, bad := range []url.Values{
		{"plate": {""}, "level": {""}},
		{"plate": {"Z"}, "level": {"bad\nlevel"}},
		{"plate": {"Z"}, "level": {strings.Repeat("x", 101)}},
		{"plate": {"Z"}, "ip": {"1.2.3"}},
		{"plate": {"Z"}, "ip": {"::1"}},
	} {
		w = e.do("POST", "/settings", bad)
		if !strings.Contains(w.Header().Get("Location"), "invalid") || e.st.users[testLogin].Plate != "AB12" {
			t.Fatalf("invalid accepted: %v", bad)
		}
	}
}

func TestReleaseTimeFromSettings(t *testing.T) {
	e := newDEnv(t)
	e.st.settings.ReleaseTime = "10:30"
	// 06:00Z = 08:00 Warsaw (CEST): today's 10:30 release is still ahead.
	e.srv.d.Now = func() time.Time { return time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC) }
	e.st.users[testLogin] = model.User{Login: testLogin, Plate: "X", Schedule: "1,2,3,4,5,6,7", Active: true}
	b := get(e, "/")
	if !strings.Contains(b, "24.10.2026 10:30") || !strings.Contains(b, `data-countdown="2026-10-24T08:30:00Z"`) || !strings.Contains(b, "rezerwacja automatyczna o 10:30") {
		t.Fatal("dashboard does not use the stored release time")
	}
	if strings.Contains(b, "11:00") {
		t.Fatal("hard-coded 11:00 left")
	}
	// a settings read error falls back to 11:00
	if got := (&Server{d: Deps{Store: failingSettings{e.st}}}).releaseTime(); got != "11:00" {
		t.Fatal(got)
	}
}

type failingSettings struct{ *dStore }

func (failingSettings) AppSettings() (store.AppSettings, error) {
	return store.AppSettings{}, errors.New("boom")
}

func TestRemovedRoutes404(t *testing.T) {
	e := newDEnv(t)
	for _, c := range []struct{ method, path string }{
		{"GET", "/register"}, {"POST", "/register"}, {"GET", "/admin"}, {"POST", "/admin/act"}, {"GET", "/sw.js"},
		{"POST", "/monitors"}, {"POST", "/monitors/delete"}, {"POST", "/push/subscribe"}, {"POST", "/push/unsubscribe"},
		{"POST", "/push/test"}, {"POST", "/telegram/unlink"}, {"POST", "/telegram/notify"}, {"POST", "/news/seen"},
		{"POST", "/onboarding/done"}, {"POST", "/account/delete"},
	} {
		// with and without a session the answer is the same: the route does not exist
		w := e.do(c.method, c.path, nil)
		if w.Code != 404 && w.Code != 405 {
			t.Errorf("%s %s (session): %d", c.method, c.path, w.Code)
		}
		w = httptest.NewRecorder()
		e.h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != 404 && w.Code != 405 {
			t.Errorf("%s %s: %d", c.method, c.path, w.Code)
		}
	}
	b := get(e, "/")
	for _, gone := range []string{"Telegram", "monitor", "Monitor", "push", "news", "data-tour", "Przewodnik", "Usuń konto", "/admin"} {
		if strings.Contains(b, gone) {
			t.Errorf("dashboard still mentions %q", gone)
		}
	}
	if strings.Contains(e.do("GET", "/static/app.js", nil).Body.String(), "serviceWorker") {
		t.Error("app.js still registers a service worker")
	}
}

func TestDashboardRequiresLogin(t *testing.T) {
	e := newDEnv(t)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("POST", "/settings", nil))
	if w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatal(w.Code)
	}
}

func get(e *dEnv, path string) string {
	return e.do("GET", path, nil).Body.String()
}

func TestStaticAssetsServed(t *testing.T) {
	e := newDEnv(t)
	for path, ct := range map[string]string{"/static/app.css": "text/css", "/static/app.js": "text/javascript"} {
		r := httptest.NewRequest("GET", path+"?v=abc", nil)
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), ct) ||
			w.Header().Get("Cache-Control") != "public, max-age=86400" || w.Body.Len() == 0 {
			t.Fatal(path, w.Code, w.Header())
		}
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("GET", "/static/nope.js", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	b := get(e, "/")
	if !strings.Contains(b, "/static/app.css?v="+assets["app.css"].version) {
		t.Fatal("no cache-busting version")
	}
}

var inlineScript = regexp.MustCompile(`<script(\s[^>]*)?>`)

func TestCSPNoInline(t *testing.T) {
	e := newDEnv(t)
	w := e.do("GET", "/", nil)
	c := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(c, "script-src 'self'") || strings.Contains(c, "unsafe-inline") || strings.Contains(c, "unsafe-hashes") {
		t.Fatal(c)
	}
	pages := map[string]string{"dash": w.Body.String()}
	r := httptest.NewRecorder()
	e.h.ServeHTTP(r, httptest.NewRequest("GET", "/login", nil))
	pages["/login"] = r.Body.String()
	for name, b := range pages {
		for _, m := range inlineScript.FindAllString(b, -1) {
			if !strings.Contains(m, "src=") {
				t.Fatal(name, "inline script", m)
			}
		}
		if strings.Contains(b, " style=") || strings.Contains(b, "<style") || strings.Contains(b, "onsubmit") {
			t.Fatal(name, "inline style/handler")
		}
	}
}

func TestDashboardKPIs(t *testing.T) {
	e := newDEnv(t)
	// now = Sat 2026-10-24 10:00Z (12:00 Warsaw CEST). Release 11:00 passed -> tomorrow 11:00,
	// target 2026-11-21 (Sat). Schedule Mon+Wed -> first match: release Mon 10-26 -> 11-23 (Mon).
	e.st.tasks = []model.Task{
		{Day: "2026-10-26", Kind: "future", Status: "paid"},
		{Day: "2026-10-28", Kind: "future", Status: "paid"},
		{Day: "2026-10-20", Kind: "future", Status: "paid"},
		{Day: "2026-10-21", Kind: "future", Status: "failed"},
		{Day: "2026-10-22", Kind: "future", Status: "cancelled"},
	}
	b := get(e, "/")
	for _, want := range []string{"75%", "3 opłaconych z 4", "najbliższy: <strong>2026-10-26</strong>",
		`data-countdown="2026-10-26T10:00:00Z"`, "26.10.2026 11:00", "<strong>2026-11-23</strong>"} {
		if !strings.Contains(b, want) {
			t.Fatal("missing", want, b)
		}
	}
}

func TestNextReleaseSkipsUnscheduled(t *testing.T) {
	e := newDEnv(t)
	// only Sunday (7) scheduled: target must be a Sunday.
	n := e.srv.nextRelease(time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC), map[string]bool{"7": true}, nil)
	if !n.Found || n.Target != "2026-11-22" || n.TargetWD != "Nd" {
		t.Fatalf("%+v", n)
	}
	// release 11:00 Warsaw today still ahead (09:00Z = 11:00 CET? Oct 24 is CEST: 11:00 = 09:00Z)
	n = e.srv.nextRelease(time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC), map[string]bool{"6": true}, nil)
	if !n.Found || n.Target != "2026-11-21" || !n.At.Equal(time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v", n)
	}
	if n := e.srv.nextRelease(time.Now(), map[string]bool{}, nil); n.Found {
		t.Fatal("empty schedule found")
	}
	// inactive user: no countdown
	u := e.st.users[testLogin]
	u.Active = false
	e.st.users[testLogin] = u
	if b := get(e, "/"); strings.Contains(b, "data-countdown") {
		t.Fatal("countdown for inactive user")
	}
}

func TestCalendarCells(t *testing.T) {
	e := newDEnv(t)
	e.st.tasks = []model.Task{
		{Day: "2026-10-26", Kind: "future", Status: "paid"},
		{Day: "2026-10-27", Kind: "future", Status: "failed"},
		{Day: "2026-10-28", Kind: "future", Status: "unconfirmed"},
	}
	b := get(e, "/")
	// every dated cell, whatever extra classes it has (today, month-start)
	if n := len(regexp.MustCompile(`<div class="cell[^"]*" data-day=`).FindAllString(b, -1)); n != 31 {
		t.Fatal("cells", n)
	}
	if strings.Count(b, `class="cell blank"`) != 3 { // 1 Oct 2026 is a Thursday -> 3 blanks
		t.Fatal("blanks")
	}
	if !regexp.MustCompile(`class="cell today[^"]*" data-day="2026-10-24"`).MatchString(b) {
		t.Fatal("today not marked")
	}
	cell := func(day string) string {
		i := strings.Index(b, `data-day="`+day+`"`)
		j := strings.Index(b[i+1:], "data-day=")
		if j < 0 {
			j = len(b) - i - 1
		}
		return b[i : i+1+j]
	}
	c26 := cell("2026-10-26") // Monday, scheduled, paid
	if !strings.Contains(c26, "badge auto") || !strings.Contains(c26, "pill paid") || strings.Contains(c26, "<form") {
		t.Fatal(c26)
	}
	c27 := cell("2026-10-27") // Tuesday, not scheduled, failed
	if strings.Contains(c27, "badge auto") || !strings.Contains(c27, "pill failed") || strings.Contains(c27, "<form") {
		t.Fatal(c27)
	}
	if c28 := cell("2026-10-28"); !strings.Contains(c28, "pill unconfirmed") || !strings.Contains(c28, "badge auto") {
		t.Fatal(c28)
	}
}

func settingsForm(over url.Values) url.Values {
	v := url.Values{"plate": {"AB12"}, "level": {"nadziemny"}, "parking_id": {"123e4567-e89b-12d3-a456-426614174000"},
		"release_time": {"11:00"}, "amount": {"15"}}
	for k, x := range over {
		v[k] = x
	}
	return v
}

func TestSaveSettingsBadParkingID(t *testing.T) {
	e := newDEnv(t)
	for _, bad := range []string{"abc", "zzzzzzzz-e89b-12d3-a456-426614174000", "123e4567-e89b-12d3-a456-4266141740000"} {
		w := e.do("POST", "/settings", settingsForm(url.Values{"parking_id": {bad}, "plate": {"NEW"}}))
		if !strings.Contains(w.Header().Get("Location"), "invalid") || e.st.settings.ParkingID != fixtureGUID || e.st.users[testLogin].Plate != "WX1" {
			t.Fatal("accepted", bad)
		}
	}
}

func TestSaveSettingsBadRelease(t *testing.T) {
	e := newDEnv(t)
	for _, bad := range []string{"25:00", "11:60", "9:00", ""} {
		w := e.do("POST", "/settings", settingsForm(url.Values{"release_time": {bad}}))
		if !strings.Contains(w.Header().Get("Location"), "invalid") || e.st.settings.ParkingID != fixtureGUID {
			t.Fatal("accepted", bad)
		}
	}
}

func TestSaveSettingsAmountBounds(t *testing.T) {
	e := newDEnv(t)
	for _, bad := range []string{"0", "1001", "-3", "abc", "NaN"} {
		w := e.do("POST", "/settings", settingsForm(url.Values{"amount": {bad}}))
		if !strings.Contains(w.Header().Get("Location"), "invalid") || e.st.settings.ParkingID != fixtureGUID {
			t.Fatal("accepted", bad)
		}
	}
	w := e.do("POST", "/settings", settingsForm(url.Values{"amount": {"15,5"}}))
	if !strings.Contains(w.Header().Get("Location"), "saved") || e.st.settings.PaymentAmount != 15.5 {
		t.Fatal(w.Header(), e.st.settings)
	}
}

func TestSettingsXSS(t *testing.T) {
	e := newDEnv(t)
	e.st.users[testLogin] = model.User{Login: testLogin, Plate: "<script>x", Schedule: "1", Active: true}
	e.st.settings.ParkingID = `"><script>`
	b := e.do("GET", "/", nil).Body.String()
	if strings.Contains(b, "<script>x") || strings.Contains(b, `"><script>`) || !strings.Contains(b, "&lt;script&gt;x") {
		t.Fatal("unescaped")
	}
}

func TestEmptyParkingIDBanner(t *testing.T) {
	e := newDEnv(t)
	e.st.settings.ParkingID = ""
	b := e.do("GET", "/", nil).Body.String()
	if !strings.Contains(b, `href="#settings"`) {
		t.Fatal("no banner")
	}
	if strings.Contains(b, `title="auto"`) {
		t.Fatal("auto hint shown")
	}
	e.st.settings.ParkingID = "123e4567-e89b-12d3-a456-426614174000"
	b = e.do("GET", "/", nil).Body.String()
	if strings.Contains(b, "nie wie, którego parkingu") || !strings.Contains(b, `title="auto"`) || !strings.Contains(b, `href="/check"`) {
		t.Fatal("banner/hints wrong")
	}
}

func TestSaveSettingsRoundTrip(t *testing.T) {
	e := newDEnv(t)
	w := e.do("POST", "/settings", settingsForm(url.Values{"release_time": {"10:30"}, "amount": {"12.25"}, "skip_holidays": {"1"}, "day": {"1", "2"}}))
	if !strings.Contains(w.Header().Get("Location"), "saved") {
		t.Fatal(w.Header())
	}
	want := store.AppSettings{ParkingID: "123e4567-e89b-12d3-a456-426614174000", ReleaseTime: "10:30", PaymentAmount: 12.25}
	if e.st.settings != want || !e.st.users[testLogin].SkipHolidays || e.st.users[testLogin].Schedule != "1,2" {
		t.Fatal(e.st.settings, e.st.users[testLogin])
	}
	b := e.do("GET", "/", nil).Body.String()
	for _, s := range []string{want.ParkingID, `value="10:30"`, `value="12.25"`} {
		if !strings.Contains(b, s) {
			t.Fatal("missing", s)
		}
	}
}

func TestCountdownDataAttr(t *testing.T) {
	e := newDEnv(t)
	e.st.settings.ReleaseTime = "10:30"
	e.srv.d.Now = func() time.Time { return time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC) }
	e.st.users[testLogin] = model.User{Login: testLogin, Plate: "X", Schedule: "1,2,3,4,5,6,7", Active: true}
	b := get(e, "/")
	if !strings.Contains(b, `class="countdown" data-countdown="2026-10-24T08:30:00Z"`) || !strings.Contains(b, "otwarcie za") {
		t.Fatal("countdown element missing")
	}
	u := e.st.users[testLogin]
	u.Active = false
	e.st.users[testLogin] = u
	if b := get(e, "/"); strings.Contains(b, "data-countdown") || strings.Contains(b, "otwarcie za") {
		t.Fatal("countdown for inactive user")
	}
}

func TestSavedToast(t *testing.T) {
	e := newDEnv(t)
	b := get(e, "/?msg=saved")
	if !strings.Contains(b, `class="toast" data-autohide="3000"`) || !strings.Contains(b, "Zapisano ustawienia.") {
		t.Fatal("saved toast missing")
	}
	if strings.Contains(b, `class="alert err"`) {
		t.Fatal("success shown as error banner")
	}
	b = get(e, "/?msg=invalid_parking")
	if strings.Contains(b, `class="toast"`) || !strings.Contains(b, `class="alert err" role="alert">Nieprawidłowy identyfikator parkingu`) {
		t.Fatal("error must be an inline banner, not a toast")
	}
}
