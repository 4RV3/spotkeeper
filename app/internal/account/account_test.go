package account

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

type fakePC struct {
	logins, vehCalls, cardCalls int
	exp                         time.Time
	vs                          []model.Vehicle
	cs                          []model.Card
	parkings                    []string
	v2Err                       error // returned by v2 (parkingID != "") calls
}

func (f *fakePC) Login(_ context.Context, l, p string) (model.Session, error) {
	f.logins++
	return model.Session{JWT: "jwt", Exp: f.exp}, nil
}
func (f *fakePC) Vehicles(_ context.Context, _, parkingID string) ([]model.Vehicle, error) {
	f.vehCalls++
	f.parkings = append(f.parkings, parkingID)
	if parkingID != "" && f.v2Err != nil {
		return nil, f.v2Err
	}
	return f.vs, nil
}
func (f *fakePC) CreditCards(_ context.Context, _, parkingID string) ([]model.Card, error) {
	f.cardCalls++
	f.parkings = append(f.parkings, parkingID)
	if parkingID != "" && f.v2Err != nil {
		return nil, f.v2Err
	}
	return f.cs, nil
}

type fakeSec struct{ keys []string }

func (f *fakeSec) Get(_ context.Context, k string) (string, error) {
	f.keys = append(f.keys, k)
	return "pw", nil
}

func newFake() *fakePC {
	return &fakePC{
		exp: time.Now().Add(time.Hour),
		vs:  []model.Vehicle{{ID: "v1", Plate: "wx1234a"}},
		cs:  []model.Card{{ID: "c0", Status: 0, ClientHash: "h"}, {ID: "c1", Status: 1, ClientHash: "h1"}},
	}
}

var user = model.User{Login: "jan.kowalski@example.com", Plate: "WX 1234A", Level: "podziemny"}

func TestPrepare(t *testing.T) {
	pc := newFake()
	sec := &fakeSec{}
	j, err := NewPreparer(pc, sec, "P1").Prepare(context.Background(), user, "2026-10-25")
	if err != nil {
		t.Fatal(err)
	}
	if j.VehicleID != "v1" || j.CardID != "c1" || j.JWT != "jwt" || j.IP != "" || j.ParkingID != "P1" || j.Day != "2026-10-25" {
		t.Fatalf("%+v", j)
	}
	if j.Start.UTC().Format(time.RFC3339) != "2026-10-24T22:00:00Z" || j.End.UTC().Format(time.RFC3339) != "2026-10-25T23:00:00Z" {
		t.Fatalf("%v %v", j.Start, j.End)
	}
	if len(sec.keys) != 1 || sec.keys[0] != "PCjan.kowalski@example.com" {
		t.Fatalf("%v", sec.keys)
	}
}

func TestSessionCached(t *testing.T) {
	pc := newFake()
	p := NewPreparer(pc, &fakeSec{}, "P1")
	for i := 0; i < 2; i++ {
		if _, err := p.Prepare(context.Background(), user, "2026-10-25"); err != nil {
			t.Fatal(err)
		}
	}
	if pc.logins != 1 {
		t.Fatalf("logins %d", pc.logins)
	}
	p.Invalidate(user.Login)
	p.Prepare(context.Background(), user, "2026-10-25")
	if pc.logins != 2 {
		t.Fatalf("after invalidate %d", pc.logins)
	}
	pc.exp = time.Now().Add(2 * time.Minute) // too close to expiry: relogin each time
	p.Invalidate(user.Login)
	p.Prepare(context.Background(), user, "2026-10-25")
	p.Prepare(context.Background(), user, "2026-10-25")
	if pc.logins != 4 {
		t.Fatalf("near-expiry %d", pc.logins)
	}
}

func TestNoUsableCard(t *testing.T) {
	pc := newFake()
	pc.cs = []model.Card{{ID: "c0", Status: 0, ClientHash: "h"}, {ID: "c2", Status: 1}}
	_, err := NewPreparer(pc, &fakeSec{}, "P").Prepare(context.Background(), user, "2026-10-25")
	if err == nil || !strings.Contains(err.Error(), "no usable card") {
		t.Fatalf("%v", err)
	}
}

