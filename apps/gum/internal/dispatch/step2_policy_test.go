// Package dispatch — Red Team failing tests for step 2: policy kernel enforcement (issue gum-vq4z.2).
//
// Spec anchors:
//   - §3.1 step 4: Risk gate — resolved variant's risk_class vs. calling path flags.
//     Mismatch returns RISK_TOOL_MISMATCH. (spec line 232)
//   - §4.1 risk gate four-step table: resolve variant → read risk_class → compare to
//     calling tool → authority. (spec lines 299–303)
//   - §4.1 RISK_TOOL_MISMATCH envelope: error_code, op_id, variant_id, variant_risk_class,
//     required_tool. Fires in BOTH directions. (spec line 330)
//   - §3.1 step 5: Auth/scope check — missing authority → AUTH_REQUIRED or SCOPE_MISSING.
//     (spec line 233)
//   - §4.1 confirmation gate: destructive op without confirmed:true returns
//     REQUIRES_CONFIRMATION. (spec line 332)
//   - §4.1 REQUIRES_CONFIRMATION: write op with confirmation_policy="high_stakes_write"
//     also triggers confirmation gate. (spec line 294)
//   - §1421 stable runtime error codes: RISK_TOOL_MISMATCH, REQUIRES_CONFIRMATION,
//     SCOPE_MISSING listed as stable. (spec line 1421)
//
// Required NEW API surface (Green Team must add to evaluatePolicy):
//
//  1. evaluatePolicy must return *StructuredError (not plain error) so the caller
//     can inspect ErrCode and Detail fields. Signature change:
//
//     func (d *dispatcher) evaluatePolicy(ctx context.Context, inv *Invocation) *StructuredError
//
//  2. ProfilePolicy struct (or inline fields on DispatcherConfig / a new PolicyConfig):
//
//     type ProfilePolicy struct {
//     // AllowOps, when non-empty, is an explicit allowlist of op_ids.
//     // An op not in this list is rejected with POLICY_DENIED.
//     AllowOps []string
//     // DenyOps is an explicit denylist. An op in this list is always
//     // rejected with POLICY_DENIED, even if it also appears in AllowOps.
//     DenyOps []string
//     // AllowedScopes is the set of OAuth scopes the profile has granted.
//     // Used in the auth scope policy check.
//     AllowedScopes []string
//     }
//
//  3. ErrCodePolicyDenied stable error code:
//
//     ErrCodePolicyDenied ErrorCode = "POLICY_DENIED"
//
//     The spec §1421 list does not include POLICY_DENIED; however the issue body
//     requires it as the discriminator for allowlist/denylist gate failures.
//     Green Team must add it to errors.go alongside the existing codes.
//
//  4. The risk-class gate must return *StructuredError with ErrCodeRiskToolMismatch
//     (not a plain fmt.Errorf string) carrying detail keys: op_id, variant_id,
//     variant_risk_class, required_tool. (spec line 330)
//
//  5. The confirmation gate for destructive ops must return *StructuredError with
//     ErrCodeRequiresConfirmation (not plain error). (spec line 332)
//
//  6. The scope check must return *StructuredError with ErrCodeScopeMissing when
//     the variant's required scope is absent from the profile's AllowedScopes.
//     (spec line 233)
//
// All tests in this file FAIL to compile until Green Team implements the API surface
// described above. That is the intended state for Red Team output.
package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// ── test catalog helpers ──────────────────────────────────────────────────────

