package engine

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"spotkeeper/internal/model"
)

// turnstile-service context_api.py accepts requestId only as [A-Za-z0-9_.:-]{1,128};
// a login with '@' was rejected with invalid_request.
var serviceRequestID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

func TestRequestIDAcceptedByTurnstileService(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: spots3, status: 2}}
	a.reserveFn = func(n int, _, _ string) (string, error) {
		if n < 2 {
			return "", &model.APIError{Code: 5}
		}
		return "R1", nil
	}
	tok := &fakeTok{}
	e := newEng(a, tok, cfg(), NewClaims())
	recSleep(e)
	r := e.Run(context.Background(), job("jan.kowalski@example.com"))
	if r.Status != model.StatusPaid || len(tok.ids) != 3 {
		t.Fatalf("result %+v ids %v", r, tok.ids)
	}
	seen := map[string]bool{}
	for _, id := range tok.ids {
		if !serviceRequestID.MatchString(id) {
			t.Fatalf("requestId %q rejected by turnstile-service", id)
		}
		if strings.Contains(id, "kowalski") || strings.Contains(id, "@") {
			t.Fatalf("requestId %q leaks the login", id)
		}
		if seen[id] {
			t.Fatalf("duplicate requestId %q", id)
		}
		seen[id] = true
	}

	// a second run gets a different run id (unique per execution)
	tok2 := &fakeTok{}
	a2 := &scriptAPI{fakeAPI: fakeAPI{spots: spots3, status: 2}}
	e2 := newEng(a2, tok2, cfg(), NewClaims())
	recSleep(e2)
	e2.Run(context.Background(), job("jan.kowalski@example.com"))
	if len(tok2.ids) == 0 || tok2.ids[0] == tok.ids[0] {
		t.Fatalf("run ids not unique: %v vs %v", tok2.ids, tok.ids)
	}
}
