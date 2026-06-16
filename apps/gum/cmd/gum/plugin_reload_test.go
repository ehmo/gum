package main_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	gummain "github.com/ehmo/gum/cmd/gum"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// seedQuarantinedPlugin writes a plugin-state.json row marking pluginName as
// quarantined with the given backoff step. Returns the registry pointing at
// the tempdir profile.
func seedQuarantinedPlugin(t *testing.T, pluginName string, step int) *registry.Registry {
	t.Helper()
	reg := registry.New(t.TempDir())
	if _, err := plugins.RecordCrash(context.Background(), reg, pluginName, "SERVICE_DOWN", time.Now()); err != nil {
		t.Fatalf("seed crash: %v", err)
	}
	for i := 1; i < step; i++ {
		if _, err := plugins.RecordCrash(context.Background(), reg, pluginName, "SERVICE_DOWN", time.Now()); err != nil {
			t.Fatalf("seed crash %d: %v", i, err)
		}
	}
	return reg
}

// TestPluginUnquarantineSubcommandClearsState routes the CLI dispatcher
// through the real registry helpers (under a tempdir) and asserts the
// quarantine flag is cleared.
func TestPluginUnquarantineSubcommandClearsState(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := seedQuarantinedPlugin(t, "flights", 1)
	host := &mockHost{} // unquarantine MUST NOT call host.Start

	args := []string{"unquarantine", "flights"}
	result, err := gummain.DispatchPluginCommandWithRegistry(args, host, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err != nil {
		t.Fatalf("DispatchPluginCommandWithRegistry: %v", err)
	}
	if !strings.Contains(result, "unquarantined flights") {
		t.Errorf("result = %q; want contains 'unquarantined flights'", result)
	}

	state, err := plugins.ReadSupervisorState(reg, "flights")
	if err != nil {
		t.Fatalf("ReadSupervisorState: %v", err)
	}
	if state.Quarantined || state.BackoffStep != 0 || state.RetryCount != 0 {
		t.Errorf("post-unquarantine state = %+v; want zero", state)
	}
}

// TestPluginUnquarantineSubcommandMissingArgReturnsError guards usage.
func TestPluginUnquarantineSubcommandMissingArgReturnsError(t *testing.T) {
	defer goleak.VerifyNone(t)

	_, err := gummain.DispatchPluginCommandWithRegistry([]string{"unquarantine"}, &mockHost{}, t.TempDir(), nil)
	if err == nil {
		t.Error("missing arg returned nil error; want error")
	}
}

// TestPluginReloadSubcommandClearsAndCanaries proves reload clears quarantine
// AND invokes host.Start exactly once as a passive canary.
func TestPluginReloadSubcommandClearsAndCanaries(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := seedQuarantinedPlugin(t, "flights", 1)

	var startCalls int
	host := &mockHost{
		startFn: func(_ context.Context, pluginID string) (*plugins.Plugin, error) {
			startCalls++
			if pluginID != "flights" {
				t.Errorf("Start called with %q; want flights", pluginID)
			}
			return &plugins.Plugin{}, nil // zero Plugin: Stop is a no-op
		},
	}

	args := []string{"reload", "flights"}
	result, err := gummain.DispatchPluginCommandWithRegistry(args, host, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err != nil {
		t.Fatalf("DispatchPluginCommandWithRegistry: %v", err)
	}
	if !strings.Contains(result, "reloaded flights") {
		t.Errorf("result = %q; want contains 'reloaded flights'", result)
	}
	if startCalls != 1 {
		t.Errorf("host.Start called %d times; want 1 (passive canary)", startCalls)
	}

	state, err := plugins.ReadSupervisorState(reg, "flights")
	if err != nil {
		t.Fatalf("ReadSupervisorState: %v", err)
	}
	if state.Quarantined {
		t.Errorf("post-reload state still quarantined: %+v", state)
	}
}

// TestPluginReloadSubcommandReQuarantinesOnCanaryFailure proves the supervisor
// path runs: a Start failure during reload re-quarantines via RecordCrash.
func TestPluginReloadSubcommandReQuarantinesOnCanaryFailure(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())
	host := &mockHost{
		startFn: func(context.Context, string) (*plugins.Plugin, error) {
			return nil, errors.New("subprocess refused to start")
		},
	}

	args := []string{"reload", "flights"}
	_, err := gummain.DispatchPluginCommandWithRegistry(args, host, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err == nil {
		t.Fatal("reload with failing canary returned nil error; want error")
	}
	if !strings.Contains(err.Error(), "passive canary") {
		t.Errorf("err = %v; want 'passive canary' in message", err)
	}

	state, err := plugins.ReadSupervisorState(reg, "flights")
	if err != nil {
		t.Fatalf("ReadSupervisorState: %v", err)
	}
	if !state.Quarantined || state.BackoffStep != 1 {
		t.Errorf("post-failed-reload state = %+v; want quarantined,step=1", state)
	}
}

// TestPluginReloadSubcommandMissingArgReturnsError guards usage.
func TestPluginReloadSubcommandMissingArgReturnsError(t *testing.T) {
	defer goleak.VerifyNone(t)

	_, err := gummain.DispatchPluginCommandWithRegistry([]string{"reload"}, &mockHost{}, t.TempDir(), nil)
	if err == nil {
		t.Error("missing arg returned nil error; want error")
	}
}

// TestPluginRunSubcommandRefusesQuarantinedPlugin pins gum-g7xr: `gum plugin
// run` routes through the Supervisor when a profile registry exists, so a
// permanently-quarantined plugin is refused and never spawned (it previously
// called host.Start directly, bypassing the quarantine gate).
func TestPluginRunSubcommandRefusesQuarantinedPlugin(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())
	for i := 0; i < plugins.MaxCrashRetries; i++ {
		if _, err := plugins.RecordCrash(context.Background(), reg, "flights", "PLUGIN_SPAWN_FAILED", time.Now()); err != nil {
			t.Fatalf("RecordCrash %d: %v", i, err)
		}
	}
	started := false
	host := &mockHost{
		startFn: func(context.Context, string) (*plugins.Plugin, error) {
			started = true
			return nil, errors.New("should not be reached for a quarantined plugin")
		},
	}

	args := []string{"run", "flights", "ping"}
	_, err := gummain.DispatchPluginCommandWithRegistry(args, host, reg.ProfileDir(), func(string) *registry.Registry { return reg })
	if err == nil {
		t.Fatal("run of a quarantined plugin returned nil error; want PLUGIN_QUARANTINED")
	}
	if !errors.Is(err, plugins.ErrPluginQuarantined) {
		t.Errorf("err = %v; want ErrPluginQuarantined", err)
	}
	if started {
		t.Error("host.Start was called for a quarantined plugin — quarantine gate bypassed")
	}
}
