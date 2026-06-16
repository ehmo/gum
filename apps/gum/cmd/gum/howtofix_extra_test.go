package main

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestHowToFixAllCodes locks remediation strings for every ErrorCode in the
// howToFix switch. A drift here means an operator who hit code X stops
// getting actionable guidance — even if the JSON envelope still validates.
func TestHowToFixAllCodes(t *testing.T) {
	cases := []struct {
		name string
		se   *dispatch.StructuredError
		want string // substring expected in returned hint
	}{
		{
			name: "risk_tool_mismatch_default_flag",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeRiskToolMismatch},
			want: "--risk=<read|write|destructive>",
		},
		{
			name: "confirmation_token_invalid",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeConfirmationTokenInvalid},
			want: "fresh token",
		},
		{
			name: "scope_missing_no_detail",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeScopeMissing},
			want: "Re-authenticate",
		},
		{
			name: "scope_missing_with_detail",
			se: &dispatch.StructuredError{
				ErrCode: dispatch.ErrCodeScopeMissing,
				Detail:  map[string]any{"scopes_missing": []any{"gmail.readonly", "calendar.events"}},
			},
			want: "gmail.readonly, calendar.events",
		},
		{
			name: "service_down",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeServiceDown},
			want: "Retry after",
		},
		{
			name: "invalid_args",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeInvalidArgs},
			want: "gum describe",
		},
		{
			name: "ambiguous_variant",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeAmbiguousVariant},
			want: "--variant-id",
		},
		{
			name: "variant_not_found",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeVariantNotFound},
			want: "Drop --variant-id",
		},
		{
			name: "variant_quarantined",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeVariantQuarantined},
			want: "quarantined",
		},
		{
			name: "variant_deprecated",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeVariantDeprecated},
			want: "deprecated",
		},
		{
			name: "destructive_budget_exceeded",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeDestructiveBudgetExceeded},
			want: "destructive.budget",
		},
		{
			name: "destructive_scope_mismatch",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeDestructiveScopeMismatch},
			want: "--risk=destructive",
		},
		{
			name: "unsupported_capability",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeUnsupportedCapability},
			want: "different variant",
		},
		{
			name: "code_output_limit_exceeded",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeCodeOutputLimitExceeded},
			want: "@out.json",
		},
		{
			name: "result_artifact_expired",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeResultArtifactExpired},
			want: "Cached artifact expired",
		},
		{
			name: "tee_secret_corrupt",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeTeeSecretCorrupt},
			want: "gum cache repair",
		},
		{
			name: "project_root_required",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeProjectRootRequired},
			want: "--project-root",
		},
		{
			name: "gain_disabled",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeGainDisabled},
			want: "gain.enabled=true",
		},
		{
			name: "gain_ledger_unavailable",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeGainLedgerUnavailable},
			want: "disk space",
		},
		{
			name: "cli_arg_duplicate",
			se:   &dispatch.StructuredError{ErrCode: dispatch.ErrCodeCLIArgDuplicate},
			want: "Remove duplicates",
		},
		{
			name: "cli_arg_invalid_with_detail_reason",
			se: &dispatch.StructuredError{
				ErrCode: dispatch.ErrCodeCLIArgInvalid,
				Detail:  map[string]any{"reason": "trailing comma"},
			},
			want: "CLI arg invalid: trailing comma",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := howToFix(tc.se, nil)
			if !strings.Contains(got, tc.want) {
				t.Errorf("howToFix(%s)=%q, missing %q", tc.se.ErrCode, got, tc.want)
			}
		})
	}
}

// TestHowToFixExtrasHintWins covers the second priority hop: when
// detail.hint is absent but extras.hint is present, extras wins over the
// per-code switch.
func TestHowToFixExtrasHintWins(t *testing.T) {
	se := &dispatch.StructuredError{ErrCode: dispatch.ErrCodeAuthRequired}
	got := howToFix(se, map[string]any{"hint": "extras override"})
	if got != "extras override" {
		t.Errorf("got %q, want %q (extras.hint should win)", got, "extras override")
	}
}

// TestHowToFixRiskToolMismatchWithRequiredFlag covers the variant where
// extras carries required_risk_flag — the message must echo the exact flag
// rather than the generic placeholder.
func TestHowToFixRiskToolMismatchWithRequiredFlag(t *testing.T) {
	se := &dispatch.StructuredError{ErrCode: dispatch.ErrCodeRiskToolMismatch}
	got := howToFix(se, map[string]any{"required_risk_flag": "--risk=destructive"})
	if !strings.Contains(got, "--risk=destructive") {
		t.Errorf("got %q, missing required_risk_flag echo", got)
	}
	if strings.Contains(got, "--risk=<read") {
		t.Errorf("got %q, must not include placeholder", got)
	}
}

// TestHowToFixUnknownCodeFallsBackToReason covers the bottom branch: an
// unmapped error code with detail.reason returns the reason verbatim.
func TestHowToFixUnknownCodeFallsBackToReason(t *testing.T) {
	se := &dispatch.StructuredError{
		ErrCode: dispatch.ErrorCode("UNMAPPED_FUTURE_CODE"),
		Detail:  map[string]any{"reason": "the upstream said no"},
	}
	if got := howToFix(se, nil); got != "the upstream said no" {
		t.Errorf("got %q, want fallback to detail.reason", got)
	}
}

// TestHowToFixUnknownCodeReturnsEmpty covers the terminal fallthrough:
// unmapped code, no detail.reason → empty string (caller suppresses the
// how_to_fix field).
func TestHowToFixUnknownCodeReturnsEmpty(t *testing.T) {
	se := &dispatch.StructuredError{ErrCode: dispatch.ErrorCode("UNMAPPED_FUTURE_CODE")}
	if got := howToFix(se, nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestFreeText covers the helper's three branches: nil map, missing key,
// non-string value, and the trim/empty-detection logic.
func TestFreeText(t *testing.T) {
	t.Run("nil_map", func(t *testing.T) {
		if got := freeText(nil, "k"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("missing_key", func(t *testing.T) {
		if got := freeText(map[string]any{"other": "v"}, "k"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("non_string_value", func(t *testing.T) {
		if got := freeText(map[string]any{"k": 42}, "k"); got != "" {
			t.Errorf("got %q, want empty for non-string", got)
		}
	})
	t.Run("whitespace_only", func(t *testing.T) {
		if got := freeText(map[string]any{"k": "   "}, "k"); got != "" {
			t.Errorf("got %q, want empty after trim", got)
		}
	})
	t.Run("trimmed_value", func(t *testing.T) {
		if got := freeText(map[string]any{"k": "  hello  "}, "k"); got != "hello" {
			t.Errorf("got %q, want hello", got)
		}
	})
}
