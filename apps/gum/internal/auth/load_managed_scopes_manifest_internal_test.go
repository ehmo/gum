package auth

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// TestLoadManagedScopesManifestNilBodyFallsBackToEmbed pins the
// nil-body arm: callers (production code at gum_oauth.go:80/138) pass
// nil so the helper substitutes the embedded JSON. A regression that
// short-circuited on nil would break gum_oauth at startup.
func TestLoadManagedScopesManifestNilBodyFallsBackToEmbed(t *testing.T) {
	// Sanity: the embed must be present and valid in normal builds.
	if len(embedded.AuthManagedScopesJSON) == 0 {
		t.Skip("embed empty; cannot exercise nil-body fallback")
	}
	m, err := loadManagedScopesManifest(nil)
	if err != nil {
		t.Fatalf("err=%v; want nil from embedded body", err)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("SchemaVersion=%d; want 1", m.SchemaVersion)
	}
}

// TestLoadManagedScopesManifestEmptyBodyErrors pins the "empty bytes"
// guard: an explicitly-empty (but non-nil) body MUST NOT fall back to
// the embed — that would mask a caller bug. Surface a clear
// diagnostic instead.
func TestLoadManagedScopesManifestEmptyBodyErrors(t *testing.T) {
	// Force the embed to be empty too so the nil-substitution arm can't
	// rescue the empty body.
	saved := embedded.AuthManagedScopesJSON
	t.Cleanup(func() { embedded.AuthManagedScopesJSON = saved })
	embedded.AuthManagedScopesJSON = nil

	_, err := loadManagedScopesManifest(nil)
	if err == nil {
		t.Fatal("want empty-manifest error; got nil")
	}
	if !strings.Contains(err.Error(), "manifest is empty") {
		t.Errorf("err=%v; want 'manifest is empty'", err)
	}
}

// TestLoadManagedScopesManifestUnparseableErrors pins the JSON-decode
// arm: corrupt bytes MUST surface the decode error, not propagate
// upward as a half-parsed manifest.
func TestLoadManagedScopesManifestUnparseableErrors(t *testing.T) {
	_, err := loadManagedScopesManifest([]byte("{not json"))
	if err == nil {
		t.Fatal("want decode error; got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("err=%v; want 'decode' wrap", err)
	}
}

// TestLoadManagedScopesManifestWrongSchemaVersion pins the
// schema-version guard: a future-format manifest MUST be rejected so
// the gate never silently dispatches against an unknown shape.
func TestLoadManagedScopesManifestWrongSchemaVersion(t *testing.T) {
	_, err := loadManagedScopesManifest([]byte(`{"schema_version": 2}`))
	if err == nil {
		t.Fatal("want schema-version error; got nil")
	}
	if !strings.Contains(err.Error(), "schema_version") || !strings.Contains(err.Error(), "want 1") {
		t.Errorf("err=%v; want schema_version=2/want 1 diag", err)
	}
}