// policyTestCatalog builds a minimal *catalog.Catalog with ops for every
// risk class, including a destructive op that carries a required OAuth scope.
func policyTestCatalog() *catalog.Catalog {
	makeVariant := func(id string, rc catalog.RiskClass) catalog.Variant {
		return catalog.Variant{
			VariantID:     id,
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindSDKNative,
			BackendKind:   catalog.BackendKindTypedRestSDK,
			RiskClass:     rc,
			Binding: &catalog.Binding{
				BindingSchemaVersion: 1,
				AdapterKey:           "test.adapter",
				OperationKey:         id + ".exec",
			},
		}
	}

	makeVariantWithScope := func(id string, rc catalog.RiskClass, scope string) catalog.Variant {
		v := makeVariant(id, rc)
		v.Scopes = []string{scope}
		return v
	}

	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test@0.0.0",
		Ops: []catalog.Op{
			{
				OpID:             "test.read.op",
				OpSchemaVersion:  1,
				Title:            "Test read op",
				Summary:          "Used by Red Team step-2 policy tests.",
				DefaultVariantID: "test.read.op.v1",
				Variants:         []catalog.Variant{makeVariant("test.read.op.v1", catalog.RiskClassRead)},
			},
			{
				OpID:             "test.write.op",
				OpSchemaVersion:  1,
				Title:            "Test write op",
				Summary:          "Used by Red Team step-2 policy tests.",
				DefaultVariantID: "test.write.op.v1",
				Variants:         []catalog.Variant{makeVariant("test.write.op.v1", catalog.RiskClassWrite)},
			},
			{
				OpID:             "test.highstakes.write",
				OpSchemaVersion:  1,
				Title:            "Test high-stakes write op",
				Summary:          "Used by policy confirmation tests.",
				DefaultVariantID: "test.highstakes.write.v1",
				Variants: []catalog.Variant{
					func() catalog.Variant {
						v := makeVariant("test.highstakes.write.v1", catalog.RiskClassWrite)
						v.ConfirmationPolicy = "high_stakes_write"
						return v
					}(),
				},
			},
			{
				OpID:             "test.destructive.op",
				OpSchemaVersion:  1,
				Title:            "Test destructive op",
				Summary:          "Used by Red Team step-2 policy tests.",
				DefaultVariantID: "test.destructive.op.v1",
				Variants:         []catalog.Variant{makeVariant("test.destructive.op.v1", catalog.RiskClassDestructive)},
			},
			{
				OpID:             "test.scoped.op",
				OpSchemaVersion:  1,
				Title:            "Test scoped write op",
				Summary:          "Used by Red Team step-2 auth scope tests.",
				DefaultVariantID: "test.scoped.op.v1",
				Variants:         []catalog.Variant{makeVariantWithScope("test.scoped.op.v1", catalog.RiskClassWrite, "https://www.googleapis.com/auth/drive")},
			},
			{
				OpID:             "gum.code",
				OpSchemaVersion:  1,
				Title:            "Run code",
				Summary:          "Used by policy confirmation tests.",
				DefaultVariantID: "gum.code.v1",
				Variants:         []catalog.Variant{makeVariant("gum.code.v1", catalog.RiskClassRead)},
			},
		},
	}
}

// newPolicyDispatcher constructs a dispatcher with a ProfilePolicy attached.
// ProfilePolicy is the NEW type that Green Team must add.
func newPolicyDispatcher(policy ProfilePolicy) *dispatcher {
	return &dispatcher{
		snapshot:      policyTestCatalog(),
		adapters:      map[string]Adapter{},
		profilePolicy: policy,
	}
}

// ── 1. Risk class hierarchy ───────────────────────────────────────────────────

// TestPolicyWriteOpRequiresAllowWrite verifies that a write-class op is rejected
// with RISK_TOOL_MISMATCH when AllowWrite=false on the Invocation.
// Spec: §3.1 step 4, §4.1 risk gate step 3 (spec line 302).
func TestPolicyWriteOpRequiresAllowWrite(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:       "test.write.op",
		Args:       map[string]any{},
		AllowWrite: false,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError for write op without allow_write, got nil")
	}
	if serr.ErrCode != ErrCodeRiskToolMismatch {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeRiskToolMismatch, serr.ErrCode)
	}
	if serr.Retryable {
		t.Error("RISK_TOOL_MISMATCH must not be retryable (spec line 330)")
	}
	// Required detail keys per spec line 330.
	if serr.Detail["op_id"] != "test.write.op" {
		t.Errorf("detail['op_id'] = %v, want 'test.write.op'", serr.Detail["op_id"])
	}
	if serr.Detail["variant_risk_class"] != string(catalog.RiskClassWrite) {
		t.Errorf("detail['variant_risk_class'] = %v, want %q", serr.Detail["variant_risk_class"], catalog.RiskClassWrite)
	}
}

// TestPolicyWriteOpAllowedWhenAllowWriteTrue verifies that a write-class op passes
// the risk gate when AllowWrite=true. Spec: §4.1 (spec line 302).
func TestPolicyWriteOpAllowedWhenAllowWriteTrue(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:       "test.write.op",
		Args:       map[string]any{},
		AllowWrite: true,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr != nil {
		t.Fatalf("expected no error for write op with allow_write=true, got: %v", serr)
	}
}

