// Package dispatch — step 2 policy kernel: allowlist/denylist gates, risk class
// hierarchy, confirmation gate, and scope check (spec.md §3.1 step 2, §4.1).
//
// Gate ordering (normative per spec §3.1 and issue gum-vq4z.2):
//
//	deny → allow → risk class → confirmation → scope
package dispatch

import (
	"context"
	"fmt"

	"github.com/ehmo/gum/internal/catalog"
)

// ProfilePolicy carries per-profile allowlist/denylist/scope gates for evaluatePolicy.
// Fields are applied in the gate ordering defined in spec §3.1 step 4–5:
//
//	deny → allow → risk → confirmation → scope
type ProfilePolicy struct {
	// AllowOps, when non-empty, restricts dispatch to only the listed op_ids.
	// Empty slice means "no restriction" (all ops permitted by other gates).
	AllowOps []string
	// DenyOps is a blocklist. Takes precedence over AllowOps.
	DenyOps []string
	// AllowedScopes is the set of OAuth scopes granted to this profile.
	// Used to enforce variant.Scopes requirements at policy time (step 2).
	AllowedScopes []string

	// UnknownReadParamsAllowlist is the spec §5.7 read-only allowlist escape
	// hatch for long-tail raw-http / discovery-rest variants. Keyed by op_id;
	// each value lists arg names that may bypass the unknown-key gate with a
	// `_validation_warnings` envelope entry instead of an INVALID_ARGS error.
	// Applies ONLY when (a) variant.RiskClass == read, (b) variant.BackendKind
	// is raw-http or discovery-rest, and (c) StrictValidation is false. Nil
	// map disables the escape hatch entirely.
	UnknownReadParamsAllowlist map[string][]string

	// StrictValidation, when true, disables the UnknownReadParamsAllowlist
	// escape hatch — every unknown key on every op_id is rejected with
	// INVALID_ARGS regardless of risk class or backend. Equivalent to
	// `gum config validation.strict=true` for the active profile.
	StrictValidation bool
}

// containsOp reports whether opID is present in the ops slice.
// Linear scan is fine: allow/deny lists are expected to be small (< 100 entries).
func containsOp(ops []string, opID string) bool {
	for _, op := range ops {
		if op == opID {
			return true
		}
	}
	return false
}

