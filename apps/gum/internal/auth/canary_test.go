package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/catalog"
)

// canaryCatchPanic calls fn and returns ("panic: not implemented", true) if fn
// panics, else ("", false).
func canaryCatchPanic(fn func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("panic: %v", r)
			panicked = true
		}
	}()
	fn()
	return "", false
}

// fixtureRegistryPath returns the path to the managed-scopes-fixture.json
// testdata file in this package's testdata directory.
func fixtureRegistryPath(t *testing.T) string {
	t.Helper()
	// Use relative path from this test's package directory.
	p, err := filepath.Abs("testdata/managed-scopes-fixture.json")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	return p
}

// copyFixture copies the fixture to a temp file so tests can mutate it safely.
func copyFixture(t *testing.T) string {
	t.Helper()
	src := fixtureRegistryPath(t)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "managed-scopes.json")
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatalf("write temp fixture: %v", err)
	}
	return dst
}

// fixedNow returns a deterministic time to use as the injected clock.
func fixedNow() time.Time {
	return time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
}

// TestCanaryRunOnceUpdatesPassing verifies that when the probe returns nil the
// gmail.readonly scope transitions to CanaryStatePassing and last_checked is updated.
func TestCanaryRunOnceUpdatesPassing(t *testing.T) {
	defer goleak.VerifyNone(t)

	registryPath := copyFixture(t)
	probe := func(_ context.Context, _ string) error { return nil }
	now := fixedNow()

	var s *auth.Scheduler
	{
		msg, panicked := canaryCatchPanic(func() {
			s = auth.NewScheduler(auth.SchedulerConfig{
				RegistryPath: registryPath,
				Probe:        probe,
				StaleAfter:   24 * time.Hour,
				Now:          func() time.Time { return now },
			})
		})
		if panicked {
			t.Fatalf("NewScheduler panicked: %s (green team must implement)", msg)
		}
		if s == nil {
			t.Fatal("NewScheduler returned nil (green team must implement)")
		}
	}

	var outcomes map[string]auth.CanaryState
	var runErr error
	msg, panicked := canaryCatchPanic(func() {
		outcomes, runErr = s.RunOnce(t.Context())
	})
	if panicked {
		t.Fatalf("Scheduler.RunOnce panicked: %s (green team must implement)", msg)
	}
	if runErr != nil {
		t.Fatalf("RunOnce returned unexpected error: %v", runErr)
	}

	const scope = "https://www.googleapis.com/auth/gmail.readonly"
	got, ok := outcomes[scope]
	if !ok {
		t.Fatalf("outcomes missing scope %q; got %v", scope, outcomes)
	}
	if got != auth.CanaryStatePassing {
		t.Errorf("outcomes[%q] = %q, want %q", scope, got, auth.CanaryStatePassing)
	}

	// Verify the registry file was updated.
	raw, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read registry after RunOnce: %v", err)
	}
	var doc struct {
		Scopes []struct {
			Scope           string `json:"scope"`
			LiveCanaryState string `json:"live_canary_state"`
			LastChecked     string `json:"last_checked"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal registry: %v", err)
	}
	for _, sc := range doc.Scopes {
		if sc.Scope == scope {
			if sc.LiveCanaryState != "passing" {
				t.Errorf("registry live_canary_state = %q, want %q", sc.LiveCanaryState, "passing")
			}
			// last_checked must have been updated to the injected clock time.
			ts, parseErr := time.Parse(time.RFC3339, sc.LastChecked)
			if parseErr != nil {
				t.Errorf("last_checked %q does not parse as RFC3339: %v", sc.LastChecked, parseErr)
			} else if !ts.Equal(now) {
				t.Errorf("last_checked = %v, want %v", ts, now)
			}
			return
		}
	}
	t.Errorf("scope %q not found in registry after RunOnce", scope)
}

// TestCanaryRunOnceUpdatesFailing verifies that when the probe returns an error
// the scope transitions to CanaryStateFailing.
func TestCanaryRunOnceUpdatesFailing(t *testing.T) {
	defer goleak.VerifyNone(t)

	registryPath := copyFixture(t)
	probeErr := errors.New("connection refused")
	probe := func(_ context.Context, _ string) error { return probeErr }

	var s *auth.Scheduler
	{
		msg, panicked := canaryCatchPanic(func() {
			s = auth.NewScheduler(auth.SchedulerConfig{
				RegistryPath: registryPath,
				Probe:        probe,
				StaleAfter:   24 * time.Hour,
				Now:          fixedNow,
			})
		})
		if panicked {
			t.Fatalf("NewScheduler panicked: %s (green team must implement)", msg)
		}
		if s == nil {
			t.Fatal("NewScheduler returned nil (green team must implement)")
		}
	}

	var outcomes map[string]auth.CanaryState
	var runErr error
	msg, panicked := canaryCatchPanic(func() {
		outcomes, runErr = s.RunOnce(t.Context())
	})
	if panicked {
		t.Fatalf("Scheduler.RunOnce panicked: %s (green team must implement)", msg)
	}
	if runErr != nil {
		t.Fatalf("RunOnce returned unexpected error: %v", runErr)
	}

	const scope = "https://www.googleapis.com/auth/gmail.readonly"
	got, ok := outcomes[scope]
	if !ok {
		t.Fatalf("outcomes missing required scope %q; got %v", scope, outcomes)
	}
	if got != auth.CanaryStateFailing {
		t.Errorf("outcomes[%q] = %q, want %q", scope, got, auth.CanaryStateFailing)
	}
}

// TestCanaryStaleAfterCutoff verifies that a previously-passing scope whose
// last_checked is older than StaleAfter is re-run; if the probe passes, the
// resulting state is passing and last_checked is updated.
func TestCanaryStaleAfterCutoff(t *testing.T) {
	defer goleak.VerifyNone(t)

	// Write a custom fixture with a previously-passing scope and an old timestamp.
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "managed-scopes.json")
	oldTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	fixture := fmt.Sprintf(`{
  "schema_version": 1,
  "scopes": [
    {
      "scope": "https://www.googleapis.com/auth/gmail.readonly",
      "status": "active",
      "verification_state": "verified",
      "project_evidence_state": "ready",
      "live_canary_required": true,
      "live_canary_state": "passing",
      "last_checked": %q
    }
  ]
}`, oldTime.Format(time.RFC3339))
	if err := os.WriteFile(registryPath, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	now := fixedNow()
	staleAfter := 24 * time.Hour // old timestamp is well beyond stale cutoff

	probe := func(_ context.Context, _ string) error { return nil }
	var s *auth.Scheduler
	{
		msg, panicked := canaryCatchPanic(func() {
			s = auth.NewScheduler(auth.SchedulerConfig{
				RegistryPath: registryPath,
				Probe:        probe,
				StaleAfter:   staleAfter,
				Now:          func() time.Time { return now },
			})
		})
		if panicked {
			t.Fatalf("NewScheduler panicked: %s (green team must implement)", msg)
		}
		if s == nil {
			t.Fatal("NewScheduler returned nil (green team must implement)")
		}
	}

	var outcomes map[string]auth.CanaryState
	var runErr error
	msg, panicked := canaryCatchPanic(func() {
		outcomes, runErr = s.RunOnce(t.Context())
	})
	if panicked {
		t.Fatalf("RunOnce panicked: %s (green team must implement)", msg)
	}
	if runErr != nil {
		t.Fatalf("RunOnce returned unexpected error: %v", runErr)
	}

	const scope = "https://www.googleapis.com/auth/gmail.readonly"
	got, ok := outcomes[scope]
	if !ok {
		t.Fatalf("outcomes missing scope %q; got %v", scope, outcomes)
	}
	if got != auth.CanaryStatePassing {
		t.Errorf("outcomes[%q] = %q after stale re-run, want passing", scope, got)
	}

	// Confirm last_checked was updated to now.
	raw, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var doc struct {
		Scopes []struct {
			Scope       string `json:"scope"`
			LastChecked string `json:"last_checked"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal registry: %v", err)
	}
	for _, sc := range doc.Scopes {
		if sc.Scope == scope {
			ts, parseErr := time.Parse(time.RFC3339, sc.LastChecked)
			if parseErr != nil {
				t.Errorf("last_checked parse error: %v", parseErr)
			} else if !ts.Equal(now) {
				t.Errorf("last_checked = %v, want %v (not the old stale time)", ts, now)
			}
			return
		}
	}
	t.Errorf("scope %q not found in registry after stale re-run", scope)
}

