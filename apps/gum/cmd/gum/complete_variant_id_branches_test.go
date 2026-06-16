package main

import (
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// TestCompleteVariantIDForOpNoArgsReturnsEmpty pins the
// `len(args) == 0` arm: invoking variant_id completion before the
// caller has typed an op_id MUST return an empty proposal list (not
// every variant in the catalog), so the shell doesn't spam an op-less
// user with unrelated candidates.
func TestCompleteVariantIDForOpNoArgsReturnsEmpty(t *testing.T) {
	out, _ := completeVariantIDForOp(nil, []string{}, "")
	if len(out) != 0 {
		t.Errorf("len(out)=%d; want 0 when args empty", len(out))
	}
}

// TestCompleteVariantIDForOpUnknownOpIDFallsThrough pins the inner
// `op.OpID != opID → continue` arm AND the trailing
// `return nil, NoFileComp` fall-through after the loop: if no op
// matches the typed prefix, the completion MUST be empty rather
// than panic or return a stale match.
func TestCompleteVariantIDForOpUnknownOpIDFallsThrough(t *testing.T) {
	out, _ := completeVariantIDForOp(nil, []string{"nonexistent.op.id"}, "")
	if len(out) != 0 {
		t.Errorf("len(out)=%d; want 0 for unknown op_id", len(out))
	}
}

// TestCompleteVariantIDForOpEmptyCatalogReturnsEmpty pins the
// `snap == nil` arm: if the embedded catalog blob is unavailable
// (e.g. a build emitted with the catalog stripped, or a test that
// mutated embedded.CatalogJSON), completion MUST return empty rather
// than NPE on snap.Ops.
func TestCompleteVariantIDForOpEmptyCatalogReturnsEmpty(t *testing.T) {
	saved := embedded.CatalogJSON
	t.Cleanup(func() { embedded.CatalogJSON = saved })
	embedded.CatalogJSON = nil

	out, _ := completeVariantIDForOp(nil, []string{"gmail.users.messages.list"}, "")
	if len(out) != 0 {
		t.Errorf("len(out)=%d; want 0 when embedded catalog is empty", len(out))
	}
}
