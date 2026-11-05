package sessionkey

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

const keyBytes = 32

// LoadOrCreate returns the 32-byte session key stored at path as 64 hex chars.
// When path does not exist it generates a new key, writes it with mode 0600 and returns it.
func LoadOrCreate(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		key, err := hex.DecodeString(strings.TrimSpace(string(data)))
		if err != nil || len(key) != keyBytes {
			return nil, fmt.Errorf("sessionkey: %s must hold %d hex chars", path, 2*keyBytes)
		}
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, keyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)), 0600); err != nil {
		return nil, err
	}
	return key, nil
}
