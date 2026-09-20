package adapters

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// TestEnsureRunningRestartsDeadPlugin pins gum-q79t: a cached handle whose
// subprocess session has ended must be evicted and replaced, not handed back.
// Before the fix the corpse stayed in p.running for the life of the process, so
// every later dispatch failed on a closed transport and nothing ever asked the
// supervisor for a new spawn — the spec §8.6 restart ladder never ran.
func TestEnsureRunningRestartsDeadPlugin(t *testing.T) {
	starts := 0
	pm := NewPluginMCPLazyWithStarter(
		func() *plugins.Host { return plugins.NewHost(plugins.HostConfig{}) },
		func(context.Context, *plugins.Host, string) (*plugins.Plugin, error) {
			starts++
			return &plugins.Plugin{}, nil
		},
	)
	ctx := context.Background()

	first, err := pm.ensureRunning(ctx, "flights")
	if err != nil {
		t.Fatalf("first ensureRunning: %v", err)
	}
	if starts != 1 {
		t.Fatalf("starts=%d after first call; want 1", starts)
	}

	// The subprocess dies. Stop marks the handle unusable exactly as the
	// crash watcher does when cs.Wait returns.
	if err := first.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	second, err := pm.ensureRunning(ctx, "flights")
	if err != nil {
		t.Fatalf("second ensureRunning: %v", err)
	}
	if starts != 2 {
		t.Errorf("starts=%d after the handle died; want 2 (dead handle must be restarted)", starts)
	}
	if second == first {
		t.Errorf("second ensureRunning returned the dead handle %p; want a fresh one", first)
	}
}

// TestEnsureRunningRestartFailureDropsCacheEntry pins the companion case: when
// the restart itself fails, the dead handle must not survive in the cache. A
// retained corpse would mask a later successful spawn.
func TestEnsureRunningRestartFailureDropsCacheEntry(t *testing.T) {
	ctx := context.Background()
	dead := &plugins.Plugin{}
	if err := dead.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	pm := NewPluginMCPLazyWithStarter(
		func() *plugins.Host { return plugins.NewHost(plugins.HostConfig{}) },
		func(context.Context, *plugins.Host, string) (*plugins.Plugin, error) {
			return nil, plugins.ErrPluginQuarantined
		},
	)
	pm.running["flights"] = dead

	if _, err := pm.ensureRunning(ctx, "flights"); err != plugins.ErrPluginQuarantined {
		t.Fatalf("err=%v; want ErrPluginQuarantined from the restart", err)
	}
	if _, still := pm.running["flights"]; still {
		t.Errorf("dead handle still cached after a failed restart")
	}
}
