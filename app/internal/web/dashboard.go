package web

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"spotkeeper/internal/model"
	"spotkeeper/internal/timeutil"
)

// maxDayOffDays is how far ahead a day off can be marked.
const maxDayOffDays = 366

var flashTexts = map[string]string{
	"saved":           "Zapisano ustawienia.",
	"off_added":       "Zapisano urlop. Automat pominie ten dzień.",
	"off_removed":     "Usunięto urlop. Ten dzień wraca do harmonogramu automatu.",
	"off_range":       "Urlop można zaznaczyć tylko na dni, których automat jeszcze nie rozpoczął (dalej niż 4 tygodnie, do roku naprzód).",
	"off_bad":         "Nieprawidłowa data. Wybierz dzień z kalendarza.",
	"invalid":         "Nie zapisano ustawień: formularz zawiera nieprawidłowe dane.",
	"invalid_parking": "Nieprawidłowy identyfikator parkingu (oczekiwany format GUID).",
	"invalid_release": "Nieprawidłowa godzina (format GG:MM).",
	"invalid_amount":  "Kwota musi być większa od 0 i nie większa niż 1000 zł.",
	"error":           "Wystąpił błąd. Spróbuj ponownie.",
}

var statusPL = map[string]string{
	model.StatusPending:     "oczekuje",
	model.StatusRunning:     "w toku",
	model.StatusPaid:        "opłacono",
	model.StatusUnconfirmed: "niepotwierdzona płatność",
	model.StatusFailed:      "nieudana",
	model.StatusCancelled:   "anulowana",
	model.StatusSkipped:     "pominięto",
}

var kindPL = map[string]string{
	model.KindFuture: "przyszły",
	model.KindPast:   "bieżący",
}

type taskView struct {
	Day, Kind, Status, Class, Detail, Spot string
	Attempts                               int
}

var statusClass = map[string]string{
	model.StatusPending:     "pending",
	model.StatusRunning:     "running",
	model.StatusPaid:        "paid",
	model.StatusUnconfirmed: "unconfirmed",
	model.StatusFailed:      "failed",
	model.StatusCancelled:   "cancelled",
	model.StatusSkipped:     "skipped",
}

var weekdayShort = []string{"Pn", "Wt", "Śr", "Cz", "Pt", "So", "Nd"}

type calCell struct {
	Blank         bool
	Day           string
	Num           int
	WD            string
	Today         bool
	Auto          bool
	Status, Class string
	// Past days only show their history.
	Past bool
	// Title is the full date for the tooltip ("1 listopada 2026").
	Title string
	// Off marks a weekend or Polish public holiday; Holiday is the holiday's name.
	Off     bool
	Holiday string
	// Vacation is the user's day off; CanVac offers toggling it (days the 11:00 run has
	// not reached yet).
	Vacation, CanVac bool
}

var monthName = []string{"styczeń", "luty", "marzec", "kwiecień", "maj", "czerwiec", "lipiec",
	"sierpień", "wrzesień", "październik", "listopad", "grudzień"}
var monthGen = []string{"stycznia", "lutego", "marca", "kwietnia", "maja", "czerwca", "lipca",
	"sierpnia", "września", "października", "listopada", "grudnia"}

