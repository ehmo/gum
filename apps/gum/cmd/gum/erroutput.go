package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ehmo/gum/internal/dispatch"
)

// errRendered marks a StructuredError that printDispatchError has already
// written to the error stream as a full JSON envelope. The top-level handler
// in main.go exits non-zero without printing a duplicate terse "Error:" line.
// It unwraps to the underlying error so errors.As keeps resolving the
// *dispatch.StructuredError.
type errRendered struct{ err error }

func (e errRendered) Error() string { return e.err.Error() }
func (e errRendered) Unwrap() error { return e.err }

// renderStructuredEnvelope writes a JSON error envelope to w. It flattens
// StructuredError.Detail at the top level (spec §1421) and additionally adds:
//
//   - "how_to_fix": one-line, actionable remediation derived from the error
//     code and any free-text reason carried in Detail. Empty when the code
//     has no canned hint and the upstream did not carry a reason.
//   - "machine_envelope": the original error_code+message+detail envelope
//     preserved verbatim, so automation that already targets the §1421 shape
//     keeps parsing without churn after we add the human-facing field.
//
// Tracks gum-fkme: gives the LLM and human user a single read-and-act surface
// without breaking the machine schema.
func renderStructuredEnvelope(w io.Writer, se *dispatch.StructuredError, extras map[string]any) error {
	flat := map[string]any{
		"error_code": string(se.ErrCode),
		"message":    se.Message,
	}
	for k, v := range se.Detail {
		flat[k] = v
	}
	for k, v := range extras {
		flat[k] = v
	}

	machine := map[string]any{
		"error_code": string(se.ErrCode),
		"message":    se.Message,
	}
	for k, v := range se.Detail {
		machine[k] = v
	}

	hint := howToFix(se, extras)
	out := map[string]any{}
	for k, v := range flat {
		out[k] = v
	}
	if hint != "" {
		out["how_to_fix"] = hint
	}
	out["machine_envelope"] = machine

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w)
	return nil
}

