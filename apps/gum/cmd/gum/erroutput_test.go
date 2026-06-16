package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestRenderStructuredEnvelope_AddsHowToFix verifies that every known error
// code grows a "how_to_fix" key while preserving the original §1421 envelope
// verbatim under "machine_envelope" (gum-fkme).
func TestRenderStructuredEnvelope_AddsHowToFix(t *testing.T) {
	cases := []struct {
		name string
		code dispatch.ErrorCode
		want string // substring expected in how_to_fix
	}{
		{"auth_required", dispatch.ErrCodeAuthRequired, "gum auth login"},
		{"requires_confirmation", dispatch.ErrCodeRequiresConfirmation, "--confirmed --token"},
		{"op_not_found", dispatch.ErrCodeOpNotFound, "gum search"},
		{"response_too_large", dispatch.ErrCodeResponseTooLarge, "response_cap"},
		{"rate_limited", dispatch.ErrCodeRateLimited, "Retry-After"},
		{"policy_denied", dispatch.ErrCodePolicyDenied, "allowlist/denylist"},
		{"cli_arg_invalid", dispatch.ErrCodeCLIArgInvalid, "§12.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			se := &dispatch.StructuredError{
				ErrCode: tc.code,
				Message: "boom",
			}
			var buf bytes.Buffer
			if err := renderStructuredEnvelope(&buf, se, nil); err != nil {
				t.Fatalf("renderStructuredEnvelope: %v", err)
			}
			var env map[string]any
			if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
				t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
			}
			hint, _ := env["how_to_fix"].(string)
			if !strings.Contains(hint, tc.want) {
				t.Errorf("how_to_fix for %s = %q, want substring %q", tc.code, hint, tc.want)
			}
			machine, ok := env["machine_envelope"].(map[string]any)
			if !ok {
				t.Fatalf("machine_envelope missing or wrong type: %v", env["machine_envelope"])
			}
			if machine["error_code"] != string(tc.code) {
				t.Errorf("machine_envelope.error_code = %v, want %s", machine["error_code"], tc.code)
			}
			if machine["message"] != "boom" {
				t.Errorf("machine_envelope.message = %v, want boom", machine["message"])
			}
			// machine_envelope MUST NOT carry the human-only fields.
			if _, leaked := machine["how_to_fix"]; leaked {
				t.Error("machine_envelope leaked how_to_fix — it must stay §1421-pure")
			}
		})
	}
}

func TestRenderStructuredEnvelope_CodeConfirmationHowToFix(t *testing.T) {
	se := dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation, "confirm code").
		WithDetail("confirmation_purpose", dispatch.ConfirmationPurposeCodeWrite).
		WithDetail("confirmation_token", "tok-code")

	var buf bytes.Buffer
	if err := renderStructuredEnvelope(&buf, se, nil); err != nil {
		t.Fatalf("renderStructuredEnvelope: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	hint, _ := env["how_to_fix"].(string)
	if !strings.Contains(hint, "Elevated code") {
		t.Fatalf("how_to_fix = %q; want Elevated code", hint)
	}
	if strings.Contains(hint, "Destructive op") {
		t.Fatalf("how_to_fix = %q; must not label code confirmation as destructive", hint)
	}
}

// TestRenderStructuredEnvelope_PreferDetailHint verifies that an explicit
// detail.hint trumps the canned per-code remediation. This is the seam where
// adapters surface site-specific guidance (gum-fkme).
func TestRenderStructuredEnvelope_PreferDetailHint(t *testing.T) {
	se := &dispatch.StructuredError{
		ErrCode: dispatch.ErrCodeAuthRequired,
		Message: "boom",
		Detail:  map[string]any{"hint": "Run my-special-command"},
	}
	var buf bytes.Buffer
	if err := renderStructuredEnvelope(&buf, se, nil); err != nil {
		t.Fatalf("renderStructuredEnvelope: %v", err)
	}
	var env map[string]any
	_ = json.Unmarshal(buf.Bytes(), &env)
	if env["how_to_fix"] != "Run my-special-command" {
		t.Errorf("how_to_fix = %v, want detail.hint override", env["how_to_fix"])
	}
}

// TestJoinAny verifies the scopes-missing folder used inside how_to_fix hints.
// Non-string elements should be coerced via fmt to avoid a panic on malformed
// upstream payloads.
func TestJoinAny(t *testing.T) {
	cases := []struct {
		name string
		in   []any
		want string
	}{
		{name: "empty", in: nil, want: ""},
		{name: "single_string", in: []any{"https://www.googleapis.com/auth/gmail.readonly"}, want: "https://www.googleapis.com/auth/gmail.readonly"},
		{name: "two_strings", in: []any{"a", "b"}, want: "a, b"},
		{name: "mixed_types", in: []any{"a", 42, true}, want: "a, 42, true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinAny(tc.in); got != tc.want {
				t.Errorf("joinAny(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRenderStructuredEnvelope_FlatDetail verifies §1421 layout: detail keys
// remain at the top level (never nested), and extras are merged in too.
func TestRenderStructuredEnvelope_FlatDetail(t *testing.T) {
	se := &dispatch.StructuredError{
		ErrCode: dispatch.ErrCodeRiskToolMismatch,
		Message: "risk mismatch",
		Detail:  map[string]any{"variant_risk_class": "destructive"},
	}
	extras := map[string]any{
		"requested_risk":     "read",
		"required_risk_flag": "--risk=destructive",
	}
	var buf bytes.Buffer
	if err := renderStructuredEnvelope(&buf, se, extras); err != nil {
		t.Fatalf("renderStructuredEnvelope: %v", err)
	}
	var env map[string]any
	_ = json.Unmarshal(buf.Bytes(), &env)
	if env["variant_risk_class"] != "destructive" {
		t.Errorf("detail flattening failed: %v", env)
	}
	if env["required_risk_flag"] != "--risk=destructive" {
		t.Errorf("extras flattening failed: %v", env)
	}
	hint, _ := env["how_to_fix"].(string)
	if !strings.Contains(hint, "--risk=destructive") {
		t.Errorf("how_to_fix did not surface required_risk_flag: %q", hint)
	}
}