// monthCells builds the Monday-first grid of a YYYY-MM month. Past days show the
// latest task status only; days the daily run has not reached yet show the schedule and
// can be marked as days off. skipped reports days the run leaves out.
func (s *Server) monthCells(u model.User, sched, daysOff map[string]bool, skipped func(string) bool, month string, now time.Time) []calCell {
	today := timeutil.Today(now)
	first := month + "-01"
	last := timeutil.AddDays(addMonths(month, 1)+"-01", -1)
	lastDay := s.lastBookedDay(now)
	lastOff := timeutil.AddDays(today, maxDayOffDays)

	latest := map[string]model.Task{}
	tasks, _ := s.d.Store.ListTasksInRange(u.Login, first, last)
	for _, t := range tasks {
		if cur, ok := latest[t.Day]; !ok || t.UpdatedAt.After(cur.UpdatedAt) {
			latest[t.Day] = t
		}
	}

	cells := make([]calCell, 0, 42)
	for i := 1; i < timeutil.Weekday(first); i++ {
		cells = append(cells, calCell{Blank: true})
	}
	for day := first; day <= last; day = timeutil.AddDays(day, 1) {
		wd := timeutil.Weekday(day)
		c := calCell{Day: day, Num: atoi(day[8:]), WD: weekdayShort[wd-1], Today: day == today, Past: day < today,
			Title: fmt.Sprintf("%d %s %s", atoi(day[8:]), monthGen[atoi(day[5:7])-1], day[:4])}
		c.Vacation = daysOff[day]
		c.Auto = !c.Past && u.Active && sched[strconv.Itoa(wd)] && !skipped(day)
		// a day off only matters where the 11:00 run would book (or to undo one)
		c.CanVac = !c.Past && day > lastDay && day <= lastOff && (c.Vacation || c.Auto)
		c.Off, c.Holiday = timeutil.DayOff(day)
		if c.Holiday != "" {
			c.Title += " · " + c.Holiday
		}
		if t, ok := latest[day]; ok {
			c.Status, c.Class = statusPL[t.Status], statusClass[t.Status]
			if c.Status == "" {
				c.Status = t.Status
			}
		}
		cells = append(cells, c)
	}
	return cells
}

// lastBookedDay is the furthest day already opened for booking: spots for
// today+AheadDays are only published at the release time, so before then
// the limit is one day earlier.
func (s *Server) lastBookedDay(now time.Time) string {
	return timeutil.LastMonitorDay(now, s.releaseTime(), s.d.AheadDays)
}

type nextRun struct {
	Found    bool
	At       time.Time
	AtText   string
	Target   string
	TargetWD string
}

// nextRelease finds the next release instant after now whose target day
// (release day + ahead) has a scheduled weekday and is not skipped (day off,
// holiday); it looks 7 days ahead. skip may be nil.
func (s *Server) nextRelease(now time.Time, sched map[string]bool, skip func(day string) bool) nextRun {
	today := timeutil.Today(now)
	release := s.releaseTime()
	for i := 0; i <= 7; i++ {
		day := timeutil.AddDays(today, i)
		at, err := timeutil.At(day, release)
		if err != nil || !at.After(now) {
			continue
		}
		target := timeutil.AddDays(day, s.d.AheadDays)
		wd := timeutil.Weekday(target)
		if sched[strconv.Itoa(wd)] && (skip == nil || !skip(target)) {
			return nextRun{true, at, at.In(timeutil.Warsaw).Format("02.01.2006 15:04"), target, weekdayShort[wd-1]}
		}
	}
	return nextRun{}
}

func init() {
	dashboardRoutes = func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("GET /{$}", s.requireUser(s.dashboard))
		mux.HandleFunc("POST /settings", s.requireUser(s.saveSettings))
		mux.HandleFunc("GET /settings/levels", s.requireUser(s.levelsHandler))
		mux.HandleFunc("POST /daysoff", s.requireUser(s.addDayOff))
		mux.HandleFunc("POST /daysoff/delete", s.requireUser(s.deleteDayOff))
	}
}

