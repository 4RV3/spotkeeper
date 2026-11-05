package sessionkey

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.key")
	first, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 {
		t.Fatalf("key len %d want 32", len(first))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes.TrimSpace(raw)) != 64 {
		t.Fatalf("file holds %d bytes want 64 hex chars", len(raw))
	}
	second, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("key changed between calls")
	}
}

func TestLoadOrCreatePerm0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.key")
	if _, err := LoadOrCreate(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Fatalf("mode %o want 600", mode)
	}
}

func TestLoadOrCreateRejectsShortFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.key")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err == nil {
		t.Fatal("want error for short key file")
	}
}
