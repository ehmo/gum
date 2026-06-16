package mcp

import (
	"testing"
	"time"

	"github.com/ehmo/gum/internal/lro"
)

// TestSetVersion locks the override semantics: empty input must NOT
// clobber the existing Version (otherwise a missing build-time wiring
// would silently empty the advertised version). Non-empty input replaces
// the package-level value.
//
// We snapshot and restore Version around the mutation so adjacent tests
// asserting "0.1.0-dev" don't see the rewrite.
func TestSetVersion(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	t.Run("empty_does_not_clobber", func(t *testing.T) {
		Version = "snapshot-v1"
		SetVersion("")
		if Version != "snapshot-v1" {
			t.Errorf("Version = %q, want snapshot-v1 (empty must not overwrite)", Version)
		}
	})

	t.Run("non_empty_replaces", func(t *testing.T) {
		SetVersion("v0.2.0-rc1")
		if Version != "v0.2.0-rc1" {
			t.Errorf("Version = %q, want v0.2.0-rc1", Version)
		}
	})
}

// TestDefaultPollerFactoryWiring exercises the factory: the returned
// poller must be a non-nil *lro.Poller, must carry an HTTPFetcher, and
// must forward elapsed-time ticks to the caller-supplied callback. This
// is the seam through which handleLROCheck surfaces poll progress to MCP
// clients; a regression here silently disables those updates.
func TestDefaultPollerFactoryWiring(t *testing.T) {
	s := &Server{profile: "test"}
	var seen time.Duration
	cb := func(d time.Duration) { seen = d }

	got := s.defaultPollerFactory(cb)
	if got == nil {
		t.Fatal("defaultPollerFactory returned nil")
	}
	poller, ok := got.(*lro.Poller)
	if !ok {
		t.Fatalf("got %T, want *lro.Poller", got)
	}
	if poller.Fetcher == nil {
		t.Errorf("Fetcher field is nil; want *lro.HTTPFetcher")
	}
	if poller.OnTick == nil {
		t.Fatal("OnTick is nil; factory did not wire the callback")
	}
	poller.OnTick(7 * time.Millisecond)
	if seen != 7*time.Millisecond {
		t.Errorf("OnTick callback not wired through: seen=%v", seen)
	}
}
