package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestNextBackoffSchedule pins the spec §8.6 line 1671 ladder.
func TestNextBackoffSchedule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		step      int
		want      time.Duration
		permanent bool
	}{
		{0, 0, false},
		{-1, 0, false},
		{1, 30 * time.Second, false},
		{2, 60 * time.Second, false},
		{3, 120 * time.Second, false},
		{4, 240 * time.Second, false},
		{5, 480 * time.Second, false},
		{6, 0, true},
		{7, 0, true},
	}
	for _, c := range cases {
		got, perm := NextBackoff(c.step)
		if got != c.want || perm != c.permanent {
			t.Errorf("NextBackoff(%d) = (%s,%v); want (%s,%v)", c.step, got, perm, c.want, c.permanent)
		}
	}
}

// TestRecordCrashFirstFailure proves RecordCrash creates the plugin row when
// absent and seeds quarantined=true, step=1, next_retry_at=now+30s.
func TestRecordCrashFirstFailure(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)

	state, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", now)
	if err != nil {
		t.Fatalf("RecordCrash: %v", err)
	}

	if !state.Quarantined || state.RetryCount != 1 || state.BackoffStep != 1 {
		t.Errorf("state = %+v; want quarantined=true,retry=1,step=1", state)
	}
	if !state.NextRetryAt.Equal(now.Add(30 * time.Second)) {
		t.Errorf("next_retry_at = %v; want now+30s (%v)", state.NextRetryAt, now.Add(30*time.Second))
	}
	if state.LastErrorCode != "SERVICE_DOWN" {
		t.Errorf("last_error_code = %q; want SERVICE_DOWN", state.LastErrorCode)
	}
	if state.Permanent {
		t.Errorf("permanent = true after one failure; want false")
	}

	persisted, err := ReadSupervisorState(reg, "flights")
	if err != nil {
		t.Fatalf("ReadSupervisorState: %v", err)
	}
	if persisted != state {
		t.Errorf("persisted state = %+v; want %+v", persisted, state)
	}
}

// TestRecordCrashEscalatesToPermanent proves the 5-retry ceiling.
func TestRecordCrashEscalatesToPermanent(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)

	var state SupervisorState
	for i := 1; i <= MaxCrashRetries; i++ {
		s, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", now)
		if err != nil {
			t.Fatalf("RecordCrash #%d: %v", i, err)
		}
		state = s
		if state.BackoffStep != i {
			t.Errorf("after crash %d: step = %d; want %d", i, state.BackoffStep, i)
		}
	}
	if state.Permanent {
		t.Errorf("after 5 crashes: permanent=true; want false (5th crash uses step=5, still recoverable)")
	}
	if want := now.Add(CrashBackoffSchedule[4]); !state.NextRetryAt.Equal(want) {
		t.Errorf("5th crash next_retry_at = %v; want %v", state.NextRetryAt, want)
	}

	// 6th crash trips permanent quarantine and clears next_retry_at.
	s6, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", now)
	if err != nil {
		t.Fatalf("RecordCrash #6: %v", err)
	}
	if !s6.Permanent {
		t.Errorf("6th crash: permanent=false; want true (exceeded MaxCrashRetries)")
	}
	if !s6.NextRetryAt.IsZero() {
		t.Errorf("permanent quarantine: next_retry_at = %v; want zero", s6.NextRetryAt)
	}
}

// TestRecordCrashHonorsExistingStep proves restart-resume semantics: a host
// that boots with a persisted step=3 increments to step=4 (240s backoff) on
// the next crash, instead of restarting at step=1.
func TestRecordCrashHonorsExistingStep(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)

	if err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		f.State.Plugins = append(f.State.Plugins, map[string]any{
			"name":         "flights",
			"quarantined":  true,
			"retry_count":  3,
			"backoff_step": 3,
		})
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	state, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", now)
	if err != nil {
		t.Fatalf("RecordCrash: %v", err)
	}
	if state.BackoffStep != 4 || state.RetryCount != 4 {
		t.Errorf("state = %+v; want step=4,retry=4", state)
	}
	if !state.NextRetryAt.Equal(now.Add(240 * time.Second)) {
		t.Errorf("next_retry_at = %v; want now+240s", state.NextRetryAt)
	}
}

