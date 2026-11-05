package check

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"spotkeeper/internal/model"
	"spotkeeper/internal/store"
	"spotkeeper/internal/timeutil"
)

// Step is one line of the configuration check report.
type Step struct {
	Name    string
	OK      bool
	Skipped bool
	Detail  string
	// Help is an in-app link with more guidance for a failed step (optional).
	Help string
}

// helpURL points to the "Zanim zaczniesz" checklist of the Pomoc page.
const helpURL = "/pomoc#zanim-zaczniesz"

// Deps holds the collaborators Run needs.
type Deps struct {
	User     func() (model.User, error)
	Settings func() (store.AppSettings, error)
	Prepare  func(ctx context.Context, u model.User, day string) (model.Job, error)
	Offers   func(ctx context.Context, jwt, parkingID string, start, end time.Time) (model.Offers, error)
	// Levels lists the spot levels of a parking (optional; nil skips the level check).
	Levels func(ctx context.Context, jwt, parkingID string, now time.Time) ([]string, error)
	Token  func(ctx context.Context, turnstileURL, requestID string) (model.Token, error)
	// PublicIP detects the payment IP when the user has none set (optional).
	PublicIP func(ctx context.Context) (string, error)
	Now      func() time.Time
	// Progress, when set, is called with done=false as a step starts and with
	// done=true and the result when it ends (skipped steps only report done).
	Progress func(stepIndex int, name string, done bool, s Step)
}

// callTimeout bounds each external call (a variable so tests can shorten it).
var callTimeout = 60 * time.Second

const skipped = "Pominięto"

// StepNames lists the check steps in order (also used to render the live list).
var StepNames = []string{"Ustawienia", "Logowanie do serwisu", "Pojazd i karta", "Parking", "Poziom", "Turnstile"}

