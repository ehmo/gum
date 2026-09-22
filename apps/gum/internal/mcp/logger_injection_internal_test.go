package mcp

import (
	"bytes"
	"log/slog"
	"testing"
)

// TestServerLoggerInjection proves the three §14.1 rule 2 behaviours for
// internal/mcp: SetLogger reaches the emission, a DiscardHandler silences the
// server, and an unset logger falls back to slog.Default(). The admin-tuning
// clamp is the trigger, because it is the only emission in this package and
// handlers.go reaches it through s.log().
func TestServerLoggerInjection(t *testing.T) {
	emit := func(l *slog.Logger) {
		s := &Server{}
		s.SetLogger(l)
		clampInt("GUM_TEST_KEY", "not-an-integer", 5, 1, 10, s.log())
	}

	var injected bytes.Buffer
	emit(slog.New(slog.NewTextHandler(&injected, nil)))
	if injected.Len() == 0 {
		t.Fatal("SetLogger did not reach the emission")
	}

	var leaked bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&leaked, &slog.HandlerOptions{Level: slog.LevelDebug})))

	emit(slog.New(slog.DiscardHandler))
	if leaked.Len() != 0 {
		t.Errorf("slog.New(slog.DiscardHandler) still emitted %q", leaked.String())
	}

	leaked.Reset()
	emit(nil)
	if leaked.Len() == 0 {
		t.Error("SetLogger(nil) must restore the slog.Default() fallback")
	}
}

// TestLoggerOrDefaultFallsBack covers the nil arm of the helper the tuning
// loaders use. They take the logger as a parameter rather than off a receiver,
// so an in-package caller can still pass nil.
func TestLoggerOrDefaultFallsBack(t *testing.T) {
	if got := loggerOrDefault(nil); got != slog.Default() {
		t.Error("loggerOrDefault(nil) must return slog.Default()")
	}
	want := slog.New(slog.DiscardHandler)
	if got := loggerOrDefault(want); got != want {
		t.Error("loggerOrDefault must return a non-nil logger unchanged")
	}
}