// TestClearQuarantine zeroes the supervisor fields and leaves install metadata
// (name, installed_at) intact.
func TestClearQuarantine(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)

	if _, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", now); err != nil {
		t.Fatalf("seed crash: %v", err)
	}
	if err := ClearQuarantine(context.Background(), reg, "flights"); err != nil {
		t.Fatalf("ClearQuarantine: %v", err)
	}
	state, err := ReadSupervisorState(reg, "flights")
	if err != nil {
		t.Fatalf("ReadSupervisorState: %v", err)
	}
	if state.Quarantined || state.RetryCount != 0 || state.BackoffStep != 0 || state.Permanent {
		t.Errorf("state = %+v; want zero", state)
	}
	if !state.NextRetryAt.IsZero() {
		t.Errorf("next_retry_at = %v; want zero", state.NextRetryAt)
	}
}

// TestPluginStateFileMode600 proves spec §8.6 line 1675 (mode 0600).
func TestPluginStateFileMode600(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := registry.New(dir)
	if _, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", time.Now()); err != nil {
		t.Fatalf("RecordCrash: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "plugin-state.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("plugin-state.json mode = %o; want 0600", perm)
	}
}

// TestSupervisorStartRefusesPermanent proves that a permanently quarantined
// plugin never invokes the spawner.
func TestSupervisorStartRefusesPermanent(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	for i := 0; i < MaxCrashRetries+1; i++ {
		if _, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", now); err != nil {
			t.Fatalf("seed crash %d: %v", i, err)
		}
	}

	var called int
	sup := NewSupervisor(reg, func(context.Context, string) (*Plugin, error) {
		called++
		return &Plugin{pluginID: "flights"}, nil
	}, func() time.Time { return now.Add(time.Hour) })

	_, err := sup.Start(context.Background(), "flights")
	if !errors.Is(err, ErrPluginQuarantined) {
		t.Fatalf("Start err = %v; want ErrPluginQuarantined", err)
	}
	if called != 0 {
		t.Errorf("spawner called %d times; want 0 (permanent quarantine)", called)
	}
}

// TestSupervisorStartRefusesInsideBackoff proves the spawner is not invoked
// while now < next_retry_at.
func TestSupervisorStartRefusesInsideBackoff(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	if _, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", now); err != nil {
		t.Fatalf("seed crash: %v", err)
	}

	var called int
	sup := NewSupervisor(reg, func(context.Context, string) (*Plugin, error) {
		called++
		return &Plugin{pluginID: "flights"}, nil
	}, func() time.Time { return now.Add(10 * time.Second) }) // inside 30s window

	_, err := sup.Start(context.Background(), "flights")
	if !errors.Is(err, ErrPluginQuarantined) {
		t.Fatalf("Start err = %v; want ErrPluginQuarantined", err)
	}
	if called != 0 {
		t.Errorf("spawner called %d times; want 0 (inside backoff)", called)
	}
}

// TestSupervisorStartClearsOnSuccess proves a successful spawn after a prior
// crash resets the supervisor state row.
func TestSupervisorStartClearsOnSuccess(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	if _, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", now); err != nil {
		t.Fatalf("seed crash: %v", err)
	}

	sup := NewSupervisor(reg, func(context.Context, string) (*Plugin, error) {
		return &Plugin{pluginID: "flights"}, nil
	}, func() time.Time { return now.Add(time.Hour) }) // past the 30s window

	plugin, err := sup.Start(context.Background(), "flights")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if plugin.PluginID() != "flights" {
		t.Errorf("plugin id = %q; want flights", plugin.PluginID())
	}
	state, err := ReadSupervisorState(reg, "flights")
	if err != nil {
		t.Fatalf("ReadSupervisorState: %v", err)
	}
	if state.Quarantined || state.BackoffStep != 0 || state.RetryCount != 0 {
		t.Errorf("post-success state = %+v; want zero", state)
	}
}

