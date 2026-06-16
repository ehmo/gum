package dispatch

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// readOnlyOp builds a minimal raw-http read op suitable for
// applyReadOnlyAllowlist gate-eligibility. Per-test mutation is done by
// the caller before passing to the function under test.
func readOnlyOp() *catalog.Op {
	return &catalog.Op{
		OpID:             "admin.directory.users.list",
		DefaultVariantID: "admin.v1.rawhttp.list",
		Variants: []catalog.Variant{
			{
				VariantID:     "admin.v1.rawhttp.list",
				BackendKind:   catalog.BackendKindRawHTTP,
				RiskClass:     catalog.RiskClassRead,
				InterfaceKind: catalog.InterfaceKindDiscoveryREST,
			},
		},
	}
}

// TestApplyReadOnlyAllowlistDefaultVariantMissingShortCircuits pins
// the `defaultVariant == nil → return false` arm. If catalog validation
// has been bypassed (or temporarily inconsistent during a snapshot
// swap) and DefaultVariantID does not resolve to any variant, the
// allowlist MUST NOT fire — falling through would call `.RiskClass` on
// a nil pointer.
func TestApplyReadOnlyAllowlistDefaultVariantMissingShortCircuits(t *testing.T) {
	op := readOnlyOp()
	op.DefaultVariantID = "this.variant.does.not.exist"
	policy := &ProfilePolicy{
		UnknownReadParamsAllowlist: map[string][]string{op.OpID: {"showDeleted"}},
	}

	rem, warn, fired := applyReadOnlyAllowlist(op, policy, []string{"showDeleted"}, false, false)
	if fired {
		t.Error("fired=true with no default variant; want false")
	}
	if warn != "" {
		t.Errorf("warn=%q; want empty", warn)
	}
	if len(rem) != 1 || rem[0] != "showDeleted" {
		t.Errorf("rem=%v; want unknown passed through unchanged", rem)
	}
}

// TestApplyReadOnlyAllowlistNonReadRiskShortCircuits pins the
// `defaultVariant.RiskClass != RiskClassRead → return false` arm. The
// allowlist is gated to read-class default variants per spec §5.7 —
// a write/destructive default must NOT receive the pass-through, even
// if its op_id is keyed in the allowlist map.
func TestApplyReadOnlyAllowlistNonReadRiskShortCircuits(t *testing.T) {
	op := readOnlyOp()
	op.Variants[0].RiskClass = catalog.RiskClassWrite
	policy := &ProfilePolicy{
		UnknownReadParamsAllowlist: map[string][]string{op.OpID: {"showDeleted"}},
	}

	_, _, fired := applyReadOnlyAllowlist(op, policy, []string{"showDeleted"}, false, false)
	if fired {
		t.Error("fired=true on write-class default variant; want false (spec §5.7 read-only gate)")
	}
}

// TestApplyReadOnlyAllowlistUnsupportedBackendShortCircuits pins the
// `BackendKind != RawHTTP && != DiscoveryREST → return false` arm.
// The allowlist only applies to long-tail (raw-http / discovery-rest)
// reads — typed-sdk default variants have their own param contract
// (the SDK won't accept undeclared keys), so the warning would be
// misleading there.
func TestApplyReadOnlyAllowlistUnsupportedBackendShortCircuits(t *testing.T) {
	op := readOnlyOp()
	op.Variants[0].BackendKind = catalog.BackendKindTypedRestSDK
	policy := &ProfilePolicy{
		UnknownReadParamsAllowlist: map[string][]string{op.OpID: {"showDeleted"}},
	}

	_, _, fired := applyReadOnlyAllowlist(op, policy, []string{"showDeleted"}, false, false)
	if fired {
		t.Error("fired=true on typed-sdk default variant; want false (gate is raw-http/discovery-rest only)")
	}
}

// TestApplyReadOnlyAllowlistNoOverlapReturnsOriginalUnknowns pins the
// `len(waived) == 0 → return original unknowns, no warning` arm. When
// none of the unknown keys match the allowlist, the function MUST
// return the unknowns unchanged (so the downstream INVALID_ARGS path
// can surface them) — emitting an empty warning would be misleading.
func TestApplyReadOnlyAllowlistNoOverlapReturnsOriginalUnknowns(t *testing.T) {
	op := readOnlyOp()
	policy := &ProfilePolicy{
		UnknownReadParamsAllowlist: map[string][]string{op.OpID: {"allowed.but.absent"}},
	}

	rem, warn, fired := applyReadOnlyAllowlist(op, policy, []string{"absolutely.unrelated"}, false, false)
	if fired {
		t.Error("fired=true with zero overlap; want false")
	}
	if warn != "" {
		t.Errorf("warn=%q; want empty (no waived keys to mention)", warn)
	}
	if len(rem) != 1 || rem[0] != "absolutely.unrelated" {
		t.Errorf("rem=%v; want original unknowns passed through unchanged", rem)
	}
}
