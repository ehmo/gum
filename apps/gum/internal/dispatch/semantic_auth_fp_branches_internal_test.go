package dispatch

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
)

// TestSemanticAuthFPCredsFallback pins the
// `inv.AuthSubjectFingerprint == "" && creds.SubjectFingerprint != ""
// → return creds.SubjectFingerprint` arm. The semantic-cache key
// SHOULD prefer the Invocation's pre-computed fingerprint (faster path
// when the dispatcher pre-hashed) but MUST fall through to the live
// Credentials fingerprint so two callers with different ADC subjects
// don't share cache entries.
func TestSemanticAuthFPCredsFallback(t *testing.T) {
	inv := &Invocation{OpID: "x"} // no AuthSubjectFingerprint
	creds := &Credentials{SubjectFingerprint: "sha256:fp-from-creds"}

	if got := semanticAuthFP(inv, creds); got != "sha256:fp-from-creds" {
		t.Errorf("got %q; want creds-fallback fingerprint", got)
	}
}

// TestSemanticFieldsOutputProfileEmptyProjectionReturnsEmpty pins the
// `len(Projection) == 0 && len(KeepFields) == 0 → ""` arm. An
// OutputProfile that exists but declares no field-mask projection
// produces no cache-key contribution; spec §10.3 treats it identical
// to no projection at all, so the key MUST be empty rather than e.g.
// the profile name (which would leak shaper identity into cache
// segmentation).
func TestSemanticFieldsOutputProfileEmptyProjectionReturnsEmpty(t *testing.T) {
	inv := &Invocation{OpID: "x", OutputProfile: &profile.Profile{Name: "p"}}
	if got := semanticFields(inv); got != "" {
		t.Errorf("semanticFields(empty projection)=%q; want \"\"", got)
	}
}

// TestServiceFamilyForNilSnapshotReturnsEmpty pins the
// `d.snapshot == nil → ""` arm: dispatcher.serviceFamilyFor MUST NOT
// panic if invoked before snapshot was wired (e.g. transient
// configuration window during reload). Returns empty string as a safe
// fallback — service-family labels are advisory in error envelopes,
// not load-bearing.
func TestServiceFamilyForNilSnapshotReturnsEmpty(t *testing.T) {
	d := &dispatcher{snapshot: nil}
	if got := d.serviceFamilyFor("any.op"); got != "" {
		t.Errorf("serviceFamilyFor(nil snapshot)=%q; want \"\"", got)
	}
}

// TestServiceFamilyForUnknownOpReturnsEmpty pins the
// `findOp == nil → ""` arm: an opID absent from the snapshot MUST also
// surface as "" rather than panic on nil dereference. Symmetric to
// the nil-snapshot guard.
func TestServiceFamilyForUnknownOpReturnsEmpty(t *testing.T) {
	d := &dispatcher{
		snapshot: &catalog.Catalog{
			CatalogSchemaVersion: 1,
			Ops:                  []catalog.Op{},
		},
	}
	if got := d.serviceFamilyFor("does.not.exist"); got != "" {
		t.Errorf("serviceFamilyFor(unknown)=%q; want \"\"", got)
	}
}