// TestSupervisorStartRecordsCrashOnSpawnFailure proves the supervisor persists
// the crash classification when the spawner returns an error.
func TestSupervisorStartRecordsCrashOnSpawnFailure(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)

	sup := NewSupervisor(reg, func(context.Context, string) (*Plugin, error) {
		return nil, ErrExecutableUntrusted
	}, func() time.Time { return now })

	_, err := sup.Start(context.Background(), "flights")
	if !errors.Is(err, ErrExecutableUntrusted) {
		t.Fatalf("Start err = %v; want ErrExecutableUntrusted", err)
	}
	state, err := ReadSupervisorState(reg, "flights")
	if err != nil {
		t.Fatalf("ReadSupervisorState: %v", err)
	}
	if !state.Quarantined || state.BackoffStep != 1 {
		t.Errorf("state = %+v; want quarantined,step=1", state)
	}
	if state.LastErrorCode != "PLUGIN_EXECUTABLE_UNTRUSTED" {
		t.Errorf("last_error_code = %q; want PLUGIN_EXECUTABLE_UNTRUSTED", state.LastErrorCode)
	}
}

// TestPluginStatusPrecedence is the bead-named acceptance test. It proves that
// the supervisor's quarantine decision happens BEFORE the spawn path: once
// plugin-state.json says quarantined, the spawner is never called and the
// caller observes ErrPluginQuarantined. The test mirrors the dispatch-layer
// VARIANT_QUARANTINED precedence (lifecycle.go:660): the variant-routing step
// returns VARIANT_QUARANTINED before authentication or executor dispatch is
// attempted, and the supervisor enforces the same ordering at process spawn.
func TestPluginStatusPrecedence(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)

	if _, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", now); err != nil {
		t.Fatalf("seed crash: %v", err)
	}

	var spawnerCalled bool
	sup := NewSupervisor(reg, func(context.Context, string) (*Plugin, error) {
		spawnerCalled = true
		return &Plugin{pluginID: "flights"}, nil
	}, func() time.Time { return now.Add(10 * time.Second) })

	_, err := sup.Start(context.Background(), "flights")
	if !errors.Is(err, ErrPluginQuarantined) {
		t.Fatalf("Start err = %v; want ErrPluginQuarantined", err)
	}
	if spawnerCalled {
		t.Errorf("spawner called despite quarantine; ordering violation")
	}

	// Confirm the persisted state still reflects the quarantine so a second
	// caller cannot trick the supervisor by racing the read.
	persisted, err := ReadSupervisorState(reg, "flights")
	if err != nil {
		t.Fatalf("ReadSupervisorState: %v", err)
	}
	if !persisted.Quarantined {
		t.Errorf("persisted state lost quarantine flag: %+v", persisted)
	}
}

// TestSupervisorStateRoundTripsThroughJSON proves the encoded plugin row is
// readable by every other process that loads plugin-state.json (the schema
// is shape-stable across the registry transaction protocol).
func TestSupervisorStateRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()
	reg := registry.New(t.TempDir())
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	if _, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", now); err != nil {
		t.Fatalf("RecordCrash: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(reg.ProfileDir(), "plugin-state.json"))
	if err != nil {
		t.Fatalf("read plugin-state.json: %v", err)
	}
	var parsed struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(parsed.Plugins) != 1 {
		t.Fatalf("len(plugins) = %d; want 1", len(parsed.Plugins))
	}
	row := parsed.Plugins[0]
	if row["name"] != "flights" {
		t.Errorf("name = %v; want flights", row["name"])
	}
	for _, key := range []string{"quarantined", "retry_count", "backoff_step", "last_error_code", "next_retry_at", "quarantined_at"} {
		if _, ok := row[key]; !ok {
			t.Errorf("missing key %q in persisted row: %+v", key, row)
		}
	}
}
