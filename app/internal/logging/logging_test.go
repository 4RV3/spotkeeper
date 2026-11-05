package logging

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLevelFiltersAndOutputIsJSON(t *testing.T) {
	var b bytes.Buffer
	lg := setup("warn", &b)
	lg.Info("hidden")
	lg.Warn("shown", "k", "v")
	var rec map[string]any
	if err := json.Unmarshal(b.Bytes(), &rec); err != nil {
		t.Fatalf("not one JSON line: %q %v", b.String(), err)
	}
	if rec["msg"] != "shown" || rec["k"] != "v" {
		t.Fatalf("record %v", rec)
	}
}

func TestBadLevelFallsBackToInfo(t *testing.T) {
	var b bytes.Buffer
	setup("bogus", &b).Info("x")
	if b.Len() == 0 {
		t.Fatal("info suppressed")
	}
}
