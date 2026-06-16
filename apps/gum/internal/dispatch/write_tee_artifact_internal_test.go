package dispatch

import (
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
)

// TestWriteTeeArtifactShortCircuits pins every guard branch in
// writeTeeArtifact: empty ProfileDir, any nil input, empty body, "off"
// / "failures" with success status, and unknown mode all MUST return
// (nil, nil) without touching the disk. A regression here could
// silently spam <profileDir>/tee/* with phantom artifacts.
func TestWriteTeeArtifactShortCircuits(t *testing.T) {
	dir := t.TempDir()
	mkDispatcher := func(profDir, mode string) *dispatcher {
		return &dispatcher{
			teeConfig: TeeConfig{ProfileDir: profDir, Mode: mode, RetentionHours: 24},
		}
	}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v1"}}
	inv := &Invocation{OpID: "gum.code"}
	resp := &Response{Body: []byte(`{"k":"v"}`), StatusCode: 200, Format: "json"}

	t.Run("empty_profile_dir", func(t *testing.T) {
		d := mkDispatcher("", "always")
		got, err := d.writeTeeArtifact(inv, rv, nil, resp)
		if err != nil || got != nil {
			t.Errorf("got=(%v,%v); want (nil,nil)", got, err)
		}
	})

	t.Run("nil_invocation", func(t *testing.T) {
		d := mkDispatcher(dir, "always")
		got, err := d.writeTeeArtifact(nil, rv, nil, resp)
		if err != nil || got != nil {
			t.Errorf("got=(%v,%v); want (nil,nil)", got, err)
		}
	})

	t.Run("nil_resolved_variant", func(t *testing.T) {
		d := mkDispatcher(dir, "always")
		got, err := d.writeTeeArtifact(inv, nil, nil, resp)
		if err != nil || got != nil {
			t.Errorf("got=(%v,%v); want (nil,nil)", got, err)
		}
	})

	t.Run("empty_body", func(t *testing.T) {
		d := mkDispatcher(dir, "always")
		emptyResp := &Response{Body: nil, StatusCode: 200, Format: "json"}
		got, err := d.writeTeeArtifact(inv, rv, nil, emptyResp)
		if err != nil || got != nil {
			t.Errorf("got=(%v,%v); want (nil,nil)", got, err)
		}
	})

	t.Run("mode_off_skips", func(t *testing.T) {
		d := mkDispatcher(dir, "off")
		// Override the profile/Recovery so effectiveTeeMode resolves to "off".
		got, err := d.writeTeeArtifact(inv, rv, nil, resp)
		if err != nil || got != nil {
			t.Errorf("got=(%v,%v); want (nil,nil)", got, err)
		}
	})

	t.Run("mode_failures_with_2xx_skips", func(t *testing.T) {
		d := mkDispatcher(dir, "failures")
		got, err := d.writeTeeArtifact(inv, rv, nil, resp)
		if err != nil || got != nil {
			t.Errorf("got=(%v,%v); want (nil,nil)", got, err)
		}
	})

	t.Run("unknown_mode_treated_as_off", func(t *testing.T) {
		d := mkDispatcher(dir, "weird-mode")
		// Use an empty profile so effectiveTeeMode falls through to the
		// raw Mode, which is "weird-mode" → default branch.
		got, err := d.writeTeeArtifact(inv, rv, nil, resp)
		if err != nil || got != nil {
			t.Errorf("got=(%v,%v); want (nil,nil)", got, err)
		}
	})
}

// TestWriteTeeArtifactCredsFingerprintWins pins the creds-fingerprint
// override path: when creds is non-nil AND has a SubjectFingerprint,
// it takes precedence over inv.AuthSubjectFingerprint. The hash that
// indexes the artifact MUST be deterministic per (op, args, subject),
// so this branch swap would silently break dedupe.
func TestWriteTeeArtifactCredsFingerprintWins(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "default")
	d := &dispatcher{teeConfig: TeeConfig{ProfileDir: dir, Mode: "always", RetentionHours: 24}}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v1"}}
	inv := &Invocation{OpID: "gum.code", AuthSubjectFingerprint: "fp-inv"}
	resp := &Response{Body: []byte(`{"k":"v"}`), StatusCode: 200, Format: "json"}
	creds := &Credentials{SubjectFingerprint: "fp-creds"}

	art1, err := d.writeTeeArtifact(inv, rv, creds, resp)
	if err != nil {
		t.Fatalf("write with creds: %v", err)
	}
	if art1 == nil {
		t.Fatal("artifact is nil")
	}

	// Recompute with no creds; hash MUST differ because the fingerprint
	// switched from "fp-creds" to "fp-inv".
	art2, err := d.writeTeeArtifact(inv, rv, nil, resp)
	if err != nil {
		t.Fatalf("write without creds: %v", err)
	}
	if art1.Hash == art2.Hash {
		t.Errorf("hashes match (creds fingerprint did NOT win): %q", art1.Hash)
	}
}

// TestWriteTeeArtifactProfileRecoveryWiringSurfacesPath pins the
// happy "always" write: the returned teeArtifact carries the path
// on disk plus the profile's Recovery hint, so the dispatch caller
// can attach `recovery` to the §9.0 envelope.
func TestWriteTeeArtifactProfileRecoveryWiringSurfacesPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "default")
	d := &dispatcher{teeConfig: TeeConfig{ProfileDir: dir, RetentionHours: 24}}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v1"}}
	inv := &Invocation{
		OpID:          "gum.code",
		OutputProfile: &profile.Profile{Recovery: "local_artifact"},
	}
	resp := &Response{Body: []byte(`{"k":"v"}`), StatusCode: 200, Format: "json"}

	art, err := d.writeTeeArtifact(inv, rv, nil, resp)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if art == nil || art.Path == "" {
		t.Fatalf("artifact path empty; got %+v", art)
	}
	if art.Recovery != "local_artifact" {
		t.Errorf("Recovery=%q; want local_artifact", art.Recovery)
	}
	if art.Size != int64(len(resp.Body)) {
		t.Errorf("Size=%d; want %d", art.Size, len(resp.Body))
	}
}