// howToFix maps a StructuredError to a one-line remediation. We prefer a
// hand-tuned hint per ErrorCode and fall back to detail.reason / detail.hint
// when the upstream surfaced something more specific.
func howToFix(se *dispatch.StructuredError, extras map[string]any) string {
	if reason := freeText(se.Detail, "hint"); reason != "" {
		return reason
	}
	if reason := freeText(extras, "hint"); reason != "" {
		return reason
	}
	switch se.ErrCode {
	case dispatch.ErrCodeRiskToolMismatch:
		want, _ := extras["required_risk_flag"].(string)
		if want == "" {
			want = "--risk=<read|write|destructive>"
		}
		return "Re-invoke with " + want + " — the resolved variant's risk_class does not match the requested risk path."
	case dispatch.ErrCodeRequiresConfirmation:
		// Interpolate the freshly-issued token when present so the user can
		// copy a ready-to-run retry instead of hunting for the value in the
		// envelope (review gum-s985).
		tok := freeText(se.Detail, "confirmation_token")
		if tok == "" {
			tok = freeText(extras, "confirmation_token")
		}
		label := confirmationHintLabel(se, extras)
		if tok != "" {
			return label + " — retry with --confirmed --token " + tok
		}
		return label + " — run once without --confirmed to receive confirmation_token, then retry with --confirmed --token <confirmation_token>."
	case dispatch.ErrCodeConfirmationTokenInvalid:
		return "Confirmation token expired or did not match the destructive op. Re-issue the call without --confirmed to receive a fresh token."
	case dispatch.ErrCodeAuthRequired:
		return "Run `gum auth login` (or `gcloud auth application-default login`) and retry. See `gum doctor` for current auth status."
	case dispatch.ErrCodeScopeMissing:
		missing, _ := se.Detail["scopes_missing"].([]any)
		if len(missing) > 0 {
			return "Re-authenticate with the missing scopes: " + joinAny(missing) + ". Run `gum auth login --scopes=...`."
		}
		return "Re-authenticate with the OAuth scopes required by this variant. See `gum describe <op_id>` for the scopes list."
	case dispatch.ErrCodeRateLimited:
		return "Upstream rate limit hit. Wait for the Retry-After window (when present) and retry; consider --page-size to reduce request volume."
	case dispatch.ErrCodeServiceDown:
		return "Upstream service is unavailable. Retry after a brief delay; check `gum doctor` to confirm the local stack is healthy."
	case dispatch.ErrCodeOpNotFound:
		return "Op_id not in catalog. Use `gum search <keywords>` to find the correct op_id or `gum describe <op_id>` to inspect a known id."
	case dispatch.ErrCodeInvalidArgs:
		return "One or more args failed schema validation. Run `gum describe <op_id>` to see required params and example_args."
	case dispatch.ErrCodeAmbiguousVariant:
		return "Multiple variants match. Pin one with --variant-id=<id>; tab-completion lists the candidates."
	case dispatch.ErrCodeVariantNotFound:
		return "The requested --variant-id is not in the op's variants[]. Drop --variant-id to use the default, or run `gum describe <op_id>` to list valid ids."
	case dispatch.ErrCodeVariantQuarantined:
		return "Variant is quarantined. Pin a different --variant-id, or wait for the curator to lift the quarantine."
	case dispatch.ErrCodeVariantDeprecated:
		return "Variant is deprecated. Check `gum describe <op_id>` for the superseded_variant_ids mapping and pin the replacement."
	case dispatch.ErrCodeDestructiveBudgetExceeded:
		return "Destructive budget exceeded for the current session. Raise it in profile config (destructive.budget) or split the workload."
	case dispatch.ErrCodeDestructiveScopeMismatch:
		return "Destructive scope does not match the variant's declared scope. Re-issue --risk=destructive only when the variant actually requires it."
	case dispatch.ErrCodeUnsupportedCapability:
		return "Variant does not advertise the requested capability. Pick a different variant via --variant-id, or use a different op."
	case dispatch.ErrCodeCodeOutputLimitExceeded:
		return "Script output exceeded the configured cap. Persist intermediate state to disk (`@out.json`) or tighten the pipeline."
	case dispatch.ErrCodeResponseTooLarge:
		return "Upstream response exceeded the response_cap. Pass --page-size, narrow --fields, or apply a TOON profile to shrink the payload."
	case dispatch.ErrCodeResultArtifactExpired:
		return "Cached artifact expired. Re-issue the originating call; do not retry against the stale handle."
	case dispatch.ErrCodeTeeSecretCorrupt:
		return "Tee secret is corrupt — run `gum cache repair` or remove ~/.local/share/gum/<profile>/tee/ to reset."
	case dispatch.ErrCodeProjectRootRequired:
		return "Operation needs a project root. Cd into a project directory or pass --project-root=<path>."
	case dispatch.ErrCodeGainDisabled:
		return "Gain ledger is disabled. Run `gum config set gain.enabled=true` to enable it for this profile."
	case dispatch.ErrCodeGainLedgerUnavailable:
		return "Gain ledger could not be opened. Check disk space and permissions on ~/.local/share/gum/<profile>/gain-ledger.jsonl."
	case dispatch.ErrCodeCLIArgDuplicate:
		return "Same flag specified more than once. Remove duplicates and retry."
	case dispatch.ErrCodeCLIArgInvalid:
		if reason := freeText(se.Detail, "reason"); reason != "" {
			return "CLI arg invalid: " + reason
		}
		return "CLI arg invalid — re-check the §12.0 positional grammar (key=value, key:=json, @file)."
	case dispatch.ErrCodePolicyDenied:
		return "Profile policy denied this op_id. Inspect the active profile's allowlist/denylist or switch profiles via --profile=<name>."
	}
	if reason := freeText(se.Detail, "reason"); reason != "" {
		return reason
	}
	return ""
}

func confirmationHintLabel(se *dispatch.StructuredError, extras map[string]any) string {
	purpose := freeText(se.Detail, "confirmation_purpose")
	if purpose == "" {
		purpose = freeText(extras, "confirmation_purpose")
	}
	switch purpose {
	case dispatch.ConfirmationPurposeCodeWrite, dispatch.ConfirmationPurposeCodeDestroy:
		return "Elevated code"
	case dispatch.ConfirmationPurposeWrite:
		return "Write op"
	default:
		return "Destructive op"
	}
}

// freeText returns m[key] as a string when present and non-empty.
func freeText(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

// joinAny renders a []any slice of strings as a comma-separated list. Used to
// fold scopes_missing into a single human-readable hint line.
func joinAny(xs []any) string {
	parts := make([]string, 0, len(xs))
	for _, x := range xs {
		if s, ok := x.(string); ok {
			parts = append(parts, s)
		} else {
			parts = append(parts, fmt.Sprintf("%v", x))
		}
	}
	return strings.Join(parts, ", ")
}