// Step 2 — evaluate policy (allowlist/denylist, risk gate, confirmation, scope check).
//
// Returns a *StructuredError when the request must be rejected, nil on success.
//
// Gate ordering (normative per spec §3.1 and issue gum-vq4z.2):
//  1. Denylist  — DenyOps check; deny always wins, even over AllowOps.
//  2. Allowlist — AllowOps check; only enforced when len(AllowOps) > 0.
//  3. Risk class — compare inv.AllowWrite/AllowDestructive to variant.RiskClass
//     (read < write < destructive). Mismatch → RISK_TOOL_MISMATCH.
//  4. Confirmation — destructive + AllowDestructive=true but not confirmed+token →
//     REQUIRES_CONFIRMATION.
//  5. Scope — variant.Scopes vs ProfilePolicy.AllowedScopes → SCOPE_MISSING.
func (d *dispatcher) evaluatePolicy(ctx context.Context, inv *Invocation) *StructuredError {
	// When no catalog snapshot exists (legacy gum.code path), skip risk evaluation.
	if d.snapshot == nil {
		return nil
	}

	// Gate on the variant resolveVariant will actually execute (honoring an
	// explicit variant_id pin), not just the default — otherwise a caller could
	// pin a higher-risk variant past a gate evaluated on a lower-risk default.
	v := d.policyVariant(inv)
	if v == nil {
		// Op not in catalog, or a pinned variant that resolveVariant will reject
		// with VARIANT_NOT_FOUND. Either way, defer the error to resolveVariant.
		return nil
	}

	pol := d.profilePolicy

	// Gate 1: Denylist — deny always wins, even over allowlist.
	if containsOp(pol.DenyOps, inv.OpID) {
		return NewStructuredError(ErrCodePolicyDenied,
			fmt.Sprintf("op %s is in the profile deny_ops list", inv.OpID)).
			WithDetail("reason", "op_id in deny_ops").
			WithRetryable(false)
	}

	// Gate 2: Allowlist — only enforced when non-empty.
	if len(pol.AllowOps) > 0 && !containsOp(pol.AllowOps, inv.OpID) {
		return NewStructuredError(ErrCodePolicyDenied,
			fmt.Sprintf("op %s is not in the profile allow_ops list", inv.OpID)).
			WithDetail("reason", "op_id not in allow_ops").
			WithRetryable(false)
	}

	// Gate 3: Risk class hierarchy (read < write < destructive).
	switch v.RiskClass {
	case catalog.RiskClassDestructive:
		// When the caller provides a confirmation token and Confirmed=true, the
		// AllowDestructive flag is treated as implicitly granted (the caller already
		// went through the confirmation flow). Otherwise AllowDestructive=false means
		// the gum.destructive tool was not used → RISK_TOOL_MISMATCH.
		implicitlyAllowed := inv.Confirmed && inv.ConfirmationToken != ""
		if !inv.AllowDestructive && !implicitlyAllowed {
			return NewStructuredError(ErrCodeRiskToolMismatch,
				fmt.Sprintf("destructive op %s requires allow_destructive=true (gum.destructive tool); re-invoke with confirmed=true and a valid CONFIRMATION_REQUIRED token", inv.OpID)).
				WithDetail("op_id", inv.OpID).
				WithDetail("variant_id", v.VariantID).
				WithDetail("variant_risk_class", string(v.RiskClass)).
				WithDetail("required_tool", "gum.destructive").
				WithRetryable(false)
		}

		// Gate 4: Confirmation — destructive with AllowDestructive=true requires
		// Confirmed=true and a server-signed confirmation_token bound to the
		// invocation (spec §6.1.2 / gum-3j3g).
		//
		// First contact (Confirmed=false, no token): issue a fresh token bound
		// to (op_id, variant_id, purpose) and surface it in the
		// REQUIRES_CONFIRMATION envelope so the caller can echo it back.
		if se := d.evaluateConfirmation(inv, v, ConfirmationPurposeDestructive, string(catalog.RiskClassDestructive)); se != nil {
			return se
		}
		// Valid token + confirmed=true: allow through.

	case catalog.RiskClassWrite:
		if !inv.AllowWrite {
			return NewStructuredError(ErrCodeRiskToolMismatch,
				fmt.Sprintf("write op %s requires allow_write=true (gum.write tool)", inv.OpID)).
				WithDetail("op_id", inv.OpID).
				WithDetail("variant_id", v.VariantID).
				WithDetail("variant_risk_class", string(v.RiskClass)).
				WithDetail("required_tool", "gum.write").
				WithRetryable(false)
		}
		if v.ConfirmationPolicy == "high_stakes_write" || inv.RequireWriteConfirmation {
			label := v.ConfirmationPolicy
			if label == "" {
				label = "high_stakes_write"
			}
			if se := d.evaluateConfirmation(inv, v, ConfirmationPurposeWrite, label); se != nil {
				return se
			}
		}

	case catalog.RiskClassRead:
		// Spec §4.1 bidirectional rule: a read-class variant invoked via the
		// gum.write or gum.destructive tool is also RISK_TOOL_MISMATCH. The
		// CLI surface routes --risk=write→AllowWrite and --risk=destructive
		// →AllowDestructive, so either of those flags being set on a read
		// variant signals "caller-tool too permissive."
		// gum.code is the one read-class exception: its allow_* flags authorize
		// sandbox capabilities for inner gum_call/gum_parallel calls, not a
		// higher-risk invocation of the gum.code meta-op itself.
		if inv.OpID != "gum.code" && (inv.AllowDestructive || inv.AllowWrite) {
			required := "gum.read"
			return NewStructuredError(ErrCodeRiskToolMismatch,
				fmt.Sprintf("read op %s cannot be invoked via gum.write or gum.destructive; use %s", inv.OpID, required)).
				WithDetail("op_id", inv.OpID).
				WithDetail("variant_id", v.VariantID).
				WithDetail("variant_risk_class", string(v.RiskClass)).
				WithDetail("required_tool", required).
				WithRetryable(false)
		}
		if inv.OpID == "gum.code" && (inv.AllowDestructive || inv.AllowWrite) {
			purpose := ConfirmationPurposeCodeWrite
			if inv.AllowDestructive {
				purpose = ConfirmationPurposeCodeDestroy
			}
			if se := d.evaluateConfirmation(inv, v, purpose, "gum.code"); se != nil {
				return se
			}
		}

	default:
		// Fail CLOSED on an unknown/empty risk_class. The only valid values are
		// read|write|destructive; anything else is a malformed catalog entry, and
		// without this arm the switch would fall through and execute the op with
		// NO risk gate at all (a write/destructive op running without the
		// gum.write/gum.destructive tool). Reject rather than silently allow.
		return NewStructuredError(ErrCodeRiskToolMismatch,
			fmt.Sprintf("op %s has an unrecognized risk_class %q; refusing to dispatch", inv.OpID, string(v.RiskClass))).
			WithDetail("op_id", inv.OpID).
			WithDetail("variant_id", v.VariantID).
			WithDetail("variant_risk_class", string(v.RiskClass)).
			WithRetryable(false)
	}

	// Gate 5: Scope check — variant.Scopes vs ProfilePolicy.AllowedScopes.
	// Fires whenever the variant declares required scopes. A nil or empty
	// AllowedScopes means "no scopes granted" — all scoped ops are rejected.
	if len(v.Scopes) > 0 {
		grantedSet := make(map[string]struct{}, len(pol.AllowedScopes))
		for _, s := range pol.AllowedScopes {
			grantedSet[s] = struct{}{}
		}
		for _, required := range v.Scopes {
			if _, ok := grantedSet[required]; !ok {
				return NewStructuredError(ErrCodeScopeMissing,
					fmt.Sprintf("op %s requires OAuth scope %s which is not granted in this profile", inv.OpID, required)).
					WithDetail("required_scope", required).
					WithRetryable(false)
			}
		}
	}

	return nil
}

