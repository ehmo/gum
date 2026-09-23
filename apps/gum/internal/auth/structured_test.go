package auth_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestAuthErrorIsStructuredErrorCarrier pins the seam the dispatch kernel uses.
// If *AuthError stops implementing the carrier, resolveAuth silently falls back
// to flattening every auth failure into AUTH_REQUIRED and this test is the only
// thing that notices.
func TestAuthErrorIsStructuredErrorCarrier(t *testing.T) {
	var err error = &auth.AuthError{Code: "AUTH_REQUIRED"}

	var carrier dispatch.StructuredErrorCarrier
	if !errors.As(err, &carrier) {
		t.Fatal("*auth.AuthError no longer satisfies dispatch.StructuredErrorCarrier")
	}
}

// TestCompoundAuthErrorKeepsEnvelopeFields pins spec §7: a
// compound failure must name its strategy, its missing components and the
// command that fixes them.
func TestCompoundAuthErrorKeepsEnvelopeFields(t *testing.T) {
	ae := &auth.AuthError{
		Code:              "AUTH_REQUIRED",
		Strategy:          "compound",
		HumanRemediation:  "compound auth requires multiple components",
		MissingComponents: []string{"developer_token", "customer_id"},
		SetupCommand:      "gum auth setup googleads.generateKeywordIdeas",
		OpID:              "googleads.generateKeywordIdeas",
		UserMessage:       "This operation needs additional credentials.",
		Retryable:         false,
	}

	se := ae.AsStructuredError()
	if se == nil {
		t.Fatal("want structured error, got nil")
	}
	if se.ErrCode != "AUTH_REQUIRED" {
		t.Errorf("ErrCode=%q want AUTH_REQUIRED", se.ErrCode)
	}
	if se.Message != ae.HumanRemediation {
		t.Errorf("Message=%q want %q", se.Message, ae.HumanRemediation)
	}
	if se.Detail["auth_strategy"] != "compound" {
		t.Errorf("auth_strategy=%v want compound", se.Detail["auth_strategy"])
	}
	if se.Detail["setup_command"] != ae.SetupCommand {
		t.Errorf("setup_command=%v want %q", se.Detail["setup_command"], ae.SetupCommand)
	}
	if se.Detail["op_id"] != ae.OpID {
		t.Errorf("op_id=%v want %q", se.Detail["op_id"], ae.OpID)
	}
	if se.Detail["user_message"] != ae.UserMessage {
		t.Errorf("user_message=%v want %q", se.Detail["user_message"], ae.UserMessage)
	}
	missing, ok := se.Detail["missing_components"].([]string)
	if !ok || len(missing) != 2 || missing[0] != "developer_token" {
		t.Errorf("missing_components=%v want [developer_token customer_id]", se.Detail["missing_components"])
	}
}

// TestAuthErrorKeepsScopePayload covers the SCOPE_MISSING side of the same
// envelope: required_scopes and have_scopes must reach the caller so an agent
// can tell which scope is absent without re-running the op.
func TestAuthErrorKeepsScopePayload(t *testing.T) {
	ae := &auth.AuthError{
		Code:             "SCOPE_MISSING",
		Strategy:         "byo_oauth",
		HumanRemediation: "re-run gum auth login with the missing scope",
		RequiredScopes:   []string{"https://www.googleapis.com/auth/gmail.readonly"},
		HaveScopes:       []string{"https://www.googleapis.com/auth/webmasters.readonly"},
		Retryable:        true,
	}

	se := ae.AsStructuredError()
	if se.ErrCode != "SCOPE_MISSING" {
		t.Errorf("ErrCode=%q want SCOPE_MISSING", se.ErrCode)
	}
	if !se.Retryable {
		t.Error("Retryable=false want true")
	}
	req, _ := se.Detail["required_scopes"].([]string)
	have, _ := se.Detail["have_scopes"].([]string)
	if len(req) != 1 || len(have) != 1 {
		t.Errorf("scope payload dropped: required=%v have=%v", se.Detail["required_scopes"], se.Detail["have_scopes"])
	}
}

// TestAuthErrorOmitsEmptyFields keeps the envelope key set identical to
// MarshalJSON's: an absent field must be absent, not an empty string that an
// agent would read as a real value.
func TestAuthErrorOmitsEmptyFields(t *testing.T) {
	se := (&auth.AuthError{Code: "AUTH_KEYCHAIN_UNAVAILABLE", HumanRemediation: "unlock the keychain"}).AsStructuredError()

	for _, key := range []string{"auth_strategy", "op_id", "setup_command", "missing_components", "required_scopes", "have_scopes"} {
		if _, present := se.Detail[key]; present {
			t.Errorf("detail %q present on a bare error: %v", key, se.Detail[key])
		}
	}
	if se.Detail["user_message"] != "unlock the keychain" {
		t.Errorf("user_message=%v want the HumanRemediation fallback", se.Detail["user_message"])
	}
}

// TestAuthErrorNilCodeDefaults guards the zero value: an AuthError built
// without a Code must not produce an empty error_code in the §7 envelope.
func TestAuthErrorNilCodeDefaults(t *testing.T) {
	if se := (&auth.AuthError{HumanRemediation: "x"}).AsStructuredError(); se.ErrCode != "AUTH_REQUIRED" {
		t.Errorf("ErrCode=%q want AUTH_REQUIRED", se.ErrCode)
	}
	var nilErr *auth.AuthError
	if se := nilErr.AsStructuredError(); se != nil {
		t.Errorf("nil receiver returned %v, want nil", se)
	}
}
