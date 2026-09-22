package dispatch

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
)

// refusingLedger fails every Append, which is the cheapest way to reach a
// kernel diagnostic: appendGainEntry logs the failure and swallows it, because
// step 9 accounting must never fail a call that already produced a response.
type refusingLedger struct{}

func (refusingLedger) Append(gain.Entry) error { return errors.New("ledger closed") }

// TestDispatcherLoggerInjection proves the three §14.1 rule 2 behaviours for
// internal/dispatch: DispatcherConfig.Logger reaches the emission, a
// DiscardHandler silences the kernel, and the zero value falls back to
// slog.Default(). The cross-package gate is TestLoggerInjectionContract in
// internal/lint; this one covers the accessor branches the external test
// cannot reach, because the dispatcher type is unexported.
func TestDispatcherLoggerInjection(t *testing.T) {
	emit := func(l *slog.Logger) {
		d := &dispatcher{logger: l, gainLedger: refusingLedger{}}
		d.appendGainEntry(gain.Entry{}, "op-1")
	}

	var injected bytes.Buffer
	emit(slog.New(slog.NewTextHandler(&injected, nil)))
	if injected.Len() == 0 {
		t.Fatal("DispatcherConfig.Logger did not reach the emission")
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
		t.Error("a nil Logger must fall back to slog.Default()")
	}
}

// TestNewDispatcherWithConfigCarriesTheLogger pins the wiring between the
// exported config surface and the unexported field, so a refactor that drops
// the assignment fails here rather than silently reverting every kernel
// diagnostic to the process default.
func TestNewDispatcherWithConfigCarriesTheLogger(t *testing.T) {
	want := slog.New(slog.DiscardHandler)
	built := NewDispatcherWithConfig(nil, nil, DispatcherConfig{Logger: want})
	d, ok := built.(*dispatcher)
	if !ok {
		t.Fatalf("NewDispatcherWithConfig returned %T; want *dispatcher", built)
	}
	if d.logger != want {
		t.Fatalf("dispatcher.logger = %p; want the injected %p", d.logger, want)
	}
	if d.log() != want {
		t.Fatal("log() did not return the injected logger")
	}
}
