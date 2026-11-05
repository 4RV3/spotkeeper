// Package engine runs one reservation.
package engine

import (
	"math/rand/v2"
	"strings"
	"sync"

	"spotkeeper/internal/model"
)

// SelectSpot picks a random usable spot: not handicapped, charger or mini-EV,
// level (if set) a case-insensitive substring of the spot level, and not taken.
func SelectSpot(spots []model.Spot, level string, taken func(string) bool, rnd *rand.Rand) (model.Spot, bool) {
	level = strings.ToLower(strings.TrimSpace(level))
	var ok []model.Spot
	for _, s := range spots {
		if s.Handicapped || s.Charger || s.MiniEV {
			continue
		}
		if level != "" && !strings.Contains(strings.ToLower(s.Level), level) {
			continue
		}
		if taken != nil && taken(s.ID) {
			continue
		}
		ok = append(ok, s)
	}
	if len(ok) == 0 {
		return model.Spot{}, false
	}
	if rnd == nil {
		return ok[rand.IntN(len(ok))], true
	}
	return ok[rnd.IntN(len(ok))], true
}

// Claims tracks spots already claimed per day, safe for concurrent use.
type Claims struct {
	mu sync.Mutex
	m  map[string]struct{}
}

// NewClaims returns an empty Claims.
func NewClaims() *Claims {
	return &Claims{m: map[string]struct{}{}}
}

// Claim returns true if the spot was free for that day and is now claimed.
func (c *Claims) Claim(day, spotID string) bool {
	k := day + "\x00" + spotID
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[k]; ok {
		return false
	}
	c.m[k] = struct{}{}
	return true
}

// Release frees a claim.
func (c *Claims) Release(day, spotID string) {
	c.mu.Lock()
	delete(c.m, day+"\x00"+spotID)
	c.mu.Unlock()
}
