package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestWriteTeeArtifactSecretLoadFailureWrapsAsTeeSecretErr pins the
// `tee.LoadOrCreateSecret err → "dispatch: tee secret:" wrap` arm
// (tee.go:105-107). Reached when ProfileDir is invalid for secret
// creation — here a regular file is planted at the parent so
// MkdirAll on the child returns ENOTDIR. The "dispatch: tee secret:"
// wrap is the operator's grep handle to triage secret-init failures
// separately from hash/write failures downstream — without the
// explicit wrap they'd all surface as opaque tee errors.
func TestWriteTeeArtifactSecretLoadFailureWrapsAsTeeSecretErr(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	// ProfileDir is a child of the blocker file → MkdirAll inside
	// LoadOrCreateSecret fails with ENOTDIR.
	profileDir := filepath.Join(blocker, "profile")

	d := &dispatcher{teeConfig: TeeConfig{ProfileDir: profileDir, Mode: "always", RetentionHours: 24}}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v1"}}
	inv := &Invocation{OpID: "gum.code"}
	resp := &Response{Body: []byte(`{"k":"v"}`), StatusCode: 200, Format: "json"}

	got, err := d.writeTeeArtifact(inv, rv, nil, resp)
	if err == nil {
		t.Fatalf("writeTeeArtifact(blocker)=%+v nil err; want secret wrap", got)
	}
	if got != nil {
		t.Errorf("got=%+v; want nil artifact on err", got)
	}
	if !strings.Contains(err.Error(), "dispatch: tee secret") {
		t.Errorf("err=%q; want 'dispatch: tee secret:' wrap", err)
	}
}

// TestWriteTeeArtifactHashFailureWrapsAsTeeHashErr pins the
// `tee.ComputeHash err → "dispatch: tee hash:" wrap` arm
// (tee.go:121-123). ComputeHash calls jcs.Marshal on the canonical
// args; a chan-valued arg trips jcs's ErrJCSUnsupportedType. In
// production an unencodable arg shouldn't reach this stage (the
// adapter would have rejected it earlier), but the defensive wrap
// keeps the surface debuggable when an adapter passes through a
// malformed Args map.
func TestWriteTeeArtifactHashFailureWrapsAsTeeHashErr(t *testing.T) {
	profileDir := filepath.Join(t.TempDir(), "default")
	d := &dispatcher{teeConfig: TeeConfig{ProfileDir: profileDir, Mode: "always", RetentionHours: 24}}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v1"}}
	// chan is unsupported by jcs → ComputeHash returns the wrapped err.
	inv := &Invocation{OpID: "gum.code", Args: map[string]any{"bad": make(chan int)}}
	resp := &Response{Body: []byte(`{"k":"v"}`), StatusCode: 200, Format: "json"}

	got, err := d.writeTeeArtifact(inv, rv, nil, resp)
	if err == nil {
		t.Fatalf("writeTeeArtifact(chan arg)=%+v nil err; want hash wrap", got)
	}
	if got != nil {
		t.Errorf("got=%+v; want nil artifact on err", got)
	}
	if !strings.Contains(err.Error(), "dispatch: tee hash") {
		t.Errorf("err=%q; want 'dispatch: tee hash:' wrap", err)
	}
}

// TestWriteTeeArtifactWriteFailureWrapsAsTeeWriteErr pins the
// `tee.Write err → "dispatch: tee write:" wrap` arm (tee.go:125-127).
// Reached when secret creation + hash both succeed but the artifact
// directory layer can't be created — here a regular file is planted
// at <profileDir>/tee so tee.Write's MkdirAll on
// <profileDir>/tee/<day>/<opID> fails with ENOTDIR.
//
// SecretPath is <profileDir>/tee.secret (different from
// <profileDir>/tee), so the planted blocker doesn't interfere with
// LoadOrCreateSecret — only with tee.Write.
func TestWriteTeeArtifactWriteFailureWrapsAsTeeWriteErr(t *testing.T) {
	profileDir := filepath.Join(t.TempDir(), "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	// Plant a regular file at <profileDir>/tee — ArtifactDir
	// (profileDir/tee/<day>/<opID>) then fails to mkdir.
	teeBlocker := filepath.Join(profileDir, "tee")
	if err := os.WriteFile(teeBlocker, []byte("file"), 0o600); err != nil {
		t.Fatalf("plant tee blocker: %v", err)
	}

	d := &dispatcher{teeConfig: TeeConfig{ProfileDir: profileDir, Mode: "always", RetentionHours: 24}}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v1"}}
	inv := &Invocation{OpID: "gum.code"}
	resp := &Response{Body: []byte(`{"k":"v"}`), StatusCode: 200, Format: "json"}

	got, err := d.writeTeeArtifact(inv, rv, nil, resp)
	if err == nil {
		t.Fatalf("writeTeeArtifact(tee blocker)=%+v nil err; want write wrap", got)
	}
	if got != nil {
		t.Errorf("got=%+v; want nil artifact on err", got)
	}
	if !strings.Contains(err.Error(), "dispatch: tee write") {
		t.Errorf("err=%q; want 'dispatch: tee write:' wrap", err)
	}
}