func TestPolicyHighStakesWriteRequiresConfirmationToken(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:       "test.highstakes.write",
		Args:       map[string]any{"id": "A"},
		AllowWrite: true,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected high_stakes_write to require confirmation")
	}
	if serr.ErrCode != ErrCodeRequiresConfirmation {
		t.Fatalf("ErrCode = %q; want %q", serr.ErrCode, ErrCodeRequiresConfirmation)
	}
	token, _ := serr.Detail["confirmation_token"].(string)
	if token == "" {
		t.Fatal("REQUIRES_CONFIRMATION missing confirmation_token")
	}
	if got := serr.Detail["confirmation_purpose"]; got != ConfirmationPurposeWrite {
		t.Fatalf("confirmation_purpose = %v; want %s", got, ConfirmationPurposeWrite)
	}

	inv.Confirmed = true
	inv.ConfirmationToken = token
	if serr := d.evaluatePolicy(context.Background(), inv); serr != nil {
		t.Fatalf("confirmed high_stakes_write rejected: %v", serr)
	}
}

func TestPolicyInvocationWriteConfirmationFlagRequiresToken(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:                     "test.write.op",
		Args:                     map[string]any{"id": "A"},
		AllowWrite:               true,
		RequireWriteConfirmation: true,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("RequireWriteConfirmation=true should require confirmation")
	}
	if serr.ErrCode != ErrCodeRequiresConfirmation {
		t.Fatalf("ErrCode = %q; want %q", serr.ErrCode, ErrCodeRequiresConfirmation)
	}
	token, _ := serr.Detail["confirmation_token"].(string)
	if token == "" {
		t.Fatal("missing confirmation_token")
	}

	inv.Confirmed = true
	inv.ConfirmationToken = token
	if serr := d.evaluatePolicy(context.Background(), inv); serr != nil {
		t.Fatalf("confirmed RequireWriteConfirmation rejected: %v", serr)
	}
}

func TestPolicyHighStakesWriteTokenDoesNotSatisfyDestructive(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	writeInv := &Invocation{
		OpID:       "test.highstakes.write",
		Args:       map[string]any{"id": "A"},
		AllowWrite: true,
	}
	v := d.policyVariant(writeInv)
	token, err := IssueConfirmationToken(d.confirmationParams(writeInv, v, ConfirmationPurposeWrite))
	if err != nil {
		t.Fatalf("IssueConfirmationToken(write): %v", err)
	}

	destructiveInv := &Invocation{
		OpID:              "test.destructive.op",
		Args:              map[string]any{"id": "A"},
		AllowDestructive:  true,
		Confirmed:         true,
		ConfirmationToken: token,
	}
	serr := d.evaluatePolicy(context.Background(), destructiveInv)
	if serr == nil {
		t.Fatal("write token satisfied destructive op; want rejection")
	}
	if serr.ErrCode != ErrCodeConfirmationTokenInvalid {
		t.Fatalf("ErrCode = %q; want %q", serr.ErrCode, ErrCodeConfirmationTokenInvalid)
	}
}

func TestPolicyGumCodeElevatedRequiresConfirmationBeforeExecution(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:       "gum.code",
		Args:       map[string]any{"language": "risor", "source": `gum_print("nope")`},
		AllowWrite: true,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("gum.code with allow_write must require confirmation")
	}
	if serr.ErrCode != ErrCodeRequiresConfirmation {
		t.Fatalf("ErrCode = %q; want %q", serr.ErrCode, ErrCodeRequiresConfirmation)
	}
	token, _ := serr.Detail["confirmation_token"].(string)
	if token == "" {
		t.Fatal("gum.code confirmation missing token")
	}
	if got := serr.Detail["confirmation_purpose"]; got != ConfirmationPurposeCodeWrite {
		t.Fatalf("confirmation_purpose = %v; want %s", got, ConfirmationPurposeCodeWrite)
	}

	inv.Confirmed = true
	inv.ConfirmationToken = token
	if serr := d.evaluatePolicy(context.Background(), inv); serr != nil {
		t.Fatalf("confirmed gum.code rejected: %v", serr)
	}
}

