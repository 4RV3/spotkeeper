package check

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"spotkeeper/internal/model"
	"spotkeeper/internal/store"
)

// fakeDeps returns fakes; SUPERLITE_REAL=1 has no real wiring here because
// Run's collaborators are injected by main (covered by the seam check).
func fakeDeps() (Deps, *[]string) {
	var calls []string
	_ = os.Getenv("SUPERLITE_REAL")
	return Deps{
		User:     func() (model.User, error) { return model.User{Login: "l", Plate: "WX1"}, nil },
		Settings: func() (store.AppSettings, error) { return store.AppSettings{ParkingID: "g"}, nil },
		Prepare: func(_ context.Context, _ model.User, day string) (model.Job, error) {
			calls = append(calls, "prepare "+day)
			return model.Job{JWT: "j"}, nil
		},
		Offers: func(context.Context, string, string, time.Time, time.Time) (model.Offers, error) {
			calls = append(calls, "offers")
			return model.Offers{TurnstileURL: "http://t"}, nil
		},
		Token: func(_ context.Context, _, id string) (model.Token, error) {
			calls = append(calls, "token "+id)
			return model.Token{Value: "v"}, nil
		},
		Now: func() time.Time { return time.Date(2026, 10, 24, 23, 30, 0, 0, time.UTC) },
	}, &calls
}

func wantSteps(t *testing.T, got []Step, ok int, failName string) {
	t.Helper()
	if len(got) != 6 {
		t.Fatal(got)
	}
	for i, s := range got {
		switch {
		case i < ok:
			if !s.OK {
				t.Fatalf("step %d: %+v", i, s)
			}
		case i == ok && failName != "":
			if s.OK || s.Skipped || s.Name != failName {
				t.Fatalf("step %d: %+v", i, s)
			}
		default:
			if s.OK || !s.Skipped || s.Detail != "Pominięto" {
				t.Fatalf("step %d: %+v", i, s)
			}
		}
	}
}

func TestCheckAllOK(t *testing.T) {
	d, calls := fakeDeps()
	wantSteps(t, Run(context.Background(), d), 6, "")
	// 23:30Z on 24 Oct is 01:30 on 25 Oct in Warsaw; tomorrow is the 26th.
	if len(*calls) != 3 || (*calls)[0] != "prepare 2026-10-26" {
		t.Fatal(*calls)
	}
}

func TestCheckEmptyParkingID(t *testing.T) {
	d, calls := fakeDeps()
	d.Settings = func() (store.AppSettings, error) { return store.AppSettings{}, nil }
	wantSteps(t, Run(context.Background(), d), 0, "Ustawienia")
	if len(*calls) != 0 {
		t.Fatal(*calls)
	}
}

func TestCheck401(t *testing.T) {
	d, _ := fakeDeps()
	d.Prepare = func(context.Context, model.User, string) (model.Job, error) {
		return model.Job{}, fmt.Errorf("login: %w", model.APIError{HTTPStatus: 401, Message: "bad"})
	}
	got := Run(context.Background(), d)
	wantSteps(t, got, 1, "Logowanie do serwisu")
	if !strings.HasPrefix(got[1].Detail, "Nieprawidłowy login lub hasło do serwisu") {
		t.Fatal(got[1])
	}
}

func TestCheckTokenFailure(t *testing.T) {
	d, _ := fakeDeps()
	d.Token = func(context.Context, string, string) (model.Token, error) { return model.Token{}, errors.New("boom") }
	got := Run(context.Background(), d)
	wantSteps(t, got, 5, "Turnstile")
	if !contains(got[5].Detail, "TURNSTILE_URL") {
		t.Fatal(got[4])
	}
}

func TestCheckNoReservation(t *testing.T) {
	// Deps has no reserve/pay hooks; a vehicle error stops at step 3.
	d, _ := fakeDeps()
	d.Prepare = func(context.Context, model.User, string) (model.Job, error) {
		return model.Job{}, errors.New("vehicle not found")
	}
	got := Run(context.Background(), d)
	wantSteps(t, got, 2, "Pojazd i karta")
	if !got[1].OK || got[2].Detail != "W serwisie nie ma samochodu o numerze WX1. Dodaj go w aplikacji serwisu rezerwacji (Pojazdy) albo popraw numer w ustawieniach." || got[2].Help != "/pomoc#zanim-zaczniesz" {
		t.Fatal(got)
	}
}