// redirectMsg returns to the dashboard with a flash code, keeping the calendar
// month a form was posted from (hidden "m" field).
func redirectMsg(w http.ResponseWriter, r *http.Request, code string) {
	target := "/?msg=" + url.QueryEscape(code)
	if r.PostForm != nil {
		if m := r.PostForm.Get("m"); validMonth(m) {
			target += "&m=" + m + "#calendar"
		}
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// calMonthsAhead is how far the calendar can be browsed into the future.
const calMonthsAhead = 12

var monthRe = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

func validMonth(m string) bool { return monthRe.MatchString(m) }

// addMonths shifts a YYYY-MM month by n months.
func addMonths(m string, n int) string {
	t := time.Date(atoi(m[:4]), time.Month(atoi(m[5:7])+n), 1, 0, 0, 0, 0, time.UTC)
	return t.Format("2006-01")
}

// calMonth resolves ?m=YYYY-MM to a month between the user's first task (or the
// current month) and calMonthsAhead ahead; prev/next are "" at the bounds.
func (s *Server) calMonth(login, raw, today string) (month, prev, next string) {
	cur := today[:7]
	lo, hi := cur, addMonths(cur, calMonthsAhead)
	if first, err := s.d.Store.FirstTaskDay(login); err == nil && len(first) >= 7 && first[:7] < lo {
		lo = first[:7]
	}
	month = cur
	if validMonth(raw) {
		month = min(max(raw, lo), hi)
	}
	if month > lo {
		prev = addMonths(month, -1)
	}
	if month < hi {
		next = addMonths(month, 1)
	}
	return month, prev, next
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request, login string) {
	u, err := s.d.Store.GetUser(login)
	if err != nil {
		s.clearSession(w)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	checked := map[string]bool{}
	for _, d := range strings.Split(u.Schedule, ",") {
		checked[strings.TrimSpace(d)] = true
	}
	tasks, _ := s.d.Store.ListTasks(login, 30)
	daysOff := map[string]bool{}
	if ds, err := s.d.Store.ListDaysOff(login); err == nil {
		for _, d := range ds {
			daysOff[d] = true
		}
	}
	// skipped reports days the 11:00 run leaves out for this user
	skipped := func(day string) bool {
		return daysOff[day] || (u.SkipHolidays && timeutil.Holiday(day) != "")
	}
	now := s.d.Now()
	today := timeutil.Today(now)
	tv := make([]taskView, 0, len(tasks))
	var paid, bad, booked int
	nearest := ""
	for _, t := range tasks {
		st, ok := statusPL[t.Status]
		if !ok {
			st = t.Status
		}
		k, ok := kindPL[t.Kind]
		if !ok {
			k = t.Kind
		}
		tv = append(tv, taskView{t.Day, k, st, statusClass[t.Status], t.Detail, t.SpotID, t.Attempts})
		switch t.Status {
		case model.StatusPaid:
			paid++
			if t.Day >= today {
				booked++
				if nearest == "" || t.Day < nearest {
					nearest = t.Day
				}
			}
		case model.StatusUnconfirmed, model.StatusFailed:
			bad++
		}
	}
	rate := -1
	if paid+bad > 0 {
		rate = (paid*100 + (paid+bad)/2) / (paid + bad)
	}
	month, prevMonth, nextMonth := s.calMonth(login, r.URL.Query().Get("m"), today)
	app, _ := s.d.Store.AppSettings()
	noParking := app.ParkingID == ""
	cells := s.monthCells(u, checked, daysOff, skipped, month, now)
	if noParking {
		for i := range cells {
			cells[i].Auto = false
		}
	}
	next := nextRun{}
	if u.Active && !noParking {
		next = s.nextRelease(now, checked, skipped)
	}
	var flashOK, flashErr string
	msg := r.URL.Query().Get("msg")
	switch msg {
	case "saved", "off_added", "off_removed":
		flashOK = flashTexts[msg]
	default:
		flashErr = flashTexts[msg]
	}
	plist, perr := s.parkings(r)
	data := map[string]any{
		"ParkingOpts": parkingOpts(plist, app.ParkingID),
		"ParkingsErr": perr != nil,
		"User":        login,
		"Settings":    u,
		"Days":        dayOpts(checked),
		"Tasks":       tv,
		"Cells":       cells,
		"CalTitle":    monthName[atoi(month[5:7])-1] + " " + month[:4],
		"CalMonth":    month,
		"CalPrev":     prevMonth,
		"CalNext":     nextMonth,
		"CalIsCur":    month == today[:7],
		"Next":        next,
		"NextISO":     next.At.Format(time.RFC3339),
		"Rate":        rate,
		"Paid":        paid,
		"Bad":         bad,
		"NTasks":      len(tasks),
		"Booked":      booked,
		"Nearest":     nearest,
		"FlashOK":     flashOK,
		"FlashErr":    flashErr,
		"Release":     s.releaseTime(),
		"App":         app,
		"Amount":      strconv.FormatFloat(app.PaymentAmount, 'f', -1, 64),
		"NoParking":   noParking,
		"NoPlate":     strings.TrimSpace(u.Plate) == "",
		"LevelSel":    u.Level,
		"LevelOpts":   levelOptions(s.cachedLevels(app.ParkingID), u.Level),
	}
	s.render(w, http.StatusOK, "dashboard.html", data)
}

var (
	guidRe    = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)
	releaseRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
)

// validIP accepts an empty value (the default applies) or a dotted IPv4 address.
func validIP(ip string) bool {
	if ip == "" {
		return true
	}
	a, err := netip.ParseAddr(ip)
	return err == nil && a.Is4() && strings.Count(ip, ".") == 3
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request, login string) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u, err := s.d.Store.GetUser(login)
	if err != nil {
		redirectMsg(w, r, "error")
		return
	}
	seen := map[int]bool{}
	for _, d := range r.PostForm["day"] {
		if n, err := strconv.Atoi(d); err == nil && n >= 1 && n <= 7 {
			seen[n] = true
		}
	}
	var days []string
	for i := 1; i <= 7; i++ {
		if seen[i] {
			days = append(days, strconv.Itoa(i))
		}
	}
	app, _ := s.d.Store.AppSettings()
	// the picker posts parking_id; a GUID typed under "Zaawansowane" wins over it
	manual := strings.TrimSpace(r.PostForm.Get("parking_id_manual"))
	if r.PostForm.Has("parking_id") || manual != "" {
		p := strings.TrimSpace(r.PostForm.Get("parking_id"))
		if manual != "" {
			p = manual
		}
		if p != "" && !guidRe.MatchString(p) {
			redirectMsg(w, r, "invalid_parking")
			return
		}
		app.ParkingID = p
	}
	if r.PostForm.Has("release_time") {
		rt := strings.TrimSpace(r.PostForm.Get("release_time"))
		if !releaseRe.MatchString(rt) {
			redirectMsg(w, r, "invalid_release")
			return
		}
		app.ReleaseTime = rt
	}
	if r.PostForm.Has("amount") {
		a, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r.PostForm.Get("amount")), ",", "."), 64)
		if err != nil || math.IsNaN(a) || !(a > 0 && a <= 1000) {
			redirectMsg(w, r, "invalid_amount")
			return
		}
		app.PaymentAmount = a
	}
	plate := strings.ToUpper(strings.TrimSpace(r.PostFormValue("plate")))
	level := strings.TrimSpace(r.PostFormValue("level"))
	ip := strings.TrimSpace(r.PostFormValue("ip"))
	if plate == "" || len(plate) > 16 || !validLevel(level) || !validIP(ip) {
		redirectMsg(w, r, "invalid")
		return
	}
	u.Plate, u.Level, u.IP, u.Schedule = plate, level, ip, strings.Join(days, ",")
	u.Active = r.PostFormValue("active") != ""
	u.SkipHolidays = r.PostFormValue("skip_holidays") != ""
	if err := s.d.Store.SaveAppSettings(app); err != nil {
		slog.Error("save app settings", "err", err)
		redirectMsg(w, r, "error")
		return
	}
	if err := s.d.Store.UpsertUser(u); err != nil {
		slog.Error("save settings", "login", login, "err", err)
		redirectMsg(w, r, "error")
		return
	}
	redirectMsg(w, r, "saved")
}