func TestPolicyGumCodeTokenBoundToSourceAndFlags(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:             "gum.code",
		Args:             map[string]any{"language": "risor", "source": `gum_print("approved")`},
		AllowDestructive: true,
	}
	token, err := IssueConfirmationToken(d.confirmationParams(inv, d.policyVariant(inv), ConfirmationPurposeCodeDestroy))
	if err != nil {
		t.Fatalf("IssueConfirmationToken(code destructive): %v", err)
	}

	inv.Confirmed = true
	inv.ConfirmationToken = token
	inv.Args["source"] = `gum_print("changed")`
	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("changed source accepted with old gum.code token")
	}
	if serr.ErrCode != ErrCodeConfirmationTokenInvalid {
		t.Fatalf("ErrCode = %q; want %q", serr.ErrCode, ErrCodeConfirmationTokenInvalid)
	}
}

// TestPolicyDestructiveOpRequiresAllowDestructive verifies that a destructive-class
// op is rejected with RISK_TOOL_MISMATCH when AllowDestructive=false and AllowWrite=false.
// Spec: §3.1 step 4. Destructive is a strictly higher tier than write:
// read < write < destructive.
func TestPolicyDestructiveOpRequiresAllowDestructive(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:             "test.destructive.op",
		Args:             map[string]any{},
		AllowWrite:       false,
		AllowDestructive: false,
		Confirmed:        false,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError for destructive op without allow_destructive, got nil")
	}
	// Must be RISK_TOOL_MISMATCH before we even get to the confirmation gate.
	if serr.ErrCode != ErrCodeRiskToolMismatch {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeRiskToolMismatch, serr.ErrCode)
	}
	if serr.Retryable {
		t.Error("RISK_TOOL_MISMATCH must not be retryable")
	}
}

// TestPolicyDestructiveOpRequiresConfirmationToken verifies that a destructive-class
// op with AllowDestructive=true but Confirmed=false returns REQUIRES_CONFIRMATION.
// Spec: §4.1 confirmation gate (spec line 332).
func TestPolicyDestructiveOpRequiresConfirmationToken(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:             "test.destructive.op",
		Args:             map[string]any{},
		AllowDestructive: true,
		Confirmed:        false,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError for destructive op without confirmation, got nil")
	}
	if serr.ErrCode != ErrCodeRequiresConfirmation {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeRequiresConfirmation, serr.ErrCode)
	}
	if serr.Retryable {
		t.Error("REQUIRES_CONFIRMATION must not be retryable (Retryable=false per issue)")
	}
	// Must expose op_id so LLM knows which op triggered it (spec line 332 envelope shape).
	if serr.Detail["op_id"] != "test.destructive.op" {
		t.Errorf("detail['op_id'] = %v, want 'test.destructive.op'", serr.Detail["op_id"])
	}
}

// TestPolicyDestructiveOpConfirmedWithoutTokenRejected verifies that confirmed=true
// without a confirmation_token is still rejected. Token is mandatory.
// Spec §4.1: "Re-invocation with confirmed: true MUST include confirmation_token" (line 332).
func TestPolicyDestructiveOpConfirmedWithoutTokenRejected(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:              "test.destructive.op",
		Args:              map[string]any{},
		AllowDestructive:  true,
		Confirmed:         true,
		ConfirmationToken: "", // missing
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError for confirmed=true with missing token, got nil")
	}
	// Must be REQUIRES_CONFIRMATION (token not yet present) or CONFIRMATION_TOKEN_INVALID.
	validCodes := map[ErrorCode]bool{
		ErrCodeRequiresConfirmation:     true,
		ErrCodeConfirmationTokenInvalid: true,
	}
	if !validCodes[serr.ErrCode] {
		t.Errorf("expected ErrCode in {REQUIRES_CONFIRMATION, CONFIRMATION_TOKEN_INVALID}, got %q", serr.ErrCode)
	}
}

// TestPolicyReadOpDoesNotRequireFlags verifies that a read-class op passes the risk
// gate with no flags set. Spec: §4.1, gum.read annotation (spec line 268).
func TestPolicyReadOpDoesNotRequireFlags(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID: "test.read.op",
		Args: map[string]any{},
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr != nil {
		t.Fatalf("read op with no flags should pass policy, got: %v", serr)
	}
}

// TestPolicyWriteAllowWriteDoesNotSatisfyDestructive verifies the hierarchy:
// AllowWrite=true is insufficient for a destructive op — AllowDestructive is required.
// Spec: §3.1 step 4 "read < write < destructive".
func TestPolicyWriteAllowWriteDoesNotSatisfyDestructive(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:             "test.destructive.op",
		Args:             map[string]any{},
		AllowWrite:       true,  // write flag only
		AllowDestructive: false, // destructive flag absent
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("allow_write alone must not satisfy a destructive op; expected error")
	}
	if serr.ErrCode != ErrCodeRiskToolMismatch {
		t.Errorf("expected RISK_TOOL_MISMATCH, got %q", serr.ErrCode)
	}
}