func TestPlateMissing(t *testing.T) {
	pc := newFake()
	pc.vs = []model.Vehicle{{ID: "v9", Plate: "XX1"}}
	_, err := NewPreparer(pc, &fakeSec{}, "P").Prepare(context.Background(), user, "2026-10-25")
	if err == nil || !strings.Contains(err.Error(), "vehicle not found") {
		t.Fatalf("%v", err)
	}
}

func TestVerify(t *testing.T) {
	pc := newFake()
	p := NewPreparer(pc, &fakeSec{}, "P")
	if err := p.Verify(context.Background(), user.Login, "pw", "wx 1234 a"); err != nil {
		t.Fatal(err)
	}
	if err := p.Verify(context.Background(), user.Login, "pw", "ZZ"); err == nil || !strings.Contains(err.Error(), "vehicle not found") {
		t.Fatalf("%v", err)
	}
}

func TestPrepareCachesVehicleAndCard(t *testing.T) {
	ctx := context.Background()
	pc := newFake()
	p := NewPreparer(pc, &fakeSec{}, "P1")
	for i := 0; i < 2; i++ {
		j, err := p.Prepare(ctx, user, "2026-10-25")
		if err != nil || j.VehicleID != "v1" || j.CardID != "c1" {
			t.Fatalf("%+v %v", j, err)
		}
	}
	if pc.logins != 1 || pc.vehCalls != 1 || pc.cardCalls != 1 {
		t.Fatalf("%d %d %d", pc.logins, pc.vehCalls, pc.cardCalls)
	}
	p.Invalidate(user.Login)
	p.Prepare(ctx, user, "2026-10-25")
	if pc.logins != 2 || pc.vehCalls != 2 || pc.cardCalls != 2 {
		t.Fatalf("after invalidate %d %d %d", pc.logins, pc.vehCalls, pc.cardCalls)
	}
	pc.vs = append(pc.vs, model.Vehicle{ID: "v2", Plate: "KR1"})
	u2 := user
	u2.Plate = "kr 1"
	j, err := p.Prepare(ctx, u2, "2026-10-25")
	if err != nil || j.VehicleID != "v2" || pc.logins != 2 || pc.vehCalls != 3 {
		t.Fatalf("plate change %+v %v %d %d", j, err, pc.logins, pc.vehCalls)
	}
}

func TestSessionCachedNoVehicle(t *testing.T) {
	pc := newFake()
	sec := &fakeSec{}
	p := NewPreparer(pc, sec, "P1")
	for i := 0; i < 2; i++ {
		jwt, err := p.Session(context.Background(), "op.x@example.com")
		if err != nil || jwt != "jwt" {
			t.Fatalf("%q %v", jwt, err)
		}
	}
	if pc.logins != 1 || pc.vehCalls != 0 || pc.cardCalls != 0 {
		t.Fatalf("logins %d veh %d card %d", pc.logins, pc.vehCalls, pc.cardCalls)
	}
	if len(sec.keys) != 1 || sec.keys[0] != "PCop.x@example.com" {
		t.Fatalf("%v", sec.keys)
	}
	pc.exp = time.Now().Add(time.Minute)
	p.Invalidate("op.x@example.com")
	p.Session(context.Background(), "op.x@example.com")
	p.Session(context.Background(), "op.x@example.com")
	if pc.logins != 3 {
		t.Fatalf("near-expiry logins %d", pc.logins)
	}
}

func TestVerifyLogin(t *testing.T) {
	pc := newFake()
	p := NewPreparer(pc, &fakeSec{}, "P1")
	if err := p.VerifyLogin(context.Background(), "a.b@example.com", "x"); err != nil {
		t.Fatal(err)
	}
	if pc.logins != 1 || pc.vehCalls != 0 || pc.cardCalls != 0 {
		t.Fatalf("%d %d %d", pc.logins, pc.vehCalls, pc.cardCalls)
	}
}

