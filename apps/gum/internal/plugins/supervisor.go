package plugins

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// ErrPluginQuarantined is the host-side rendering of the spec §8.6 sentinel
// returned by the supervisor when a spawn attempt is refused because the
// plugin is currently quarantined (either inside a backoff window or
// permanently after 5 failed restarts).
var ErrPluginQuarantined = errors.New("PLUGIN_QUARANTINED")

// MaxCrashRetries is the spec §8.6 ceiling: after 5 failed restarts the
// plugin enters permanent quarantine until the user runs
// `gum plugin unquarantine <name>`.
const MaxCrashRetries = 5

// CrashBackoffSchedule is the spec §8.6 line 1671 wait ladder. Index i (1-based)
// is the delay before retry attempt i. After step==MaxCrashRetries the plugin
// is permanently quarantined.
var CrashBackoffSchedule = []time.Duration{
	30 * time.Second,
	60 * time.Second,
	120 * time.Second,
	240 * time.Second,
	480 * time.Second,
}

// NextBackoff returns the wait duration before the retry attempt identified by
// step (1..MaxCrashRetries). For step<=0 it returns 0; for step>MaxCrashRetries
// it returns (0, true) meaning the plugin is permanently quarantined.
func NextBackoff(step int) (time.Duration, bool) {
	if step <= 0 {
		return 0, false
	}
	if step > MaxCrashRetries {
		return 0, true
	}
	return CrashBackoffSchedule[step-1], false
}

// SupervisorState is the projection of the plugin-state.json row needed for
// crash-recovery decisions. The persisted JSON keeps additional fields (name,
// installed_at, ...) which the supervisor leaves untouched.
type SupervisorState struct {
	Quarantined   bool
	RetryCount    int
	BackoffStep   int
	NextRetryAt   time.Time
	LastErrorCode string
	Permanent     bool
}

// ReadSupervisorState returns the persisted state for pluginName. A row absent
// from plugin-state.json yields a zero SupervisorState{} (not quarantined,
// step 0). Other read errors propagate.
func ReadSupervisorState(reg *registry.Registry, pluginName string) (SupervisorState, error) {
	files, err := reg.Load()
	if err != nil {
		return SupervisorState{}, err
	}
	for _, raw := range files.State.Plugins {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := m["name"].(string); n != pluginName {
			continue
		}
		return decodeSupervisorState(m), nil
	}
	return SupervisorState{}, nil
}

// RecordCrash mutates the plugin row to reflect one failed spawn at `now`.
// Step is incremented; quarantined=true; next_retry_at is computed from the
// backoff schedule; once step exceeds MaxCrashRetries the row is marked
// permanently quarantined. Returns the post-mutation state.
//
// If the row is absent it is created with the minimum fields needed to drive
// recovery; the caller (Host.Install) is responsible for the full install
// row layout.
func RecordCrash(ctx context.Context, reg *registry.Registry, pluginName, errorCode string, now time.Time) (SupervisorState, error) {
	var out SupervisorState
	err := reg.WriteTransaction(ctx, func(f *registry.Files) error {
		row, idx := findOrAppendRow(f, pluginName)
		state := decodeSupervisorState(row)
		state.RetryCount++
		state.BackoffStep++
		state.LastErrorCode = errorCode
		state.Quarantined = true
		if wait, permanent := NextBackoff(state.BackoffStep); permanent {
			state.Permanent = true
			state.NextRetryAt = time.Time{}
		} else {
			state.NextRetryAt = now.Add(wait)
		}
		encodeSupervisorState(row, state, now)
		f.State.Plugins[idx] = row
		out = state
		return nil
	})
	return out, err
}

// ClearQuarantine resets the supervisor fields on the plugin row to the
// non-quarantined zero state. Used by `gum plugin unquarantine` and on a
// successful canary in `gum plugin reload`. Missing rows are a no-op.
func ClearQuarantine(ctx context.Context, reg *registry.Registry, pluginName string) error {
	return reg.WriteTransaction(ctx, func(f *registry.Files) error {
		for i, raw := range f.State.Plugins {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if n, _ := row["name"].(string); n != pluginName {
				continue
			}
			delete(row, "quarantined_at")
			delete(row, "last_error_code")
			delete(row, "next_retry_at")
			row["quarantined"] = false
			row["retry_count"] = 0
			row["backoff_step"] = 0
			row["permanent_quarantine"] = false
			f.State.Plugins[i] = row
			return nil
		}
		return nil
	})
}

// findOrAppendRow returns the plugin row map for name, appending a new row
// if absent. The returned index points to f.State.Plugins.
func findOrAppendRow(f *registry.Files, name string) (map[string]any, int) {
	for i, raw := range f.State.Plugins {
		if m, ok := raw.(map[string]any); ok {
			if n, _ := m["name"].(string); n == name {
				return m, i
			}
		}
	}
	row := map[string]any{"name": name}
	f.State.Plugins = append(f.State.Plugins, row)
	return row, len(f.State.Plugins) - 1
}