// ── 2. Allowlist gate ─────────────────────────────────────────────────────────

// TestPolicyAllowlistIncludesOpID verifies that an op in AllowOps passes the gate.
// Spec: §3.1 step 4 policy wrappers (spec line 234); issue body "allowlist gate".
func TestPolicyAllowlistIncludesOpID(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{
		AllowOps: []string{"test.read.op", "test.write.op"},
	})
	inv := &Invocation{OpID: "test.read.op", Args: map[string]any{}}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr != nil {
		t.Fatalf("op in allowlist should pass, got: %v", serr)
	}
}

// TestPolicyAllowlistExcludesOpID verifies that an op NOT in a non-empty AllowOps
// is rejected with POLICY_DENIED.
// Issue body: "ops not in allowlist (when allowlist non-empty) return POLICY_DENIED".
func TestPolicyAllowlistExcludesOpID(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{
		AllowOps: []string{"test.write.op"}, // read.op is NOT in the list
	})
	inv := &Invocation{OpID: "test.read.op", Args: map[string]any{}}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("op absent from non-empty allowlist should be rejected; got nil error")
	}
	if serr.ErrCode != ErrCodePolicyDenied {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodePolicyDenied, serr.ErrCode)
	}
	if serr.Retryable {
		t.Error("POLICY_DENIED must not be retryable")
	}
	// detail["reason"] should explain which gate fired.
	reason, ok := serr.Detail["reason"]
	if !ok {
		t.Errorf("expected detail['reason'], got detail=%v", serr.Detail)
	}
	if reason == "" {
		t.Error("detail['reason'] must be non-empty")
	}
}

// TestPolicyEmptyAllowlistPermitsAll verifies that an empty AllowOps slice is a
// no-op (all ops permitted subject to other gates). Issue: "when allowlist non-empty".
func TestPolicyEmptyAllowlistPermitsAll(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{
		AllowOps: []string{}, // empty: no restriction
	})
	inv := &Invocation{OpID: "test.read.op", Args: map[string]any{}}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr != nil {
		t.Fatalf("empty allowlist should permit all ops, got: %v", serr)
	}
}

// ── 3. Denylist gate ──────────────────────────────────────────────────────────

// TestPolicyDenylistBlocksOpEvenInAllowlist verifies that an op present in both
// AllowOps and DenyOps is rejected. DenyOps takes precedence.
// Issue body: "ops in denylist return POLICY_DENIED even if in allow_ops".
func TestPolicyDenylistBlocksOpEvenInAllowlist(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{
		AllowOps: []string{"test.read.op"},
		DenyOps:  []string{"test.read.op"}, // same op — deny wins
	})
	inv := &Invocation{OpID: "test.read.op", Args: map[string]any{}}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("op in denylist must be rejected even if in allowlist; got nil error")
	}
	if serr.ErrCode != ErrCodePolicyDenied {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodePolicyDenied, serr.ErrCode)
	}
	if serr.Retryable {
		t.Error("POLICY_DENIED must not be retryable")
	}
	reason, ok := serr.Detail["reason"]
	if !ok {
		t.Errorf("expected detail['reason'], got detail=%v", serr.Detail)
	}
	if reason == "" {
		t.Error("detail['reason'] must be non-empty")
	}
}

// TestPolicyDenylistBlocksOpNotInAllowlist verifies that an op in DenyOps is
// rejected even when AllowOps is empty (deny-by-default for denylisted ops).
func TestPolicyDenylistBlocksOpNotInAllowlist(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{
		DenyOps: []string{"test.read.op"},
	})
	inv := &Invocation{OpID: "test.read.op", Args: map[string]any{}}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("op in denylist must always be rejected; got nil error")
	}
	if serr.ErrCode != ErrCodePolicyDenied {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodePolicyDenied, serr.ErrCode)
	}
}

// TestPolicyDenylistDoesNotAffectUnmentionedOps verifies that an op absent from
// DenyOps (and no AllowOps restriction) passes the gate normally.
func TestPolicyDenylistDoesNotAffectUnmentionedOps(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{
		DenyOps: []string{"test.write.op"}, // only write is denied
	})
	inv := &Invocation{OpID: "test.read.op", Args: map[string]any{}}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr != nil {
		t.Fatalf("op absent from denylist should pass, got: %v", serr)
	}
}