func (d *dispatcher) evaluateConfirmation(inv *Invocation, v *catalog.Variant, purpose, riskLabel string) *StructuredError {
	params := d.confirmationParams(inv, v, purpose)
	if !inv.Confirmed {
		tok, terr := IssueConfirmationToken(params)
		se := NewStructuredError(ErrCodeRequiresConfirmation,
			fmt.Sprintf("op %s requires confirmed=true with a valid confirmation_token", inv.OpID)).
			WithDetail("op_id", inv.OpID).
			WithDetail("risk_class", riskLabel).
			WithDetail("confirmation_purpose", purpose).
			WithRetryable(false)
		if terr == nil {
			se = se.WithDetail("confirmation_token", tok)
		}
		return se
	}
	if verr := VerifyConfirmationToken(inv.ConfirmationToken, params); verr != nil {
		if se, ok := verr.(*StructuredError); ok {
			return se.WithDetail("op_id", inv.OpID).WithRetryable(false)
		}
		return NewStructuredError(ErrCodeConfirmationTokenInvalid,
			"confirmation token invalid").
			WithDetail("op_id", inv.OpID).
			WithDetail("reason", "mismatch").
			WithRetryable(false)
	}
	return nil
}

func (d *dispatcher) confirmationParams(inv *Invocation, v *catalog.Variant, purpose string) ConfirmationParams {
	return ConfirmationParams{
		OpID:                 inv.OpID,
		VariantID:            v.VariantID,
		ArgsHash:             argsHashHex(d.canonicalArgs(inv.Args)),
		AuthFingerprint:      inv.AuthSubjectFingerprint,
		ProfileName:          d.profileName,
		Scope:                destructiveScopeCanonical(inv.Args),
		Purpose:              purpose,
		TTL:                  DefaultTTLForPurpose(purpose),
		ReplayStoreDir:       d.confirmationReplayDir,
		RequireDurableReplay: d.profileName != "",
	}
}

func destructiveScopeCanonical(args map[string]any) string {
	if args == nil {
		return "[]"
	}
	scope, ok := args["destructive_scope"]
	if !ok || scope == nil {
		return "[]"
	}
	return canonicalizeArgs(map[string]any{"destructive_scope": scope})
}