func TestPrepareUsesParkingIDFunc(t *testing.T) {
	p := NewPreparer(newFake(), &fakeSec{}, "OLD")
	id := "NEW1"
	p.ParkingID = func() string { return id }
	j, err := p.Prepare(context.Background(), user, "2026-10-25")
	if err != nil || j.ParkingID != "NEW1" {
		t.Fatalf("%v %+v", err, j)
	}
	id = "NEW2"
	j, _ = p.Prepare(context.Background(), user, "2026-10-25")
	if j.ParkingID != "NEW2" {
		t.Fatalf("%+v", j)
	}
}

func TestStaticSecret(t *testing.T) {
	s := Static("Jan", "pw")
	if v, err := s.Get(context.Background(), "PCjan"); err != nil || v != "pw" {
		t.Fatalf("%q %v", v, err)
	}
	if v, err := s.Get(context.Background(), "PCola"); err == nil || v != "" {
		t.Fatalf("%q %v", v, err)
	}
}

func TestPrepareUsesPublicIPWhenEmpty(t *testing.T) {
	p := NewPreparer(newFake(), &fakeSec{}, "P1")
	p.PublicIP = func(context.Context) (string, error) { return "203.0.113.7", nil }
	j, err := p.Prepare(context.Background(), user, "2026-10-25")
	if err != nil || j.IP != "203.0.113.7" {
		t.Fatalf("%+v %v", j, err)
	}
}

func TestPrepareKeepsConfiguredIP(t *testing.T) {
	p := NewPreparer(newFake(), &fakeSec{}, "P1")
	p.PublicIP = func(context.Context) (string, error) { t.Fatal("lookup must not run"); return "", nil }
	u := user
	u.IP = "198.51.100.1"
	j, err := p.Prepare(context.Background(), u, "2026-10-25")
	if err != nil || j.IP != "198.51.100.1" {
		t.Fatalf("%+v %v", j, err)
	}
}

func TestPrepareLookupFailureUsesEmpty(t *testing.T) {
	p := NewPreparer(newFake(), &fakeSec{}, "P1")
	p.PublicIP = func(context.Context) (string, error) { return "", errors.New("boom") }
	j, err := p.Prepare(context.Background(), user, "2026-10-25")
	if err != nil || j.IP != "" {
		t.Fatalf("%+v %v", j, err)
	}
}

func TestResolvePassesParkingID(t *testing.T) {
	pc := &fakePC{exp: time.Now().Add(time.Hour),
		vs: []model.Vehicle{{ID: "v1", Plate: "WX1"}},
		cs: []model.Card{{ID: "c1", Status: 1, ClientHash: "h"}}}
	p := NewPreparer(pc, Static("u", "pw"), "P1")
	if _, err := p.Prepare(context.Background(), model.User{Login: "u", Plate: "WX1", IP: "1.2.3.4"}, "2026-10-25"); err != nil {
		t.Fatal(err)
	}
	if len(pc.parkings) != 2 || pc.parkings[0] != "P1" || pc.parkings[1] != "P1" {
		t.Fatal(pc.parkings)
	}
}

func TestResolveFallsBackToV1(t *testing.T) {
	pc := &fakePC{exp: time.Now().Add(time.Hour), v2Err: errors.New("v2 down"),
		vs: []model.Vehicle{{ID: "v1", Plate: "WX1"}},
		cs: []model.Card{{ID: "c1", Status: 1, ClientHash: "h"}}}
	p := NewPreparer(pc, Static("u", "pw"), "P1")
	j, err := p.Prepare(context.Background(), model.User{Login: "u", Plate: "WX1", IP: "1.2.3.4"}, "2026-10-25")
	if err != nil || j.VehicleID != "v1" || j.CardID != "c1" {
		t.Fatal(j, err)
	}
	if strings.Join(pc.parkings, ",") != "P1,,P1," {
		t.Fatal(pc.parkings)
	}
}