// ── 4. Deny-by-default for destructive without confirmation token ─────────────

// TestPolicyDestructiveDenyByDefaultNoFlags verifies the spec's deny-by-default:
// a destructive op with no flags at all returns a structured error, not a nil.
// Spec: §3.1 step 4, §4.1 risk gate (spec lines 232, 296–332).
// Note: token verification is deferred to step 3 (confirmation_token binding);
// step 2 only flags the requirement.
func TestPolicyDestructiveDenyByDefaultNoFlags(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID: "test.destructive.op",
		Args: map[string]any{},
		// No AllowDestructive, no Confirmed, no ConfirmationToken.
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("destructive op with no flags must be rejected by deny-by-default; got nil error")
	}
	// Either RISK_TOOL_MISMATCH (no allow_destructive) or REQUIRES_CONFIRMATION is acceptable;
	// must be a *StructuredError, never a nil.
	var se *StructuredError
	if !errors.As(serr, &se) {
		t.Fatalf("expected *StructuredError, got %T", serr)
	}
}

// TestPolicyDestructiveRequiresConfirmationTokenFlaggedInStep2 verifies that even
// when AllowDestructive=true, if Confirmed=false the policy step flags the
// REQUIRES_CONFIRMATION gate (token verify is deferred to step 3, but the absence
// must be caught at step 2). Spec §4.1 line 332.
func TestPolicyDestructiveRequiresConfirmationTokenFlaggedInStep2(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID:             "test.destructive.op",
		Args:             map[string]any{},
		AllowDestructive: true,
		Confirmed:        false,
		// No token.
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("destructive with allow_destructive=true but confirmed=false must be rejected at step 2")
	}
	if serr.ErrCode != ErrCodeRequiresConfirmation {
		t.Errorf("expected %q, got %q", ErrCodeRequiresConfirmation, serr.ErrCode)
	}
}

// ── 5. Auth scope policy ──────────────────────────────────────────────────────

// TestPolicyAuthScopeRequiredScopePresent verifies that a write op whose variant
// declares RequiredScopes passes when the profile AllowedScopes includes that scope.
// Spec: §3.1 step 5 (spec line 233).
func TestPolicyAuthScopeRequiredScopePresent(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{
		AllowedScopes: []string{"https://www.googleapis.com/auth/drive"},
	})
	inv := &Invocation{
		OpID:       "test.scoped.op",
		Args:       map[string]any{},
		AllowWrite: true,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr != nil {
		t.Fatalf("op with required scope present in profile should pass, got: %v", serr)
	}
}

// TestPolicyAuthScopeRequiredScopeMissing verifies that a write op whose variant
// declares RequiredScopes fails with SCOPE_MISSING when the profile AllowedScopes
// does NOT include the required scope.
// Spec: §3.1 step 5 (spec line 233), §1421 SCOPE_MISSING.
func TestPolicyAuthScopeRequiredScopeMissing(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{
		AllowedScopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}, // wrong scope
	})
	inv := &Invocation{
		OpID:       "test.scoped.op",
		Args:       map[string]any{},
		AllowWrite: true,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("op with required scope absent from profile should be rejected; got nil error")
	}
	if serr.ErrCode != ErrCodeScopeMissing {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeScopeMissing, serr.ErrCode)
	}
	if serr.Retryable {
		t.Error("SCOPE_MISSING must not be retryable (spec: policy denials are non-retryable)")
	}
	// detail["required_scope"] must name the missing scope so the caller can
	// present it to the user (spec §3.1 step 5 — "missing authority returns SCOPE_MISSING").
	requiredScope, ok := serr.Detail["required_scope"]
	if !ok {
		t.Errorf("expected detail['required_scope'], got detail=%v", serr.Detail)
	}
	if requiredScope != "https://www.googleapis.com/auth/drive" {
		t.Errorf("detail['required_scope'] = %v, want %q", requiredScope, "https://www.googleapis.com/auth/drive")
	}
}

