package logger

import (
	"log/slog"
	"os"
	"sync/atomic"
)

// Level represents the logging verbosity level.
type Level int

const (
	LevelQuiet   Level = iota // only errors
	LevelNormal               // info + errors
	LevelVerbose              // debug + info + errors
)

var current atomic.Pointer[slog.Logger]

// Init initialises the global logger at the requested verbosity level.
func Init(level Level) {
	var slogLevel slog.Level
	switch level {
	case LevelQuiet:
		slogLevel = slog.LevelError
	case LevelVerbose:
		slogLevel = slog.LevelDebug
	default:
		slogLevel = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel})
	l := slog.New(handler)
	current.Store(l)
	slog.SetDefault(l)
}

// Reset sets the global logger to nil, forcing the next call to Get() to
// re-initialise it.  Intended for use in tests only.
func Reset() {
	current.Store(nil)
}

// Get returns the global logger instance, initialising it at normal level if it
// has not already been initialised.
func Get() *slog.Logger {
	if l := current.Load(); l != nil {
		return l
	}
	Init(LevelNormal)
	return current.Load()
}
