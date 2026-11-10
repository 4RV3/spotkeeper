package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"spotkeeper/internal/model"
)

const (
	parkingsTTL    = 10 * time.Minute
	parkingsErrTTL = time.Minute
	levelsTTL      = time.Hour
	maxLevelLen    = 100
)

type parkingsCache struct {
	list []model.Parking
	err  error
	at   time.Time
	ttl  time.Duration
}

type levelsCache struct {
	levels []string
	at     time.Time
}

// parkingOpt is one entry of the parking <select>.
type parkingOpt struct {
	ID, Label string
	Selected  bool
}

// parkingLabel renders "Name — City, Street No".
func parkingLabel(p model.Parking) string {
	var addr []string
	for _, a := range []string{p.City, p.Street} {
		if a = strings.TrimSpace(a); a != "" {
			addr = append(addr, a)
		}
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = p.ID
	}
	if len(addr) == 0 {
		return name
	}
	return name + " — " + strings.Join(addr, ", ")
}

// parkings returns the user's parkings, cached for 10 minutes (a failure for a
// minute, so a broken service is not hit on every page view).
func (s *Server) parkings(r *http.Request) ([]model.Parking, error) {
	if s.d.Parkings == nil {
		return nil, nil
	}
	now := s.d.Now()
	s.cacheMu.Lock()
	c := s.parkingsC
	s.cacheMu.Unlock()
	if !c.at.IsZero() && now.Sub(c.at) < c.ttl {
		return c.list, c.err
	}
	list, err := s.d.Parkings(r.Context())
	c = parkingsCache{list: list, err: err, at: now, ttl: parkingsTTL}
	if err != nil {
		c.ttl = parkingsErrTTL
		// the panel shows only a generic message; keep the cause in the log
		slog.Warn("parkings_list_failed", "err", err.Error())
	}
	s.cacheMu.Lock()
	s.parkingsC = c
	s.cacheMu.Unlock()
	return list, err
}

// parkingOpts builds the select options (sorted by name). The currently stored
// parking is always present, even when the list failed or does not contain it.
func parkingOpts(list []model.Parking, current string) []parkingOpt {
	sorted := append([]model.Parking(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return strings.ToLower(parkingLabel(sorted[i])) < strings.ToLower(parkingLabel(sorted[j]))
	})
	var out []parkingOpt
	found := false
	for _, p := range sorted {
		sel := p.ID == current
		found = found || sel
		out = append(out, parkingOpt{p.ID, parkingLabel(p), sel})
	}
	switch {
	case current == "":
		out = append([]parkingOpt{{"", "— wybierz parking —", true}}, out...)
	case !found:
		out = append([]parkingOpt{{current, current, true}}, out...)
	}
	return out
}

// levels returns the cached level list of a parking (1 h).
func (s *Server) levels(r *http.Request, parkingID string) ([]string, error) {
	now := s.d.Now()
	s.cacheMu.Lock()
	c, ok := s.levelsC[parkingID]
	s.cacheMu.Unlock()
	if ok && now.Sub(c.at) < levelsTTL {
		return c.levels, nil
	}
	levels, err := s.d.Levels(r.Context(), parkingID)
	if err != nil {
		return nil, err
	}
	if levels == nil {
		levels = []string{}
	}
	s.cacheMu.Lock()
	if s.levelsC == nil {
		s.levelsC = map[string]levelsCache{}
	}
	s.levelsC[parkingID] = levelsCache{levels, now}
	s.cacheMu.Unlock()
	return levels, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// levelsHandler serves GET /settings/levels?parking=<guid> as {"levels":[...]}.
func (s *Server) levelsHandler(w http.ResponseWriter, r *http.Request, _ string) {
	p := strings.TrimSpace(r.URL.Query().Get("parking"))
	if !guidRe.MatchString(p) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid parking"})
		return
	}
	if s.d.Levels == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "levels unavailable"})
		return
	}
	levels, err := s.levels(r, p)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "levels lookup failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"levels": levels})
}

// validLevel accepts "" (any level) or the service's own level text: printable,
// at most maxLevelLen characters.
func validLevel(l string) bool {
	if utf8.RuneCountInString(l) > maxLevelLen || !utf8.ValidString(l) {
		return false
	}
	for _, r := range l {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// levelOpt is one entry of the level <select>.
type levelOpt struct {
	Value, Label string
	Selected     bool
}

const levelUnknownSuffix = " (zapisany, nie występuje na parkingu)"

// levelOptions builds the level select. A stored value that is not an exact
// option but a case-insensitive substring of exactly one fetched level selects
// that level (the next save stores its full text); a value matching none (or
// several) is kept as one extra option labelled as not present on the parking.
// With no fetched list the stored value is shown as is.
func levelOptions(fetched []string, stored string) []levelOpt {
	stored = strings.TrimSpace(stored)
	out := []levelOpt{{"", "dowolny", stored == ""}}
	sel := ""
	extra := ""
	if stored != "" {
		switch {
		case len(fetched) == 0:
			extra = stored
		default:
			var hits []string
			for _, l := range fetched {
				if l == stored {
					sel, hits = l, nil
					break
				}
				if strings.Contains(strings.ToLower(l), strings.ToLower(stored)) {
					hits = append(hits, l)
				}
			}
			if sel == "" && len(hits) == 1 {
				sel = hits[0]
			}
			if sel == "" {
				extra = stored
			}
		}
	}
	for _, l := range fetched {
		out = append(out, levelOpt{l, l, l == sel})
	}
	if extra != "" {
		label := extra
		if len(fetched) > 0 {
			label += levelUnknownSuffix
		}
		out = append(out, levelOpt{extra, label, true})
	}
	return out
}

// cachedLevels returns the level list of a parking only if it is cached.
func (s *Server) cachedLevels(parkingID string) []string {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	c, ok := s.levelsC[parkingID]
	if !ok || s.d.Now().Sub(c.at) >= levelsTTL {
		return nil
	}
	return c.levels
}
