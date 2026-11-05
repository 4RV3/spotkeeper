// Package store persists the user, tasks and settings in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"spotkeeper/internal/model"
)

// Store is a SQLite-backed repository.
type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS users (
 login TEXT PRIMARY KEY, plate TEXT NOT NULL DEFAULT '', level TEXT NOT NULL DEFAULT '',
 ip TEXT NOT NULL DEFAULT '', schedule TEXT NOT NULL DEFAULT '', active INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS tasks (
 login TEXT NOT NULL REFERENCES users(login) ON DELETE CASCADE, day TEXT NOT NULL, kind TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT '', spot_id TEXT NOT NULL DEFAULT '', reservation_id TEXT NOT NULL DEFAULT '',
 payment_id TEXT NOT NULL DEFAULT '', attempts INTEGER NOT NULL DEFAULT 0, detail TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL DEFAULT '', PRIMARY KEY (login, day, kind));
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS days_off (
 login TEXT NOT NULL REFERENCES users(login) ON DELETE CASCADE, day TEXT NOT NULL, PRIMARY KEY (login, day));`

// Open opens (creating if needed) the database at path.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// migrate adds columns missing from databases created by older versions.
func migrate(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='skip_holidays'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		// users skip holidays by default
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN skip_holidays INTEGER NOT NULL DEFAULT 1`); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UpsertUser inserts or updates a user.
func (s *Store) UpsertUser(u model.User) error {
	_, err := s.db.Exec(`INSERT INTO users(login,plate,level,ip,schedule,active,skip_holidays) VALUES(?,?,?,?,?,?,?)
ON CONFLICT(login) DO UPDATE SET plate=excluded.plate, level=excluded.level, ip=excluded.ip,
 schedule=excluded.schedule, active=excluded.active, skip_holidays=excluded.skip_holidays`,
		u.Login, u.Plate, u.Level, u.IP, u.Schedule, b2i(u.Active), b2i(u.SkipHolidays))
	return err
}

const userCols = `login,plate,level,ip,schedule,active,skip_holidays`

type scanner interface{ Scan(...any) error }

func scanUser(r scanner) (model.User, error) {
	var u model.User
	var a, sh int
	err := r.Scan(&u.Login, &u.Plate, &u.Level, &u.IP, &u.Schedule, &a, &sh)
	u.Active, u.SkipHolidays = a != 0, sh != 0
	return u, err
}

// GetUser returns a user or model.ErrNotFound.
func (s *Store) GetUser(login string) (model.User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE login=?`, login))
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, model.ErrNotFound
	}
	return u, err
}

// DeleteUser removes a user with its tasks and days off.
func (s *Store) DeleteUser(login string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM tasks WHERE login=?`, `DELETE FROM days_off WHERE login=?`, `DELETE FROM users WHERE login=?`} {
		if _, err := tx.Exec(q, login); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListUsers returns all users ordered by login.
func (s *Store) ListUsers() ([]model.User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY login`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UsersForWeekday returns active users whose schedule contains weekday wd (exact element match).
func (s *Store) UsersForWeekday(wd int) ([]model.User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users WHERE active=1 ORDER BY login`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		for _, p := range strings.Split(u.Schedule, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n == wd {
				out = append(out, u)
				break
			}
		}
	}
	return out, rows.Err()
}

// UpsertTask inserts or updates a task keyed by login+day+kind.
func (s *Store) UpsertTask(t model.Task) error {
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = time.Now()
	}
	_, err := s.db.Exec(`INSERT INTO tasks(login,day,kind,status,spot_id,reservation_id,payment_id,attempts,detail,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(login,day,kind) DO UPDATE SET status=excluded.status, spot_id=excluded.spot_id,
 reservation_id=excluded.reservation_id, payment_id=excluded.payment_id, attempts=excluded.attempts,
 detail=excluded.detail, updated_at=excluded.updated_at`,
		t.Login, t.Day, t.Kind, t.Status, t.SpotID, t.ReservationID, t.PaymentID, t.Attempts, t.Detail,
		t.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// ListTasks returns the latest tasks of a user, newest first.
func (s *Store) ListTasks(login string, limit int) ([]model.Task, error) {
	rows, err := s.db.Query(`SELECT login,day,kind,status,spot_id,reservation_id,payment_id,attempts,detail,updated_at
FROM tasks WHERE login=? ORDER BY day DESC, updated_at DESC LIMIT ?`, login, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Task
	for rows.Next() {
		var t model.Task
		var ts string
		if err := rows.Scan(&t.Login, &t.Day, &t.Kind, &t.Status, &t.SpotID, &t.ReservationID, &t.PaymentID, &t.Attempts, &t.Detail, &ts); err != nil {
			return nil, err
		}
		t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListTasksInRange returns a user's tasks for days from..to (YYYY-MM-DD, inclusive), oldest day first.
func (s *Store) ListTasksInRange(login, from, to string) ([]model.Task, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+` FROM tasks WHERE login=? AND day>=? AND day<=? ORDER BY day, updated_at`, login, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// FirstTaskDay returns the earliest day the user has a task for, or "" if none.
func (s *Store) FirstTaskDay(login string) (string, error) {
	var day sql.NullString
	err := s.db.QueryRow(`SELECT MIN(day) FROM tasks WHERE login=?`, login).Scan(&day)
	return day.String, err
}

// GetSetting returns a setting value or model.ErrNotFound.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", model.ErrNotFound
	}
	return v, err
}

// SetSetting stores a setting value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

const taskCols = `login,day,kind,status,spot_id,reservation_id,payment_id,attempts,detail,updated_at`

func scanTask(r scanner) (model.Task, error) {
	var t model.Task
	var ts string
	if err := r.Scan(&t.Login, &t.Day, &t.Kind, &t.Status, &t.SpotID, &t.ReservationID, &t.PaymentID, &t.Attempts, &t.Detail, &ts); err != nil {
		return t, err
	}
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, ts)
	return t, nil
}

// GetTask returns one task or model.ErrNotFound.
func (s *Store) GetTask(login, day, kind string) (model.Task, error) {
	t, err := scanTask(s.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE login=? AND day=? AND kind=?`, login, day, kind))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Task{}, model.ErrNotFound
	}
	return t, err
}

// ListTasksByStatus returns tasks with the given status updated at or after
// since, oldest first.
func (s *Store) ListTasksByStatus(status string, since time.Time) ([]model.Task, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+` FROM tasks WHERE status=?`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		if !t.UpdatedAt.Before(since) {
			out = append(out, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.Before(out[j].UpdatedAt) })
	return out, nil
}
