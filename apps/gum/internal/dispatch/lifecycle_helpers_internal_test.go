package dispatch

import (
	"context"
	"slices"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestOpIDCandidatesWithoutASnapshot covers the nil-snapshot guard. A
// dispatcher built without a catalog must yield no suggestions rather than
// panic while building an OP_NOT_FOUND envelope.
func TestOpIDCandidatesWithoutASnapshot(t *testing.T) {
	t.Parallel()
	d := &dispatcher{}
	if got := d.opIDCandidates(); got != nil {
		t.Errorf("opIDCandidates with no snapshot = %v; want nil", got)
	}
	if got := suggestOpIDs("gmail.list", d.opIDCandidates(), MaxOpSuggestions); len(got) != 0 {
		t.Errorf("suggestOpIDs over an absent snapshot = %v; want empty", got)
	}
}

// TestResolveVariantRejectsAZeroVariantOp covers the malformed-catalog guard.
// Every path below it indexes Variants[0], so a hand-built catalog op with no
// variants must produce VARIANT_NOT_FOUND instead of a panic.
func TestResolveVariantRejectsAZeroVariantOp(t *testing.T) {
	t.Parallel()
	d := &dispatcher{snapshot: &catalog.Catalog{Ops: []catalog.Op{{OpID: "x.empty"}}}}

	rv, serr := d.resolveVariant(context.Background(), &Invocation{OpID: "x.empty"})
	if rv != nil {
		t.Errorf("resolveVariant returned %+v; want nil", rv)
	}
	if serr == nil {
		t.Fatal("resolveVariant on a zero-variant op returned no error")
	}
	if serr.ErrCode != ErrCodeVariantNotFound {
		t.Errorf("error_code = %s; want %s", serr.ErrCode, ErrCodeVariantNotFound)
	}
	if serr.Detail["op_id"] != "x.empty" {
		t.Errorf("detail op_id = %v; want x.empty", serr.Detail["op_id"])
	}
}

// twoStableVariantOp builds an op whose default_variant_id matches nothing and
// whose two active variants share the top stability, so variant selection falls
// all the way through to the interface-kind tie-break.
func twoStableVariantOp(kinds [2]catalog.InterfaceKind) catalog.Op {
	return catalog.Op{
		OpID:             "x.tied",
		DefaultVariantID: "", // unmatched: forces the stability/tie-break path
		Variants: []catalog.Variant{
			{VariantID: "a", Stability: catalog.StabilityStable, InterfaceKind: kinds[0], RiskClass: catalog.RiskClassRead},
			{VariantID: "b", Stability: catalog.StabilityStable, InterfaceKind: kinds[1], RiskClass: catalog.RiskClassRead},
		},
	}
}

// TestPolicyVariantTieBreakArms covers the tail of policyVariant: the
// interface-kind preference resolves a tie, and an unresolvable tie returns nil
// so resolveVariant can raise AMBIGUOUS_VARIANT before anything executes.
func TestPolicyVariantTieBreakArms(t *testing.T) {
	t.Parallel()
	op := twoStableVariantOp([2]catalog.InterfaceKind{
		catalog.InterfaceKindDiscoveryREST,
		catalog.InterfaceKindPluginMCP,
	})

	preferred := &dispatcher{
		snapshot:                &catalog.Catalog{Ops: []catalog.Op{op}},
		preferredInterfaceKinds: []string{string(catalog.InterfaceKindPluginMCP)},
	}
	v := preferred.policyVariant(&Invocation{OpID: "x.tied"})
	if v == nil {
		t.Fatal("policyVariant returned nil with a matching interface-kind preference")
	}
	if v.VariantID != "b" {
		t.Errorf("gated variant = %q; want the preferred kind's variant b", v.VariantID)
	}

	ambiguous := &dispatcher{snapshot: &catalog.Catalog{Ops: []catalog.Op{op}}}
	if v := ambiguous.policyVariant(&Invocation{OpID: "x.tied"}); v != nil {
		t.Errorf("policyVariant on an unresolvable tie = %q; want nil", v.VariantID)
	}

	unknownPref := &dispatcher{
		snapshot:                &catalog.Catalog{Ops: []catalog.Op{op}},
		preferredInterfaceKinds: []string{"nothing_matches"},
	}
	if v := unknownPref.policyVariant(&Invocation{OpID: "x.tied"}); v != nil {
		t.Errorf("policyVariant with a non-matching preference = %q; want nil", v.VariantID)
	}
}

// TestPolicyVariantAllQuarantined covers the empty-active return: the risk gate
// is skipped because resolveVariant raises VARIANT_QUARANTINED first.
func TestPolicyVariantAllQuarantined(t *testing.T) {
	t.Parallel()
	op := twoStableVariantOp([2]catalog.InterfaceKind{
		catalog.InterfaceKindDiscoveryREST,
		catalog.InterfaceKindPluginMCP,
	})
	op.Variants[0].Quarantined = true
	op.Variants[1].Quarantined = true

	d := &dispatcher{snapshot: &catalog.Catalog{Ops: []catalog.Op{op}}}
	if v := d.policyVariant(&Invocation{OpID: "x.tied"}); v != nil {
		t.Errorf("policyVariant with every variant quarantined = %q; want nil", v.VariantID)
	}
}

// TestValidateParamsEnforcesRequiredPathFields covers the Discovery-enriched
// branch: a required path param has no URL template substitution, so its
// absence must be a local INVALID_ARGS rather than a malformed upstream URL.
// The same name declared in both lists must be reported once.
func TestValidateParamsEnforcesRequiredPathFields(t *testing.T) {
	t.Parallel()
	op := &catalog.Op{
		OpID:           "drive.files.get",
		ParamsRequired: [][]string{{"fileId", "string"}},
		ParamsOptional: [][]string{{"fields", "string"}},
		RequestFields: []catalog.RequestField{
			{Name: "fileId", Location: catalog.RequestFieldPath, Required: true, Type: "string"},
			{Name: "revisionId", Location: catalog.RequestFieldPath, Required: true, Type: "string"},
			{Name: "optionalPath", Location: catalog.RequestFieldPath, Type: "string"},
			{Name: "pageSize", Location: catalog.RequestFieldQuery, Required: true, Type: "integer"},
		},
	}

	missing, unknown, typeErrors := validateParams(op, map[string]any{})
	if len(unknown) != 0 || len(typeErrors) != 0 {
		t.Fatalf("unknown=%v typeErrors=%v; want both empty", unknown, typeErrors)
	}
	if !slices.Contains(missing, "fileId") {
		t.Errorf("missing=%v; want the hand-authored required param", missing)
	}
	if !slices.Contains(missing, "revisionId") {
		t.Errorf("missing=%v; want the required path field", missing)
	}
	if slices.Contains(missing, "optionalPath") {
		t.Errorf("missing=%v; an optional path field must not be required", missing)
	}
	if slices.Contains(missing, "pageSize") {
		t.Errorf("missing=%v; only path location is enforced", missing)
	}
	var fileIDs int
	for _, m := range missing {
		if m == "fileId" {
			fileIDs++
		}
	}
	if fileIDs != 1 {
		t.Errorf("fileId reported %d times in %v; want once", fileIDs, missing)
	}
}

// TestAnnotateResponseWithoutAVariant covers the nil-variant guard: the adapter
// lookup keys on rv.AdapterKey, so a degraded caller must get the body back
// untouched rather than a nil dereference.
func TestAnnotateResponseWithoutAVariant(t *testing.T) {
	t.Parallel()
	d := &dispatcher{}
	body := []byte(`{"a":1}`)
	if got, _ := d.annotateResponse(&Invocation{OpID: "x"}, nil, body); string(got) != string(body) {
		t.Errorf("annotateResponse(nil rv) = %q; want the body unchanged", got)
	}
}

// panickingAnnotator is an adapter whose annotation hook panics, as a
// third-party plugin adapter's can.
type panickingAnnotator struct {
	funcAdapter
}

func (p *panickingAnnotator) AnnotateResponse(_ *Invocation, _ *ResolvedVariant, _ []byte) ([]byte, []string) {
	panic("annotator exploded")
}

// TestAnnotateSafelyRecordsAPanicInTheAuditSink covers the audit append inside
// the recover. Spec §3.1 step 7 forbids an adapter panic from ending a
// long-running mcp --stdio session, and the audit entry is the only record that
// the annotation was dropped.
func TestAnnotateSafelyRecordsAPanicInTheAuditSink(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	d := &dispatcher{auditSink: sink}
	inv := &Invocation{OpID: "gmail.messages.list", RequestID: "req-1", Args: map[string]any{"q": "x"}}
	rv := &ResolvedVariant{AdapterKey: "boom", Variant: &catalog.Variant{VariantID: "v1"}}

	out, paths := d.annotateSafely(&panickingAnnotator{}, inv, rv, []byte(`{"a":1}`))
	if out != nil {
		t.Errorf("annotateSafely returned %q after a panic; want nil", out)
	}
	if paths != nil {
		t.Errorf("annotateSafely returned paths %v after a panic; a dropped annotation must not be named in the notice", paths)
	}
	if len(sink.entries) != 1 {
		t.Fatalf("audit sink got %d entries; want 1", len(sink.entries))
	}
	if sink.entries[0]["op_id"] != "gmail.messages.list" {
		t.Errorf("audit entry op_id = %v; want the invocation op", sink.entries[0]["op_id"])
	}
}

// recordingSink is a local audit sink so this file does not depend on the
// external-test-package recorder.
type recordingSink struct {
	entries []map[string]any
}

func (s *recordingSink) Append(e map[string]any) { s.entries = append(s.entries, e) }

// TestPolicyVariantPinArms covers the explicit variant_id pin. A pin the risk
// gate cannot evaluate must return nil so resolveVariant raises the accurate
// error (VARIANT_QUARANTINED or VARIANT_NOT_FOUND) before anything executes,
// rather than gating on a different variant than the one that would run.
func TestPolicyVariantPinArms(t *testing.T) {
	t.Parallel()
	op := twoStableVariantOp([2]catalog.InterfaceKind{
		catalog.InterfaceKindDiscoveryREST,
		catalog.InterfaceKindPluginMCP,
	})
	op.Variants[1].Quarantined = true
	d := &dispatcher{snapshot: &catalog.Catalog{Ops: []catalog.Op{op}}}

	got := d.policyVariant(&Invocation{OpID: "x.tied", RequestedVariantID: "a"})
	if got == nil || got.VariantID != "a" {
		t.Errorf("pinned healthy variant = %v; want a", got)
	}
	if got := d.policyVariant(&Invocation{OpID: "x.tied", RequestedVariantID: "b"}); got != nil {
		t.Errorf("pinned quarantined variant = %q; want nil", got.VariantID)
	}
	if got := d.policyVariant(&Invocation{OpID: "x.tied", RequestedVariantID: "nope"}); got != nil {
		t.Errorf("pinned unknown variant = %q; want nil", got.VariantID)
	}
	if got := d.policyVariant(&Invocation{OpID: "absent"}); got != nil {
		t.Errorf("policyVariant for an unknown op = %q; want nil", got.VariantID)
	}
}