// decodeSupervisorState lifts SupervisorState out of a plugin row. Missing
// fields default to the zero value.
func decodeSupervisorState(row map[string]any) SupervisorState {
	s := SupervisorState{}
	if v, ok := row["quarantined"].(bool); ok {
		s.Quarantined = v
	}
	s.RetryCount = intOf(row["retry_count"])
	s.BackoffStep = intOf(row["backoff_step"])
	if v, ok := row["last_error_code"].(string); ok {
		s.LastErrorCode = v
	}
	if v, ok := row["permanent_quarantine"].(bool); ok {
		s.Permanent = v
	}
	if v, ok := row["next_retry_at"].(string); ok && v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			s.NextRetryAt = t
		}
	}
	return s
}

// encodeSupervisorState writes the supervisor fields back into the row. Other
// fields (name, installed_at, activated_at, ...) are left untouched.
func encodeSupervisorState(row map[string]any, s SupervisorState, now time.Time) {
	row["quarantined"] = s.Quarantined
	row["retry_count"] = s.RetryCount
	row["backoff_step"] = s.BackoffStep
	row["last_error_code"] = s.LastErrorCode
	row["permanent_quarantine"] = s.Permanent
	if s.Quarantined {
		row["quarantined_at"] = now.UTC().Format(time.RFC3339)
	}
	if !s.NextRetryAt.IsZero() {
		row["next_retry_at"] = s.NextRetryAt.UTC().Format(time.RFC3339)
	} else {
		delete(row, "next_retry_at")
	}
}

// intOf coerces a number stored as any JSON-decoded value back to int. JSON
// numbers come back as float64; we tolerate int and float64 here.
func intOf(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

// Spawner spawns one plugin attempt; the supervisor consults state before
// calling it and records crashes after.
type Spawner func(ctx context.Context, pluginID string) (*Plugin, error)

// Supervisor wraps a Spawner with spec §8.6 crash-recovery semantics.
//
// Start consults plugin-state.json before delegating: a permanently
// quarantined plugin returns ErrPluginQuarantined unconditionally; a plugin
// inside an active backoff window returns ErrPluginQuarantined with the
// next_retry_at timestamp; otherwise Start invokes the spawner and persists
// the outcome (success → ClearQuarantine; failure → RecordCrash).
type Supervisor struct {
	reg   *registry.Registry
	spawn Spawner
	now   func() time.Time
}

// NewSupervisor binds a Supervisor to a registry and spawner. The clock
// argument is injectable for tests; nil falls back to time.Now.
func NewSupervisor(reg *registry.Registry, spawn Spawner, now func() time.Time) *Supervisor {
	if now == nil {
		now = time.Now
	}
	return &Supervisor{reg: reg, spawn: spawn, now: now}
}

// Start runs one supervised spawn attempt for pluginName. The bool return is
// true when the attempt actually invoked the spawner (after passing quarantine
// gating); false when Start short-circuited on quarantine state.
func (s *Supervisor) Start(ctx context.Context, pluginName string) (*Plugin, error) {
	state, err := ReadSupervisorState(s.reg, pluginName)
	if err != nil {
		return nil, err
	}
	if state.Permanent {
		return nil, fmt.Errorf("%w: permanent (5 consecutive failures); run `gum plugin unquarantine %s`",
			ErrPluginQuarantined, pluginName)
	}
	now := s.now()
	if state.Quarantined && !state.NextRetryAt.IsZero() && now.Before(state.NextRetryAt) {
		return nil, fmt.Errorf("%w: retry at %s", ErrPluginQuarantined, state.NextRetryAt.UTC().Format(time.RFC3339))
	}
	plugin, spawnErr := s.spawn(ctx, pluginName)
	if spawnErr != nil {
		if _, recordErr := RecordCrash(ctx, s.reg, pluginName, classifySpawnError(spawnErr), now); recordErr != nil {
			return nil, fmt.Errorf("supervisor: spawn failed (%v); state persistence failed: %w", spawnErr, recordErr)
		}
		return nil, spawnErr
	}
	if state.Quarantined || state.BackoffStep > 0 {
		if err := ClearQuarantine(ctx, s.reg, pluginName); err != nil {
			_ = plugin.Stop(ctx)
			return nil, fmt.Errorf("supervisor: spawn ok but ClearQuarantine failed: %w", err)
		}
	}
	return plugin, nil
}

// classifySpawnError maps a Host.Start error to one of the spec §8.4 stable
// error codes recorded in last_error_code. The mapping is intentionally
// coarse: any non-recognised error becomes SERVICE_DOWN, which mirrors the
// in-flight-call error returned to the LLM.
func classifySpawnError(err error) string {
	switch {
	case errors.Is(err, ErrExecutableUntrusted):
		return "PLUGIN_EXECUTABLE_UNTRUSTED"
	case errors.Is(err, ErrManifestNotFound):
		return "PLUGIN_MANIFEST_NOT_FOUND"
	case errors.Is(err, ErrManifestInvalid):
		return "PLUGIN_MANIFEST_INVALID"
	case errors.Is(err, ErrUnsupportedShape):
		return "PLUGIN_SHAPE_UNSUPPORTED"
	case errors.Is(err, ErrUnsupportedSchemaVersion):
		return "PLUGIN_MANIFEST_SCHEMA_UNSUPPORTED"
	default:
		return "SERVICE_DOWN"
	}
}
