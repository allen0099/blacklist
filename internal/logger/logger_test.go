package logger_test

import (
"log/slog"
"testing"

"github.com/allen0099/blacklist/internal/logger"
)

func TestInit_Normal(t *testing.T) {
logger.Init(logger.LevelNormal)
l := logger.Get()
if l == nil {
t.Fatal("expected non-nil logger after Init(LevelNormal)")
}
if !l.Enabled(nil, slog.LevelInfo) {
t.Error("LevelNormal should enable Info messages")
}
if l.Enabled(nil, slog.LevelDebug) {
t.Error("LevelNormal should suppress Debug messages")
}
}

func TestInit_Verbose(t *testing.T) {
logger.Init(logger.LevelVerbose)
l := logger.Get()
if !l.Enabled(nil, slog.LevelDebug) {
t.Error("LevelVerbose should enable Debug messages")
}
}

func TestInit_Quiet(t *testing.T) {
logger.Init(logger.LevelQuiet)
l := logger.Get()
if l.Enabled(nil, slog.LevelInfo) {
t.Error("LevelQuiet should suppress Info messages")
}
if !l.Enabled(nil, slog.LevelError) {
t.Error("LevelQuiet should enable Error messages")
}
}

func TestGet_AutoInit(t *testing.T) {
// Reset internal state so Get() is forced to auto-initialise.
logger.Reset()
l := logger.Get()
if l == nil {
t.Fatal("Get() should never return nil")
}
// After auto-init, Info should be enabled (LevelNormal is the default).
if !l.Enabled(nil, slog.LevelInfo) {
t.Error("auto-initialised logger should enable Info messages")
}
}

func TestReset(t *testing.T) {
logger.Init(logger.LevelVerbose)
logger.Reset()
// After reset, Get() re-initialises at LevelNormal.
l := logger.Get()
if l == nil {
t.Fatal("Get() should return non-nil after Reset()")
}
}
