package auth_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/auth"
)

// TestCanaryRunOnceReadFailureSurfacesAsInvalid pins the
// `os.ReadFile err && !IsNotExist → ErrRegistryInvalid wrap` arm.
// Plant a directory at the RegistryPath so ReadFile EISDIRs (not
// ENOENT) — that branch MUST surface ErrRegistryInvalid rather than
// the not-found sentinel, since downstream operators interpret the two
// sentinels very differently (one is "registry missing — bootstrap?",
// the other is "filesystem is broken — bail").
func TestCanaryRunOnceReadFailureSurfacesAsInvalid(t *testing.T) {
	tmp := t.TempDir()
	registryPath := filepath.Join(tmp, "managed-scopes.json")
	if err := os.MkdirAll(registryPath, 0o700); err != nil {
		t.Fatalf("plant dir blocker: %v", err)
	}

	s := auth.NewScheduler(auth.SchedulerConfig{
		RegistryPath: registryPath,
		Probe:        func(_ context.Context, _ string) error { return nil },
	})

	_, err := s.RunOnce(t.Context())
	if err == nil {
		t.Fatal("RunOnce(dir at registry path)=nil err; want ErrRegistryInvalid")
	}
	if !errors.Is(err, auth.ErrRegistryInvalid) {
		t.Errorf("err=%v; want ErrRegistryInvalid (NOT ErrRegistryNotFound — operator semantics differ)", err)
	}
	if errors.Is(err, auth.ErrRegistryNotFound) {
		t.Errorf("err=%v; must NOT be ErrRegistryNotFound (EISDIR is corruption, not absence)", err)
	}
}

// TestCanaryRunOnceMissingScopesKeySurfacesAsInvalid pins the
// `scopes key absent from registry JSON → ErrRegistryInvalid` arm.
// A well-formed but schema-incomplete registry (no "scopes" key) is
// not a "not found" condition — it's a structural mismatch the
// operator must repair.
func TestCanaryRunOnceMissingScopesKeySurfacesAsInvalid(t *testing.T) {
	tmp := t.TempDir()
	registryPath := filepath.Join(tmp, "managed-scopes.json")
	if err := os.WriteFile(registryPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	s := auth.NewScheduler(auth.SchedulerConfig{
		RegistryPath: registryPath,
		Probe:        func(_ context.Context, _ string) error { return nil },
	})

	_, err := s.RunOnce(t.Context())
	if !errors.Is(err, auth.ErrRegistryInvalid) {
		t.Errorf("err=%v; want ErrRegistryInvalid for missing scopes key", err)
	}
}

// TestCanaryRunOnceScopesWrongTypeSurfacesAsInvalid pins the
// `scopes value not []any → ErrRegistryInvalid` arm. An operator who
// hand-edited the registry might write `"scopes": "all"` by accident;
// the canary MUST reject that rather than silently treating it as
// no-scopes (which would let drift accumulate undetected).
func TestCanaryRunOnceScopesWrongTypeSurfacesAsInvalid(t *testing.T) {
	tmp := t.TempDir()
	registryPath := filepath.Join(tmp, "managed-scopes.json")
	if err := os.WriteFile(registryPath, []byte(`{"scopes":"not-a-slice"}`), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	s := auth.NewScheduler(auth.SchedulerConfig{
		RegistryPath: registryPath,
		Probe:        func(_ context.Context, _ string) error { return nil },
	})

	_, err := s.RunOnce(t.Context())
	if !errors.Is(err, auth.ErrRegistryInvalid) {
		t.Errorf("err=%v; want ErrRegistryInvalid for non-array scopes value", err)
	}
}

// TestCanaryRunOnceWriteFailureSurfacesAsError pins the write-back failure
// arm. RunOnce persists the refreshed registry through fsatomic; if that write
// fails (here: a read-only registry directory, so the temp file cannot be
// created), the error MUST surface so the operator can repair, not silently
// succeed and lose the new state.
func TestCanaryRunOnceWriteFailureSurfacesAsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not deny writes")
	}
	tmp := t.TempDir()
	registryDir := filepath.Join(tmp, "registry")
	if err := os.Mkdir(registryDir, 0o700); err != nil {
		t.Fatalf("mkdir registry dir: %v", err)
	}
	registryPath := filepath.Join(registryDir, "managed-scopes.json")
	if err := os.WriteFile(registryPath, []byte(`{"scopes":[]}`), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	// Read-only directory: the registry itself stays readable, so RunOnce
	// gets all the way to the write-back before it fails.
	if err := os.Chmod(registryDir, 0o500); err != nil {
		t.Fatalf("chmod registry dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(registryDir, 0o700) })

	s := auth.NewScheduler(auth.SchedulerConfig{
		RegistryPath: registryPath,
		Probe:        func(_ context.Context, _ string) error { return nil },
	})

	_, err := s.RunOnce(t.Context())
	if err == nil {
		t.Fatal("RunOnce(read-only registry dir)=nil; want a write failure")
	}
	// Only the wrap prefix is asserted: the underlying errno text varies
	// across platforms.
	if !strings.Contains(err.Error(), "canary: write registry") {
		t.Errorf("err=%v; want 'canary: write registry' wrap", err)
	}
}

// TestCanaryRunOnceMalformedEntrySkipsRatherThanErrors pins TWO
// `continue` arms in the per-scope loop: (1) scopeRaw is not a
// map[string]any (e.g. raw string in the array) and (2) scope name is
// "". The function MUST skip these without failing the whole batch —
// one bad row should not block the rest of the canary sweep.
func TestCanaryRunOnceMalformedEntrySkipsRatherThanErrors(t *testing.T) {
	tmp := t.TempDir()
	registryPath := filepath.Join(tmp, "managed-scopes.json")
	registryJSON := `{
		"scopes": [
			"not-a-map",
			{"scope": "", "live_canary_required": true},
			{"scope": "good.scope", "live_canary_required": true}
		]
	}`
	if err := os.WriteFile(registryPath, []byte(registryJSON), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	probeCalls := 0
	s := auth.NewScheduler(auth.SchedulerConfig{
		RegistryPath: registryPath,
		Probe: func(_ context.Context, scope string) error {
			probeCalls++
			if scope != "good.scope" {
				t.Errorf("probe called for unexpected scope %q", scope)
			}
			return nil
		},
	})

	outcomes, err := s.RunOnce(t.Context())
	if err != nil {
		t.Fatalf("RunOnce: %v; want skip-and-continue for malformed rows", err)
	}
	if probeCalls != 1 {
		t.Errorf("probe called %d times; want exactly 1 (the good.scope row)", probeCalls)
	}
	if _, ok := outcomes["good.scope"]; !ok {
		t.Errorf("outcomes missing good.scope: %v", outcomes)
	}
}
