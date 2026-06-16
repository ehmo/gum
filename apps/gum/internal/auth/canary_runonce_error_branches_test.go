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

// TestCanaryRunOnceWriteTmpFailureSurfacesAsError pins the
// `os.WriteFile(tmpPath, ...) err → wrap` arm. The write-back step
// uses tmp+rename for atomicity; if the tmp write fails (e.g. a
// blocker directory already exists at the .tmp suffix path because a
// prior crash left it), the error MUST surface so the operator can
// repair, not silently succeed and lose the new state.
func TestCanaryRunOnceWriteTmpFailureSurfacesAsError(t *testing.T) {
	tmp := t.TempDir()
	registryPath := filepath.Join(tmp, "managed-scopes.json")
	if err := os.WriteFile(registryPath, []byte(`{"scopes":[]}`), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	// Plant a directory at the .tmp suffix path so os.WriteFile
	// returns EISDIR on its open(O_CREAT|O_TRUNC) call.
	if err := os.MkdirAll(registryPath+".tmp", 0o700); err != nil {
		t.Fatalf("plant tmp blocker: %v", err)
	}

	s := auth.NewScheduler(auth.SchedulerConfig{
		RegistryPath: registryPath,
		Probe:        func(_ context.Context, _ string) error { return nil },
	})

	_, err := s.RunOnce(t.Context())
	if err == nil {
		t.Fatal("RunOnce(tmp dir-blocker)=nil; want write-tmp surface")
	}
	// We only assert the wrap prefix because the underlying EISDIR
	// text varies cross-platform.
	if !strings.Contains(err.Error(), "canary: write tmp") {
		t.Errorf("err=%v; want 'canary: write tmp' wrap", err)
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