// Run checks the configuration step by step and stops at the first failure;
// later steps are reported as skipped. It never reserves or pays.
func Run(ctx context.Context, d Deps) []Step {
	names := StepNames
	var out []Step
	progress := func(i int, done bool, st Step) {
		if d.Progress != nil && i < len(names) {
			d.Progress(i, names[i], done, st)
		}
	}
	add := func(name string, ok bool, detail string) bool {
		st := Step{Name: name, OK: ok, Detail: detail}
		out = append(out, st)
		progress(len(out)-1, true, st)
		if ok {
			progress(len(out), false, Step{})
		}
		return ok
	}
	// addHelp records a failed step together with the link to the checklist.
	addHelp := func(name, detail string) {
		st := Step{Name: name, Detail: detail, Help: helpURL}
		out = append(out, st)
		progress(len(out)-1, true, st)
	}
	finish := func() []Step {
		for _, n := range names[len(out):] {
			st := Step{Name: n, Skipped: true, Detail: skipped}
			out = append(out, st)
			progress(len(out)-1, true, st)
		}
		return out
	}
	progress(0, false, Step{})
	call := func() (context.Context, context.CancelFunc) { return context.WithTimeout(ctx, callTimeout) }

	// 1 settings
	user, err := d.User()
	if err != nil {
		add(names[0], false, "Nie można odczytać konta: "+err.Error())
		return finish()
	}
	set, err := d.Settings()
	if err != nil {
		add(names[0], false, "Nie można odczytać ustawień: "+err.Error())
		return finish()
	}
	if strings.TrimSpace(user.Plate) == "" {
		add(names[0], false, "Brak numeru rejestracyjnego – uzupełnij go w ustawieniach.")
		return finish()
	}
	if strings.TrimSpace(set.ParkingID) == "" {
		add(names[0], false, "Brak identyfikatora parkingu – uzupełnij go w ustawieniach.")
		return finish()
	}
	detail := "OK"
	if ip := strings.TrimSpace(user.IP); ip != "" {
		detail += "; IP płatności: " + ip + " (z ustawień)"
	} else if d.PublicIP != nil {
		cctx, cancel := call()
		ip, err := d.PublicIP(cctx)
		cancel()
		if err != nil {
			detail += "; nie udało się wykryć IP płatności (" + err.Error() + ")"
		} else {
			detail += "; IP płatności: " + ip + " (wykryte automatycznie)"
		}
	}
	add(names[0], true, detail)

	// 2+3 login, vehicle and card
	day := timeutil.Today(d.Now().Add(24 * time.Hour))
	cctx, cancel := call()
	job, err := d.Prepare(cctx, user, day)
	cancel()
	if err != nil {
		var api model.APIError
		msg := err.Error()
		low := strings.ToLower(msg)
		switch {
		case errors.As(err, &api) && (api.HTTPStatus == 401 || api.HTTPStatus == 403),
			strings.Contains(low, "login:") && !errors.As(err, &api),
			strings.Contains(low, "get password"):
			add(names[1], false, "Nieprawidłowy login lub hasło do serwisu – sprawdź ACCOUNT_LOGIN i ACCOUNT_PASSWORD.")
		case strings.Contains(low, "vehicle not found"):
			add(names[1], true, "OK")
			addHelp(names[2], "W serwisie nie ma samochodu o numerze "+strings.TrimSpace(user.Plate)+". Dodaj go w aplikacji serwisu rezerwacji (Pojazdy) albo popraw numer w ustawieniach.")
		case strings.Contains(low, "no usable card"):
			add(names[1], true, "OK")
			addHelp(names[2], "Na koncie nie ma aktywnej karty płatniczej. Dodaj kartę w aplikacji serwisu (Płatności).")
		case strings.Contains(low, "vehicles:") || strings.Contains(low, "credit cards:") || strings.Contains(low, "plate"):
			add(names[1], true, "OK")
			add(names[2], false, msg)
		default:
			add(names[1], false, msg)
		}
		return finish()
	}
	add(names[1], true, "OK")
	add(names[2], true, "OK")

	// 4 parking
	cctx, cancel = call()
	offers, err := d.Offers(cctx, job.JWT, set.ParkingID, job.Start, job.End)
	cancel()
	if err != nil {
		add(names[3], false, "Parking nie odpowiada – sprawdź identyfikator parkingu ("+err.Error()+")")
		return finish()
	}
	if offers.TurnstileURL == "" {
		add(names[3], false, "Parking nie podał adresu bramki Turnstile – sprawdź identyfikator parkingu.")
		return finish()
	}
	add(names[3], true, "OK")

	// 5 level
	if !checkLevel(ctx, d, call, add, names[4], job.JWT, set.ParkingID, user.Level) {
		return finish()
	}

	// 6 turnstile
	var rb [4]byte
	rand.Read(rb[:])
	cctx, cancel = call()
	_, err = d.Token(cctx, offers.TurnstileURL, "check-"+hex.EncodeToString(rb[:]))
	cancel()
	if err != nil {
		add(names[5], false, "Serwis Turnstile nie odpowiada – sprawdź TURNSTILE_URL i czy kontener turnstile działa ("+err.Error()+")")
		return finish()
	}
	add(names[5], true, "OK")
	return out
}

// checkLevel reports the "Poziom" step; false means a failure that stops the check.
// An empty level (any) is always fine. A level the parking's current spots do not
// have is a failure; when no levels can be listed (no free spots, or the lookup
// failed) the step passes with a warning. Matching follows the engine: the stored
// level is a case-insensitive substring of a spot level.
func checkLevel(ctx context.Context, d Deps, call func() (context.Context, context.CancelFunc),
	add func(string, bool, string) bool, name, jwt, parkingID, level string) bool {
	level = strings.TrimSpace(level)
	if level == "" {
		return add(name, true, "OK (dowolny poziom)")
	}
	if d.Levels == nil {
		return add(name, true, "OK; nie sprawdzono, czy poziom występuje na parkingu.")
	}
	cctx, cancel := call()
	levels, err := d.Levels(cctx, jwt, parkingID, d.Now())
	cancel()
	if err != nil {
		return add(name, true, "OK; nie udało się sprawdzić poziomów parkingu ("+err.Error()+").")
	}
	if len(levels) == 0 {
		return add(name, true, "OK; brak wolnych miejsc do sprawdzenia poziomów – nie można potwierdzić, że poziom \""+level+"\" istnieje.")
	}
	low := strings.ToLower(level)
	for _, l := range levels {
		if strings.Contains(strings.ToLower(l), low) {
			return add(name, true, "OK")
		}
	}
	return add(name, false, "Wybrany poziom nie występuje na tym parkingu.")
}
