package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func user(login, sched string, active bool) model.User {
	return model.User{Login: login, Plate: "WX1", Schedule: sched, Active: active}
}

func TestUserRoundTrip(t *testing.T) {
	s := open(t)
	if _, err := s.GetUser("x"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	u := model.User{Login: "a", Plate: "P", Level: "podziemny", IP: "1.2.3.4", Schedule: "1,3", Active: true}
	if err := s.UpsertUser(u); err != nil {
		t.Fatal(err)
	}
	u.Plate = "Q"
	if err := s.UpsertUser(u); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetUser("a")
	if err != nil || got != u {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestWeekdayNoPrefixMatch(t *testing.T) {
	s := open(t)
	s.UpsertUser(user("a", "1,2", true))
	s.UpsertUser(user("b", "1,3", true))
	s.UpsertUser(user("c", "3", false))
	s.UpsertUser(user("d", "11", true))
	cases := map[int]int{1: 2, 2: 1, 3: 1, 11: 1, 12: 0, 4: 0}
	for wd, want := range cases {
		us, err := s.UsersForWeekday(wd)
		if err != nil || len(us) != want {
			t.Fatalf("wd %d: got %d want %d (%v)", wd, len(us), want, err)
		}
	}
}

func TestDeleteCascades(t *testing.T) {
	s := open(t)
	s.UpsertUser(user("a", "1", true))
	s.UpsertUser(user("b", "1", true))
	s.UpsertTask(model.Task{Login: "a", Day: "2026-10-25", Kind: model.KindFuture, Status: model.StatusPending})
	s.UpsertTask(model.Task{Login: "b", Day: "2026-10-25", Kind: model.KindFuture, Status: model.StatusPending})
	if err := s.DeleteUser("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetUser("a"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	if ts, _ := s.ListTasks("a", 10); len(ts) != 0 {
		t.Fatal("tasks remain")
	}
	if ts, _ := s.ListTasks("b", 10); len(ts) != 1 {
		t.Fatal("b tasks lost")
	}
}

func TestTasksUpsertAndList(t *testing.T) {
	s := open(t)
	s.UpsertUser(user("a", "1", true))
	now := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	s.UpsertTask(model.Task{Login: "a", Day: "2026-10-25", Kind: model.KindFuture, Status: model.StatusPending, UpdatedAt: now})
	s.UpsertTask(model.Task{Login: "a", Day: "2026-10-25", Kind: model.KindFuture, Status: model.StatusPaid, SpotID: "S1", ReservationID: "R", PaymentID: "P", Attempts: 3, Detail: "ok", UpdatedAt: now})
	s.UpsertTask(model.Task{Login: "a", Day: "2026-10-25", Kind: model.KindPast, Status: model.StatusFailed, UpdatedAt: now})
	s.UpsertTask(model.Task{Login: "a", Day: "2026-10-26", Kind: model.KindFuture, Status: model.StatusPending, UpdatedAt: now})
	ts, err := s.ListTasks("a", 10)
	if err != nil || len(ts) != 3 {
		t.Fatalf("%d %v", len(ts), err)
	}
	if ts[0].Day != "2026-10-26" {
		t.Fatalf("order %+v", ts)
	}
	var found model.Task
	for _, x := range ts {
		if x.Day == "2026-10-25" && x.Kind == model.KindFuture {
			found = x
		}
	}
	want := model.Task{Login: "a", Day: "2026-10-25", Kind: model.KindFuture, Status: model.StatusPaid, SpotID: "S1", ReservationID: "R", PaymentID: "P", Attempts: 3, Detail: "ok", UpdatedAt: now}
	if !found.UpdatedAt.Equal(now) {
		t.Fatalf("time %v", found.UpdatedAt)
	}
	found.UpdatedAt = now
	if found != want {
		t.Fatalf("%+v", found)
	}
	if ts, _ := s.ListTasks("a", 2); len(ts) != 2 {
		t.Fatal("limit")
	}
}

func TestConcurrentUpsertTask(t *testing.T) {
	s := open(t)
	s.UpsertUser(user("a", "1", true))
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if err := s.UpsertTask(model.Task{Login: "a", Day: fmt.Sprintf("2026-10-%02d", i+1), Kind: model.KindFuture, Status: model.StatusRunning, Attempts: j}); err != nil {
					errs <- err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if ts, _ := s.ListTasks("a", 100); len(ts) != 20 {
		t.Fatalf("tasks %d", len(ts))
	}
}

func TestListUsers(t *testing.T) {
	s := open(t)
	s.UpsertUser(user("b", "1", true))
	s.UpsertUser(user("a", "2", false))
	us, err := s.ListUsers()
	if err != nil || len(us) != 2 || us[0].Login != "a" || us[1].Login != "b" || us[0].Active {
		t.Fatalf("%+v %v", us, err)
	}
}

func TestMigrateOldSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE users (
 login TEXT PRIMARY KEY, plate TEXT NOT NULL DEFAULT '', level TEXT NOT NULL DEFAULT '',
 ip TEXT NOT NULL DEFAULT '', schedule TEXT NOT NULL DEFAULT '', active INTEGER NOT NULL DEFAULT 0,
 chat_id INTEGER NOT NULL DEFAULT 0, link_code TEXT NOT NULL DEFAULT '');
INSERT INTO users(login,plate) VALUES('old','P');`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ { // second open: migration is idempotent
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		u, err := s.GetUser("old")
		if err != nil || u.Plate != "P" {
			t.Fatalf("%+v %v", u, err)
		}
		s.Close()
	}
}

func TestSettings(t *testing.T) {
	s := open(t)
	if _, err := s.GetSetting("monitor_login"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	for _, v := range []string{"a.b@example.com", "c.d@example.com"} {
		if err := s.SetSetting("monitor_login", v); err != nil {
			t.Fatal(err)
		}
		if got, err := s.GetSetting("monitor_login"); err != nil || got != v {
			t.Fatalf("%q %v", got, err)
		}
	}
}

func TestSettingsMigrationOldDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE users (login TEXT PRIMARY KEY, plate TEXT NOT NULL DEFAULT '', level TEXT NOT NULL DEFAULT '',
 ip TEXT NOT NULL DEFAULT '', schedule TEXT NOT NULL DEFAULT '', active INTEGER NOT NULL DEFAULT 0,
 chat_id INTEGER NOT NULL DEFAULT 0, link_code TEXT NOT NULL DEFAULT '');`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetSetting("k", "v"); err != nil {
			t.Fatal(err)
		}
		if v, err := s.GetSetting("k"); err != nil || v != "v" {
			t.Fatalf("%q %v", v, err)
		}
		s.Close()
	}
}

func TestGetTaskAndListByStatus(t *testing.T) {
	s := open(t)
	if err := s.UpsertUser(user("a", "1", true)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTask("a", "2026-10-26", "future"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	now := time.Now()
	for i, tk := range []model.Task{
		{Login: "a", Day: "d1", Kind: "future", Status: model.StatusUnconfirmed, PaymentID: "P1", Detail: "x", UpdatedAt: now.Add(-10 * time.Minute)},
		{Login: "a", Day: "d2", Kind: "past", Status: model.StatusUnconfirmed, PaymentID: "P2", UpdatedAt: now.Add(-time.Hour)},
		{Login: "a", Day: "d3", Kind: "past", Status: model.StatusPaid, PaymentID: "P3", UpdatedAt: now},
	} {
		if err := s.UpsertTask(tk); err != nil {
			t.Fatal(i, err)
		}
	}
	g, err := s.GetTask("a", "d1", "future")
	if err != nil || g.PaymentID != "P1" || g.Detail != "x" || g.Status != model.StatusUnconfirmed {
		t.Fatalf("%+v %v", g, err)
	}
	l, err := s.ListTasksByStatus(model.StatusUnconfirmed, now.Add(-35*time.Minute))
	if err != nil || len(l) != 1 || l[0].Day != "d1" {
		t.Fatalf("%+v %v", l, err)
	}
	l, _ = s.ListTasksByStatus(model.StatusUnconfirmed, now.Add(-2*time.Hour))
	if len(l) != 2 || l[0].Day != "d2" {
		t.Fatalf("%+v", l)
	}
}
