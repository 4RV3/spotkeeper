// Package logging builds the application slog logger: JSON on stdout.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Setup returns a JSON logger on stdout at level (debug|info|warn|error; default info).
func Setup(level string) *slog.Logger {
	return setup(level, os.Stdout)
}

func setup(level string, w io.Writer) *slog.Logger {
	lvl := new(slog.LevelVar)
	if level == "" || lvl.UnmarshalText([]byte(strings.ToLower(level))) != nil {
		lvl.Set(slog.LevelInfo)
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl}))
}