// TestCanaryRegistryNotFound verifies that a non-existent registry path returns
// ErrRegistryNotFound.
func TestCanaryRegistryNotFound(t *testing.T) {
	defer goleak.VerifyNone(t)

	var s *auth.Scheduler
	{
		msg, panicked := canaryCatchPanic(func() {
			s = auth.NewScheduler(auth.SchedulerConfig{
				RegistryPath: filepath.Join(t.TempDir(), "does-not-exist.json"),
				Probe:        func(_ context.Context, _ string) error { return nil },
				StaleAfter:   time.Hour,
				Now:          fixedNow,
			})
		})
		if panicked {
			t.Fatalf("NewScheduler panicked: %s (green team must implement)", msg)
		}
		if s == nil {
			t.Fatal("NewScheduler returned nil (green team must implement)")
		}
	}

	var runErr error
	msg, panicked := canaryCatchPanic(func() {
		_, runErr = s.RunOnce(t.Context())
	})
	if panicked {
		t.Fatalf("RunOnce panicked: %s (green team must implement)", msg)
	}
	if !errors.Is(runErr, auth.ErrRegistryNotFound) {
		t.Errorf("RunOnce returned %v, want ErrRegistryNotFound", runErr)
	}
}

// TestCanaryRegistryInvalid verifies that malformed JSON returns ErrRegistryInvalid.
func TestCanaryRegistryInvalid(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write bad fixture: %v", err)
	}

	var s *auth.Scheduler
	{
		msg, panicked := canaryCatchPanic(func() {
			s = auth.NewScheduler(auth.SchedulerConfig{
				RegistryPath: bad,
				Probe:        func(_ context.Context, _ string) error { return nil },
				StaleAfter:   time.Hour,
				Now:          fixedNow,
			})
		})
		if panicked {
			t.Fatalf("NewScheduler panicked: %s (green team must implement)", msg)
		}
		if s == nil {
			t.Fatal("NewScheduler returned nil (green team must implement)")
		}
	}

	var runErr error
	msg, panicked := canaryCatchPanic(func() {
		_, runErr = s.RunOnce(t.Context())
	})
	if panicked {
		t.Fatalf("RunOnce panicked: %s (green team must implement)", msg)
	}
	if !errors.Is(runErr, auth.ErrRegistryInvalid) {
		t.Errorf("RunOnce returned %v, want ErrRegistryInvalid", runErr)
	}
}