func validDay(day string) bool {
	t, err := time.Parse("2006-01-02", day)
	return err == nil && t.Format("2006-01-02") == day
}

// addDayOff marks a day off. Only days the daily run has not reached yet (after the
// furthest opened day, up to maxDayOffDays ahead) make sense: earlier ones are booked.
func (s *Server) addDayOff(w http.ResponseWriter, r *http.Request, login string) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	day := strings.TrimSpace(r.PostFormValue("day"))
	if !validDay(day) {
		redirectMsg(w, r, "off_bad")
		return
	}
	now := s.d.Now()
	if day <= s.lastBookedDay(now) || day > timeutil.AddDays(timeutil.Today(now), maxDayOffDays) {
		redirectMsg(w, r, "off_range")
		return
	}
	if err := s.d.Store.AddDayOff(login, day); err != nil {
		slog.Error("add day off", "login", login, "err", err)
		redirectMsg(w, r, "error")
		return
	}
	redirectMsg(w, r, "off_added")
}

func (s *Server) deleteDayOff(w http.ResponseWriter, r *http.Request, login string) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	day := strings.TrimSpace(r.PostFormValue("day"))
	if !validDay(day) {
		redirectMsg(w, r, "off_bad")
		return
	}
	if err := s.d.Store.RemoveDayOff(login, day); err != nil {
		slog.Error("remove day off", "login", login, "err", err)
		redirectMsg(w, r, "error")
		return
	}
	redirectMsg(w, r, "off_removed")
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
