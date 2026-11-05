package engine

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"

	"spotkeeper/internal/model"
)

func none(string) bool { return false }

func TestSelectSpotLevel(t *testing.T) {
	spots := []model.Spot{{ID: "a", Level: "podziemny (-1,-2)"}, {ID: "b", Level: "nadziemny (+1,+2,+3)"}}
	for i := 0; i < 20; i++ {
		s, ok := SelectSpot(spots, "Podziemny", none, rand.New(rand.NewPCG(uint64(i), 1)))
		if !ok || s.ID != "a" {
			t.Fatalf("got %v %v", s, ok)
		}
	}
}

func TestSelectSpotExcluded(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for _, sp := range []model.Spot{{ID: "c", Charger: true}, {ID: "h", Handicapped: true}, {ID: "m", MiniEV: true}} {
		s, ok := SelectSpot([]model.Spot{sp}, "", none, r)
		if ok || s.ID != "" {
			t.Fatalf("%v selected", sp)
		}
	}
}

func TestSelectSpotTakenAndRandom(t *testing.T) {
	spots := []model.Spot{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	seen := map[string]bool{}
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 200; i++ {
		s, ok := SelectSpot(spots, "", func(id string) bool { return id == "a" }, r)
		if !ok || s.ID == "a" {
			t.Fatalf("got %v %v", s, ok)
		}
		seen[s.ID] = true
	}
	if !seen["b"] || !seen["c"] {
		t.Fatalf("not random: %v", seen)
	}
	if _, ok := SelectSpot(spots, "", func(string) bool { return true }, r); ok {
		t.Fatal("all taken")
	}
	if _, ok := SelectSpot(nil, "", nil, r); ok {
		t.Fatal("empty")
	}
}

func TestClaims(t *testing.T) {
	c := NewClaims()
	if !c.Claim("d", "x") || c.Claim("d", "x") {
		t.Fatal("claim semantics")
	}
	if !c.Claim("e", "x") {
		t.Fatal("different day must be independent")
	}
	c.Release("d", "x")
	if !c.Claim("d", "x") {
		t.Fatal("after release")
	}
}

func TestClaimsConcurrent(t *testing.T) {
	c := NewClaims()
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.Claim("d", "x") {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins=%d", wins)
	}
}
