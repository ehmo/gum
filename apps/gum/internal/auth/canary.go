package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// CanaryState is the closed-enum lifecycle state for a managed scope's live canary.
type CanaryState string

const (
	CanaryStatePassing CanaryState = "passing"
	CanaryStateFailing CanaryState = "failing"
	CanaryStateStale   CanaryState = "stale"
)

// CanaryEntry mirrors the shape of a scope row in docs/auth-managed-scopes.v1.json.
// Only the fields the canary cares about are modeled; other fields are preserved
// verbatim through the read-modify-write cycle.
type CanaryEntry struct {
	Scope              string      `json:"scope"`
	LiveCanaryRequired bool        `json:"live_canary_required"`
	LiveCanaryState    CanaryState `json:"live_canary_state"`
	LastChecked        time.Time   `json:"last_checked"`
}

// CanaryProbe is the function called to test a single scope. Returns nil on
// success (state→passing), non-nil on failure (state→failing).
type CanaryProbe func(ctx context.Context, scope string) error

// Scheduler reads the managed-scopes registry, runs each required canary, and
// writes the registry back with updated states. It MUST NOT mutate any field
// other than live_canary_state and last_checked.
type Scheduler struct {
	cfg SchedulerConfig
}

// SchedulerConfig configures a Scheduler.
type SchedulerConfig struct {
	RegistryPath string        // path to auth-managed-scopes.v1.json
	Probe        CanaryProbe   // per-scope probe
	StaleAfter   time.Duration // last_checked older than this → stale
	// StaleAfter is stored but unused in v0.1.0 logic; staleness detection
	// lands in v0.2.0 when background scheduling is introduced.
	Now func() time.Time // injectable clock; defaults to time.Now
}

// NewScheduler constructs a Scheduler.
func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.StaleAfter == 0 {
		cfg.StaleAfter = 24 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Scheduler{cfg: cfg}
}

// RunOnce runs the canary for every required scope and writes back updated
// states. Returns a per-scope outcome map.
func (s *Scheduler) RunOnce(ctx context.Context) (map[string]CanaryState, error) {
	data, err := os.ReadFile(s.cfg.RegistryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrRegistryNotFound
		}
		return nil, fmt.Errorf("%w: read: %v", ErrRegistryInvalid, err)
	}

	// Unmarshal as a generic map to preserve all unknown fields on write-back.
	var registry map[string]any
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, ErrRegistryInvalid
	}

	scopesRaw, ok := registry["scopes"]
	if !ok {
		return nil, ErrRegistryInvalid
	}
	scopesSlice, ok := scopesRaw.([]any)
	if !ok {
		return nil, ErrRegistryInvalid
	}

	outcomes := make(map[string]CanaryState)
	now := s.cfg.Now().UTC().Format(time.RFC3339)

	for i, scopeRaw := range scopesSlice {
		scopeMap, ok := scopeRaw.(map[string]any)
		if !ok {
			continue
		}

		// Extract scope name.
		scopeName, _ := scopeMap["scope"].(string)
		if scopeName == "" {
			continue
		}

		// Check live_canary_required; skip if false or missing.
		required, _ := scopeMap["live_canary_required"].(bool)
		if !required {
			continue
		}

		// Run the probe.
		var state CanaryState
		if probeErr := s.cfg.Probe(ctx, scopeName); probeErr == nil {
			state = CanaryStatePassing
		} else {
			state = CanaryStateFailing
		}

		// Update only live_canary_state and last_checked in the original map.
		scopeMap["live_canary_state"] = string(state)
		scopeMap["last_checked"] = now

		// Write the updated map back into the slice.
		scopesSlice[i] = scopeMap

		outcomes[scopeName] = state
	}

	// Put the updated scopes back into the registry.
	registry["scopes"] = scopesSlice

	// Serialize and atomic-write.
	out, err := json.Marshal(registry)
	if err != nil {
		return nil, fmt.Errorf("canary: marshal registry: %w", err)
	}

	tmpPath := s.cfg.RegistryPath + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0o644); err != nil {
		return nil, fmt.Errorf("canary: write tmp: %w", err)
	}
	if err := os.Rename(tmpPath, s.cfg.RegistryPath); err != nil {
		return nil, fmt.Errorf("canary: rename: %w", err)
	}

	return outcomes, nil
}

var (
	ErrRegistryNotFound = errors.New("AUTH_MANAGED_SCOPES_NOT_FOUND")
	ErrRegistryInvalid  = errors.New("AUTH_MANAGED_SCOPES_INVALID")
)