// TestPolicyAuthScopeEmptyProfileScopesRejected verifies that a variant with a
// required scope fails when the profile has no scopes at all.
func TestPolicyAuthScopeEmptyProfileScopesRejected(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{
		AllowedScopes: nil, // no scopes granted
	})
	inv := &Invocation{
		OpID:       "test.scoped.op",
		Args:       map[string]any{},
		AllowWrite: true,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("variant with required scope, profile with no scopes — expected rejection")
	}
	if serr.ErrCode != ErrCodeScopeMissing {
		t.Errorf("expected SCOPE_MISSING, got %q", serr.ErrCode)
	}
}

// TestPolicyAuthScopeOpWithNoRequiredScopePasses verifies that a variant with no
// RequiredScopes passes regardless of profile AllowedScopes.
func TestPolicyAuthScopeOpWithNoRequiredScopePasses(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{
		AllowedScopes: nil,
	})
	inv := &Invocation{
		OpID:       "test.read.op",
		Args:       map[string]any{},
		AllowWrite: false,
	}

	serr := d.evaluatePolicy(context.Background(), inv)
	if serr != nil {
		t.Fatalf("op with no required scopes should pass auth scope gate, got: %v", serr)
	}
}

// ── 6. Structured error shape assertions ─────────────────────────────────────

// TestPolicyDeniedErrorShapeRetryableFalse verifies that every policy denial
// from the allowlist/denylist gate is a *StructuredError with Retryable=false
// and a non-empty detail["reason"]. Issue body: "Retryable=false".
func TestPolicyDeniedErrorShapeRetryableFalse(t *testing.T) {
	cases := []struct {
		name string
		inv  *Invocation
		pol  ProfilePolicy
	}{
		{
			name: "denylist",
			inv:  &Invocation{OpID: "test.read.op", Args: map[string]any{}},
			pol:  ProfilePolicy{DenyOps: []string{"test.read.op"}},
		},
		{
			name: "allowlist exclusion",
			inv:  &Invocation{OpID: "test.read.op", Args: map[string]any{}},
			pol:  ProfilePolicy{AllowOps: []string{"test.write.op"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newPolicyDispatcher(tc.pol)
			serr := d.evaluatePolicy(context.Background(), tc.inv)
			if serr == nil {
				t.Fatalf("[%s] expected error, got nil", tc.name)
			}
			if serr.ErrCode != ErrCodePolicyDenied {
				t.Errorf("[%s] ErrCode=%q, want %q", tc.name, serr.ErrCode, ErrCodePolicyDenied)
			}
			if serr.Retryable {
				t.Errorf("[%s] Retryable must be false for POLICY_DENIED", tc.name)
			}
			reason, hasReason := serr.Detail["reason"]
			if !hasReason || reason == "" {
				t.Errorf("[%s] detail['reason'] must be non-empty, got detail=%v", tc.name, serr.Detail)
			}
		})
	}
}

// TestPolicyEvaluatePolicyReturnTypeIsStructuredError verifies at the type level
// that evaluatePolicy returns *StructuredError rather than plain error.
// This test fails to compile until Green Team changes the return type.
//
// Compile-time assertion: the return value must be assignable to *StructuredError
// without a type assertion.
func TestPolicyEvaluatePolicyReturnTypeIsStructuredError(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{})
	inv := &Invocation{
		OpID: "test.write.op",
		Args: map[string]any{},
		// AllowWrite intentionally absent → should return *StructuredError
	}
	// evaluatePolicy(ctx, inv) must return *StructuredError, not error.
	// If Green Team changes signature to (ctx, inv) *StructuredError, this compiles.
	// If it still returns error, the assignment below fails to compile.
	serr := d.evaluatePolicy(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError for write op without allow_write")
	}
}

// TestPolicyDispatcherHasProfilePolicyField verifies (at compile time) that
// the dispatcher struct carries a profilePolicy field of type ProfilePolicy.
// Green Team must add:
//
//	type ProfilePolicy struct { AllowOps, DenyOps, AllowedScopes []string }
//	type dispatcher struct { ...; profilePolicy ProfilePolicy }
func TestPolicyDispatcherHasProfilePolicyField(t *testing.T) {
	d := &dispatcher{
		snapshot:      policyTestCatalog(),
		adapters:      map[string]Adapter{},
		profilePolicy: ProfilePolicy{AllowOps: []string{"test.read.op"}},
	}
	if len(d.profilePolicy.AllowOps) != 1 {
		t.Errorf("profilePolicy.AllowOps len=%d, want 1", len(d.profilePolicy.AllowOps))
	}
}
