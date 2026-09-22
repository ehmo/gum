package registry

import (
	"bytes"
	"log/slog"
	"syscall"
	"testing"
)

// TestRegistryLoggerInjection proves the three §14.1 rule 2 behaviours for
// internal/plugins/registry: WithLogger reaches the emission, a DiscardHandler
// silences the registry, and an unchained registry falls back to
// slog.Default(). Each arm needs its own profile directory, because §8.7
// scopes the fsync warning to once per profile per process and fsyncWarned
// dedupes on that path.
func TestRegistryLoggerInjection(t *testing.T) {
	emit := func(l *slog.Logger) {
		r := New(t.TempDir()).WithLogger(l)
		if err := r.tolerateUnsupportedFsync(fsyncUnsupportedError{err: syscall.ENOTSUP}); err != nil {
			t.Fatalf("tolerateUnsupportedFsync returned %v; the §8.7 fallback must swallow it", err)
		}
	}

	var injected bytes.Buffer
	emit(slog.New(slog.NewTextHandler(&injected, nil)))
	if injected.Len() == 0 {
		t.Fatal("WithLogger did not reach the emission")
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
		t.Error("WithLogger(nil) must leave the registry on slog.Default()")
	}
}