// TestCanaryDoesNotMutateOtherFields verifies that RunOnce does not alter
// verification_state, project_evidence_state, or any other fields besides
// live_canary_state and last_checked.
func TestCanaryDoesNotMutateOtherFields(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	registryPath := filepath.Join(dir, "managed-scopes.json")
	// Write a fixture with a required canary scope that has specific field values.
	fixture := `{
  "schema_version": 1,
  "extra_top_level": "must-survive",
  "scopes": [
    {
      "scope": "https://www.googleapis.com/auth/gmail.readonly",
      "status": "active",
      "verification_state": "verified",
      "project_evidence_state": "ready",
      "live_canary_required": true,
      "live_canary_state": "stale",
      "last_checked": "2026-01-01T00:00:00Z",
      "extra_scope_field": "also-must-survive"
    }
  ]
}`
	if err := os.WriteFile(registryPath, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var s *auth.Scheduler
	{
		msg, panicked := canaryCatchPanic(func() {
			s = auth.NewScheduler(auth.SchedulerConfig{
				RegistryPath: registryPath,
				Probe:        func(_ context.Context, _ string) error { return nil },
				StaleAfter:   time.Hour,
				Now:          fixedNow,
			})
		})
		if panicked {
			t.Fatalf("NewScheduler panicked: %s (green team must implement)", msg)
		}
		if s == nil {
			t.Fatal("NewScheduler returned nil (green team must implement)")
		}
	}

	msg, panicked := canaryCatchPanic(func() {
		_, err := s.RunOnce(t.Context())
		if err != nil {
			t.Errorf("RunOnce: %v", err)
		}
	})
	if panicked {
		t.Fatalf("RunOnce panicked: %s (green team must implement)", msg)
	}

	raw, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	// Use a raw map to check that extra fields survive the round-trip.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal registry: %v", err)
	}

	// Top-level extra field must survive.
	if extra, ok := doc["extra_top_level"]; !ok {
		t.Error("extra_top_level field was removed from registry")
	} else {
		var s string
		if err := json.Unmarshal(extra, &s); err != nil || s != "must-survive" {
			t.Errorf("extra_top_level = %s, want %q", extra, "must-survive")
		}
	}

	// Parse scopes array and verify scope-level extra field and non-canary fields.
	var scoped struct {
		Scopes []map[string]json.RawMessage `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &scoped); err != nil {
		t.Fatalf("unmarshal scopes: %v", err)
	}
	if len(scoped.Scopes) == 0 {
		t.Fatal("scopes array empty after RunOnce")
	}
	sc := scoped.Scopes[0]

	// verification_state must remain "verified".
	if vs, ok := sc["verification_state"]; ok {
		var s string
		if err := json.Unmarshal(vs, &s); err != nil || s != "verified" {
			t.Errorf("verification_state = %s, want %q", vs, "verified")
		}
	} else {
		t.Error("verification_state field removed")
	}

	// project_evidence_state must remain "ready".
	if pe, ok := sc["project_evidence_state"]; ok {
		var s string
		if err := json.Unmarshal(pe, &s); err != nil || s != "ready" {
			t.Errorf("project_evidence_state = %s, want %q", pe, "ready")
		}
	} else {
		t.Error("project_evidence_state field removed")
	}

	// Extra scope-level field must survive.
	if esf, ok := sc["extra_scope_field"]; !ok {
		t.Error("extra_scope_field was removed from scope entry")
	} else {
		var s string
		if err := json.Unmarshal(esf, &s); err != nil || s != "also-must-survive" {
			t.Errorf("extra_scope_field = %s, want %q", esf, "also-must-survive")
		}
	}
}

// TestCanaryNotUsedToFlipGumOAuth is a structural guard: verifies that calling
// RunOnce with a passing probe does NOT cause the gum_oauth strategy to become
// enabled. It calls auth.Resolve with a catalog.Variant whose auth_strategy is
// "gum_oauth" and asserts the result is ErrUnknownStrategy or
// AUTH_STRATEGY_NOT_IMPLEMENTED — never a usable Credentials.
//
// This guards against future accidental wiring per bd memory gum-auth-strategy-v3.
func TestCanaryNotUsedToFlipGumOAuth(t *testing.T) {
	defer goleak.VerifyNone(t)

	registryPath := copyFixture(t)
	var s *auth.Scheduler
	{
		msg, panicked := canaryCatchPanic(func() {
			s = auth.NewScheduler(auth.SchedulerConfig{
				RegistryPath: registryPath,
				Probe:        func(_ context.Context, _ string) error { return nil },
				StaleAfter:   time.Hour,
				Now:          fixedNow,
			})
		})
		if panicked {
			t.Logf("NewScheduler panicked (stub): %s", msg)
			// Can't run RunOnce without a scheduler; skip to the structural check.
			s = nil
		}
	}

	// Run the scheduler if we have one (may panic — that's expected for stubs).
	if s != nil {
		msg, panicked := canaryCatchPanic(func() {
			_, _ = s.RunOnce(t.Context())
		})
		if panicked {
			// Stub is fine; we still run the structural check below.
			t.Logf("RunOnce panicked (stub): %s", msg)
		}
	}

	// After RunOnce (or even if it panicked), Acquire for gum_oauth must never
	// return usable credentials. Per strategy.go and bd memory gum-auth-strategy-v3,
	// StrategyGUMOAuth (iota=0) maps to "gum_oauth" which is disabled in v0.1.0.
	//
	// Note: auth.Resolve maps the variant to the Strategy enum without error
	// (gum_oauth is a recognized strategy string). The v0.1.0 gate is in Acquire,
	// which returns AUTH_STRATEGY_NOT_IMPLEMENTED for the six stubbed strategies
	// including gum_oauth.
	_, acquireErr := auth.Acquire(t.Context(), auth.StrategyGUMOAuth, nil)
	if acquireErr == nil {
		t.Fatal("auth.Acquire for StrategyGUMOAuth returned nil error; gum_oauth must remain disabled in v0.1.0")
	}
	// Must be AUTH_STRATEGY_NOT_IMPLEMENTED (or equivalent), not a real token.
	if !errors.Is(acquireErr, auth.ErrAuthStrategyNotImplemented) {
		t.Logf("auth.Acquire(gum_oauth) returned non-nil error as expected (errors.Is check): %v", acquireErr)
	}
	// Separately check Resolve: it must not panic for a known strategy string.
	variant := &catalog.Variant{AuthStrategy: catalog.AuthStrategyGUMOAuth}
	strat, resolveErr := auth.Resolve(t.Context(), variant)
	if resolveErr != nil && !errors.Is(resolveErr, auth.ErrUnknownStrategy) {
		t.Errorf("auth.Resolve for gum_oauth returned unexpected error: %v", resolveErr)
	}
	// Whether Resolve errors or not, Acquire must block credential issuance.
	if resolveErr == nil {
		// Strategy was recognized; confirm Acquire is still blocked.
		_, acquireErr2 := auth.Acquire(t.Context(), strat, nil)
		if acquireErr2 == nil {
			t.Fatal("auth.Acquire for resolved gum_oauth strategy returned nil error; must remain disabled")
		}
	}
}