func TestCheckTimeout(t *testing.T) {
	old := callTimeout
	callTimeout = 20 * time.Millisecond
	defer func() { callTimeout = old }()
	d, _ := fakeDeps()
	d.Offers = func(ctx context.Context, _, _ string, _, _ time.Time) (model.Offers, error) {
		<-ctx.Done()
		return model.Offers{}, ctx.Err()
	}
	wantSteps(t, Run(context.Background(), d), 3, "Parking")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSettingsDetailMentionsIP(t *testing.T) {
	d, _ := fakeDeps()
	d.PublicIP = func(context.Context) (string, error) { return "203.0.113.7", nil }
	got := Run(context.Background(), d)
	if want := "IP płatności: 203.0.113.7 (wykryte automatycznie)"; !strings.Contains(got[0].Detail, want) {
		t.Fatalf("%q", got[0].Detail)
	}
	d.User = func() (model.User, error) { return model.User{Login: "l", Plate: "WX1", IP: "198.51.100.1"}, nil }
	got = Run(context.Background(), d)
	if want := "IP płatności: 198.51.100.1 (z ustawień)"; !strings.Contains(got[0].Detail, want) {
		t.Fatalf("%q", got[0].Detail)
	}
	d.User = func() (model.User, error) { return model.User{Login: "l", Plate: "WX1"}, nil }
	d.PublicIP = func(context.Context) (string, error) { return "", errors.New("boom") }
	got = Run(context.Background(), d)
	if !got[0].OK || !strings.Contains(got[0].Detail, "boom") {
		t.Fatalf("lookup failure must be a hint: %+v", got[0])
	}
}

func levelDeps(level string, levels []string, err error, calls *int) Deps {
	d, _ := fakeDeps()
	d.User = func() (model.User, error) { return model.User{Login: "l", Plate: "WX1", Level: level}, nil }
	d.Levels = func(_ context.Context, jwt, parking string, _ time.Time) ([]string, error) {
		*calls++
		if jwt != "j" || parking != "g" {
			return nil, errors.New("wrong args")
		}
		return levels, err
	}
	return d
}

func TestCheckLevelAny(t *testing.T) {
	n := 0
	got := Run(context.Background(), levelDeps("", []string{"A"}, nil, &n))
	wantSteps(t, got, 6, "")
	if got[4].Name != "Poziom" || n != 0 {
		t.Fatalf("%+v calls=%d", got[4], n)
	}
}

func TestCheckLevelPresent(t *testing.T) {
	for _, lvl := range []string{"podziemny (-1)", "PODZIEMNY"} { // exact, legacy substring
		n := 0
		got := Run(context.Background(), levelDeps(lvl, []string{"nadziemny (+1)", "podziemny (-1)"}, nil, &n))
		wantSteps(t, got, 6, "")
		if n != 1 || got[4].Detail != "OK" {
			t.Fatalf("%q %+v", lvl, got[4])
		}
	}
}

func TestCheckLevelMissing(t *testing.T) {
	n := 0
	got := Run(context.Background(), levelDeps("Poziom Z", []string{"Poziom A"}, nil, &n))
	wantSteps(t, got, 4, "Poziom")
	if got[4].Detail != "Wybrany poziom nie występuje na tym parkingu." {
		t.Fatal(got[4])
	}
}

func TestCheckLevelEmptyListIsWarning(t *testing.T) {
	n := 0
	got := Run(context.Background(), levelDeps("Poziom A", []string{}, nil, &n))
	wantSteps(t, got, 6, "")
	if !got[4].OK || got[4].Detail == "OK" || !strings.Contains(got[4].Detail, "brak wolnych miejsc") {
		t.Fatalf("%+v", got[4])
	}
}

func TestRunProgress(t *testing.T) {
	var ev []string
	d, _ := fakeDeps()
	d.User = func() (model.User, error) { return model.User{Login: "u"}, nil } // no plate: step 0 fails
	d.Progress = func(i int, name string, done bool, s Step) {
		state := "start"
		if done {
			state = "done"
			if s.Skipped {
				state = "skip"
			}
		}
		ev = append(ev, fmt.Sprintf("%d:%s", i, state))
	}
	Run(context.Background(), d)
	got := strings.Join(ev, ",")
	if got != "0:start,0:done,1:skip,2:skip,3:skip,4:skip,5:skip" {
		t.Fatal(got)
	}
}

func TestCheckNoActiveCard(t *testing.T) {
	d, _ := fakeDeps()
	d.Prepare = func(context.Context, model.User, string) (model.Job, error) {
		return model.Job{}, errors.New("no usable card")
	}
	got := Run(context.Background(), d)
	wantSteps(t, got, 2, "Pojazd i karta")
	if got[2].Detail != "Na koncie nie ma aktywnej karty płatniczej. Dodaj kartę w aplikacji serwisu (Płatności)." || got[2].Help == "" {
		t.Fatal(got)
	}
}
