package store

// AddDayOff marks day as a day off (vacation) for the user; adding it twice is a no-op.
func (s *Store) AddDayOff(login, day string) error {
	_, err := s.db.Exec(`INSERT INTO days_off(login,day) VALUES(?,?) ON CONFLICT(login,day) DO NOTHING`, login, day)
	return err
}

// RemoveDayOff unmarks a day off.
func (s *Store) RemoveDayOff(login, day string) error {
	_, err := s.db.Exec(`DELETE FROM days_off WHERE login=? AND day=?`, login, day)
	return err
}

// ListDaysOff returns the user's days off, oldest first.
func (s *Store) ListDaysOff(login string) ([]string, error) {
	rows, err := s.db.Query(`SELECT day FROM days_off WHERE login=? ORDER BY day`, login)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DaysOffOn returns the logins that have day marked as a day off.
func (s *Store) DaysOffOn(day string) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT login FROM days_off WHERE day=?`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out[l] = true
	}
	return out, rows.Err()
}
