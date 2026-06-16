package dispatch

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestEvaluatePolicyNoSnapshotShortCircuits pins evaluatePolicy's
// `d.snapshot == nil → return nil` arm (policy.go:71-73). The legacy
// gum.code path constructs a dispatcher without a catalog snapshot;
// the policy kernel MUST bypass all risk gates in that mode rather
// than panic on the nil snapshot — otherwise the gum.code adapter
// (used by the eval-cli and replay paths) cannot dispatch at all.
func TestEvaluatePolicyNoSnapshotShortCircuits(t *testing.T) {
	d := &dispatcher{
		snapshot:      nil,
		adapters:      map[string]Adapter{},
		profilePolicy: ProfilePolicy{DenyOps: []string{"some.op"}},
	}
	inv := &Invocation{OpID: "some.op", Args: map[string]any{}}

	if serr := d.evaluatePolicy(context.Background(), inv); serr != nil {
		t.Errorf("evaluatePolicy(no snapshot)=%v; want nil (bypass)", serr)
	}
}

// TestEvaluatePolicyOpNotInCatalogShortCircuits pins evaluatePolicy's
// `v == nil → return nil` arm (policy.go:76-79). When the op_id is
// not in the catalog, the kernel defers to resolveVariant's
// OP_NOT_FOUND envelope rather than emitting a confusing
// POLICY_DENIED — operators reading the error need to know the op
// doesn't exist, not that policy blocked an op that doesn't exist.
func TestEvaluatePolicyOpNotInCatalogShortCircuits(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{OpID: "no.such.op", Args: map[string]any{}}

	if serr := d.evaluatePolicy(context.Background(), inv); serr != nil {
		t.Errorf("evaluatePolicy(unknown op)=%v; want nil (deferred to resolveVariant)", serr)
	}
}

// TestEvaluatePolicyReadOpRejectsAllowDestructive pins evaluatePolicy's
// `RiskClassRead + AllowDestructive/AllowWrite → RISK_TOOL_MISMATCH`
// arm (policy.go:174-183). Spec §4.1 line 304: the risk gate is
// bidirectional — a read-class variant invoked via gum.write or
// gum.destructive is ALSO RISK_TOOL_MISMATCH. The CLI surface routes
// --risk=destructive→AllowDestructive, so either of those flags on
// a read variant signals caller-tool-too-permissive.
func TestEvaluatePolicyReadOpRejectsAllowDestructive(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:             "test.read.op",
		Args:             map[string]any{},
		AllowDestructive: true,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("read op with AllowDestructive=true must be RISK_TOOL_MISMATCH; got nil")
	}
	if serr.ErrCode != ErrCodeRiskToolMismatch {
		t.Errorf("ErrCode=%q; want %q", serr.ErrCode, ErrCodeRiskToolMismatch)
	}
	if got := serr.Detail["required_tool"]; got != "gum.read" {
		t.Errorf("Detail[required_tool]=%v; want gum.read", got)
	}
	if got := serr.Detail["op_id"]; got != "test.read.op" {
		t.Errorf("Detail[op_id]=%v; want test.read.op", got)
	}
}

// TestEvaluatePolicyReadOpRejectsAllowWrite pins the AllowWrite half
// of the bidirectional read-class gate. Same arm at policy.go:174 —
// the condition is `AllowDestructive || AllowWrite`, so AllowWrite
// alone is also sufficient to fire RISK_TOOL_MISMATCH on a read op.
func TestEvaluatePolicyReadOpRejectsAllowWrite(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:       "test.read.op",
		Args:       map[string]any{},
		AllowWrite: true,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("read op with AllowWrite=true must be RISK_TOOL_MISMATCH; got nil")
	}
	if serr.ErrCode != ErrCodeRiskToolMismatch {
		t.Errorf("ErrCode=%q; want %q", serr.ErrCode, ErrCodeRiskToolMismatch)
	}
}

// TestEvaluatePolicyGatesFallbackVariantWithoutDefault is the audit regression
// for the fail-OPEN gap: an op with NO default_variant_id and a single WRITE
// variant. resolveVariant falls back to that variant via the stability pick and
// would execute it — so the risk gate MUST evaluate the SAME variant. Before the
// fix, policyVariant (via findOpVariant, which only matches default_variant_id)
// returned nil, evaluatePolicy skipped the gate, and the write executed
// ungated. This is unreachable for the embedded catalog (Validate rejects an
// empty default_variant_id) but defends a hand-built/un-validated catalog.
func TestEvaluatePolicyGatesFallbackVariantWithoutDefault(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
		Ops: []catalog.Op{{
			OpID:             "test.nodefault.write",
			OpSchemaVersion:  1,
			Title:            "No-default write op",
			Summary:          "write op without a default variant",
			DefaultVariantID: "", // the fail-open trigger
			Variants: []catalog.Variant{{
				VariantID:     "test.nodefault.write.v1",
				Stability:     catalog.StabilityStable,
				InterfaceKind: catalog.InterfaceKindDiscoveryREST,
				BackendKind:   catalog.BackendKindTypedRestSDK,
				RiskClass:     catalog.RiskClassWrite,
			}},
		}},
	}
	d := &dispatcher{snapshot: cat, adapters: map[string]Adapter{}, profilePolicy: ProfilePolicy{}}
	inv := &Invocation{OpID: "test.nodefault.write", Args: map[string]any{}, AllowWrite: false}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("write fallback variant on a no-default op must be gated (RISK_TOOL_MISMATCH); got nil — fail-open")
	}
	if serr.ErrCode != ErrCodeRiskToolMismatch {
		t.Errorf("ErrCode=%q; want %q", serr.ErrCode, ErrCodeRiskToolMismatch)
	}
}

func TestEvaluatePolicyGumCodeCapabilityFlagsRequireConfirmation(t *testing.T) {
	cat := policyTestCatalog()
	cat.Ops = append(cat.Ops, catalog.Op{
		OpID:             "gum.code",
		OpSchemaVersion:  1,
		Title:            "Run code",
		Summary:          "Executes a sandboxed Risor script.",
		DefaultVariantID: "gum.code.v1.risor",
		Variants: []catalog.Variant{
			{
				VariantID:     "gum.code.v1.risor",
				Stability:     catalog.StabilityStable,
				InterfaceKind: catalog.InterfaceKindSDKNative,
				BackendKind:   catalog.BackendKindTypedRestSDK,
				RiskClass:     catalog.RiskClassRead,
				AuthStrategy:  catalog.AuthStrategyNone,
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "code.risor",
					OperationKey:         "gum.code.risor.exec",
				},
			},
		},
	})
	d := &dispatcher{
		snapshot:      cat,
		adapters:      map[string]Adapter{},
		profilePolicy: ProfilePolicy{},
	}

	serr := d.evaluatePolicy(context.Background(), &Invocation{
		OpID:             "gum.code",
		Args:             map[string]any{},
		AllowWrite:       true,
		AllowDestructive: true,
	})
	if serr == nil {
		t.Fatal("gum.code allow_* flags must require confirmation before execution")
	}
	if serr.ErrCode != ErrCodeRequiresConfirmation {
		t.Fatalf("ErrCode=%q; want %q (not RISK_TOOL_MISMATCH)", serr.ErrCode, ErrCodeRequiresConfirmation)
	}
	if serr.Detail["confirmation_token"] == "" {
		t.Fatal("REQUIRES_CONFIRMATION missing confirmation_token")
	}
}
