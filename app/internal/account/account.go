// Package account validates registration input and prepares reservation jobs.
package account

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"spotkeeper/internal/model"
	"spotkeeper/internal/timeutil"
)

// PC is the subset of the API client used here.
type PC interface {
	Login(ctx context.Context, login, password string) (model.Session, error)
	Vehicles(ctx context.Context, jwt string) ([]model.Vehicle, error)
	CreditCards(ctx context.Context, jwt string) ([]model.Card, error)
}

// SecretGetter reads stored passwords.
type SecretGetter interface {
	Get(ctx context.Context, key string) (string, error)
}

type entry struct {
	mu   sync.Mutex
	sess model.Session
	// vehicle/card resolved for plate; valid only while sess is reused.
	plate, vid, cid string
}

// Preparer builds jobs from users, caching service sessions per login.
type Preparer struct {
	pc        PC
	sec       SecretGetter
	parkingID string
	// ParkingID, if set, overrides the constructor value at each Prepare.
	ParkingID func() string
	// PublicIP, if set, detects the IP used for payments when the user has
	// none configured.
	PublicIP func(ctx context.Context) (string, error)
	now      func() time.Time

	mu    sync.Mutex
	cache map[string]*entry
}

// NewPreparer creates a Preparer.
func NewPreparer(pc PC, sec SecretGetter, parkingID string) *Preparer {
	return &Preparer{pc: pc, sec: sec, parkingID: parkingID, now: time.Now, cache: map[string]*entry{}}
}

func (p *Preparer) parking() string {
	if p.ParkingID != nil {
		return p.ParkingID()
	}
	return p.parkingID
}

type static struct{ key, password string }

// Static returns a SecretGetter holding the single password of login.
func Static(login, password string) SecretGetter {
	return static{key: "PC" + strings.ToLower(login), password: password}
}

func (s static) Get(_ context.Context, key string) (string, error) {
	if key != s.key {
		return "", errors.New("account: unknown secret key")
	}
	return s.password, nil
}

// Invalidate drops the cached session of login.
func (p *Preparer) Invalidate(login string) {
	p.mu.Lock()
	delete(p.cache, login)
	p.mu.Unlock()
}

func (p *Preparer) session(ctx context.Context, login, plate string) (model.Session, string, string, error) {
	p.mu.Lock()
	e := p.cache[login]
	if e == nil {
		e = &entry{}
		p.cache[login] = e
	}
	p.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	want := normPlate(plate)
	if e.sess.JWT != "" && e.sess.Exp.After(p.now().Add(5*time.Minute)) {
		if e.vid != "" && e.plate == want {
			return e.sess, e.vid, e.cid, nil
		}
		return p.fill(ctx, e, want, plate)
	}
	pw, err := p.sec.Get(ctx, "PC"+login)
	if err != nil {
		return model.Session{}, "", "", fmt.Errorf("get password: %w", err)
	}
	s, err := p.pc.Login(ctx, login, pw)
	if err != nil {
		return model.Session{}, "", "", fmt.Errorf("login: %w", err)
	}
	e.sess = s
	e.plate, e.vid, e.cid = "", "", ""
	return p.fill(ctx, e, want, plate)
}

// fill resolves vehicle and card for the entry's session; e.mu must be held.
func (p *Preparer) fill(ctx context.Context, e *entry, want, plate string) (model.Session, string, string, error) {
	vid, cid, err := p.resolve(ctx, e.sess.JWT, plate)
	if err != nil {
		return model.Session{}, "", "", err
	}
	e.plate, e.vid, e.cid = want, vid, cid
	return e.sess, vid, cid, nil
}

func normPlate(s string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.Join(strings.Fields(s), ""), " ", ""))
}

// resolve finds the vehicle matching plate and the first usable card.
func (p *Preparer) resolve(ctx context.Context, jwt, plate string) (vehicleID, cardID string, err error) {
	vs, err := p.pc.Vehicles(ctx, jwt)
	if err != nil {
		return "", "", fmt.Errorf("vehicles: %w", err)
	}
	want := normPlate(plate)
	for _, v := range vs {
		if normPlate(v.Plate) == want {
			vehicleID = v.ID
			break
		}
	}
	if vehicleID == "" {
		return "", "", fmt.Errorf("vehicle not found")
	}
	cs, err := p.pc.CreditCards(ctx, jwt)
	if err != nil {
		return "", "", fmt.Errorf("credit cards: %w", err)
	}
	for _, c := range cs {
		if c.Status == 1 && c.ClientHash != "" {
			return vehicleID, c.ID, nil
		}
	}
	return "", "", fmt.Errorf("no usable card")
}

// Session returns a valid JWT for any service login, using the password
// stored under "PC"+login and the shared per-login session cache. No vehicle
// or card is resolved.
func (p *Preparer) Session(ctx context.Context, login string) (string, error) {
	p.mu.Lock()
	e := p.cache[login]
	if e == nil {
		e = &entry{}
		p.cache[login] = e
	}
	p.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sess.JWT != "" && e.sess.Exp.After(p.now().Add(5*time.Minute)) {
		return e.sess.JWT, nil
	}
	pw, err := p.sec.Get(ctx, "PC"+login)
	if err != nil {
		return "", fmt.Errorf("get password: %w", err)
	}
	s, err := p.pc.Login(ctx, login, pw)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	e.sess = s
	e.plate, e.vid, e.cid = "", "", ""
	return s.JWT, nil
}

// VerifyLogin checks only that login/password log in to the service.
func (p *Preparer) VerifyLogin(ctx context.Context, login, password string) error {
	if _, err := p.pc.Login(ctx, login, password); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	return nil
}

// Verify checks credentials, plate and card; used at registration.
func (p *Preparer) Verify(ctx context.Context, login, password, plate string) error {
	s, err := p.pc.Login(ctx, login, password)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	_, _, err = p.resolve(ctx, s.JWT, plate)
	return err
}

// Prepare builds the reservation Job for user u on day.
func (p *Preparer) Prepare(ctx context.Context, u model.User, day string) (model.Job, error) {
	start, end, err := timeutil.DayWindow(day)
	if err != nil {
		return model.Job{}, err
	}
	s, vid, cid, err := p.session(ctx, u.Login, u.Plate)
	if err != nil {
		return model.Job{}, err
	}
	ip := u.IP
	if ip == "" {
		ip = p.detectIP(ctx)
	}
	return model.Job{Login: u.Login, JWT: s.JWT, VehicleID: vid, CardID: cid, IP: ip, Level: u.Level,
		ParkingID: p.parking(), Day: day, Start: start, End: end}, nil
}

// detectIP returns the detected public IP, or "" (with a warning) when there
// is no lookup or it fails; a failed lookup never fails the reservation.
func (p *Preparer) detectIP(ctx context.Context) string {
	if p.PublicIP == nil {
		slog.Warn("public_ip_lookup_failed", "err", "no lookup configured")
		return ""
	}
	ip, err := p.PublicIP(ctx)
	if err != nil {
		slog.Warn("public_ip_lookup_failed", "err", err.Error())
		return ""
	}
	return ip
}
