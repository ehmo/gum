package main

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestOpenRegistryEmptyProfileRejected pins the guard: an empty
// profileDir surfaces the operator-friendly hint pointing at --profile
// or XDG_DATA_HOME instead of letting registry.New silently swallow it.
func TestOpenRegistryEmptyProfileRejected(t *testing.T) {
	_, err := openRegistry("", nil)
	if err == nil {
		t.Fatal("expected error for empty profileDir")
	}
	if !strings.Contains(err.Error(), "profile dir unresolved") {
		t.Errorf("err=%q; want 'profile dir unresolved' hint", err)
	}
}

// TestOpenRegistryFactoryShortCircuits drives the test-substitution
// branch: when a factory is provided it bypasses the mkdir + real
// registry.New so tests can wire deterministic fakes.
func TestOpenRegistryFactoryShortCircuits(t *testing.T) {
	called := false
	var captured string
	stub := &registry.Registry{}
	reg, err := openRegistry("/synthetic", func(dir string) *registry.Registry {
		called = true
		captured = dir
		return stub
	})
	if err != nil {
		t.Fatalf("openRegistry: %v", err)
	}
	if !called {
		t.Error("factory not invoked")
	}
	if captured != "/synthetic" {
		t.Errorf("factory got %q; want /synthetic", captured)
	}
	if reg != stub {
		t.Errorf("got %p; want stub %p", reg, stub)
	}
}

// TestOpenRegistryDefaultPathCreatesDir drives the production path:
// when no factory is supplied, openRegistry must mkdir the profile dir
// (0o700) and hand back a non-nil registry rooted there.
func TestOpenRegistryDefaultPathCreatesDir(t *testing.T) {
	dir := t.TempDir() + "/profile/sub" // intentionally nested to force mkdir
	reg, err := openRegistry(dir, nil)
	if err != nil {
		t.Fatalf("openRegistry: %v", err)
	}
	if reg == nil {
		t.Error("nil registry returned")
	}
}
