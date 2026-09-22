package adapters_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// Matrix row 96, response half (gum-ixky). A Google refusal that no amount of
// re-authenticating can clear MUST reach the caller as the same §7 envelope
// the compound pre-flight builds: AUTH_REQUIRED, the missing component kind,
// retryable=false and an op-scoped setup_command.
//
// Every case below drives the real TypedRestSDK against an httptest server
// that answers 403 with a real Google error body, so the assertion covers the
// decode, the table lookup and the envelope together.

// refuse403 runs one 403 body through TypedRestSDK.Execute and returns the
// *UpstreamError the adapter produced.
func refuse403(t *testing.T, status int, body string) *adapters.UpstreamError {
	t.Helper()
	verifyNoLeaks(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	inv, rv := makeTestInvAndVariant(srv.URL)
	rv.Variant.AuthStrategy = catalog.AuthStrategyBYOOAuth

	_, err := ex.Execute(t.Context(), inv, rv, &dispatch.Credentials{Token: "fake"})
	if err == nil {
		t.Fatalf("Execute returned nil error for HTTP %d", status)
	}
	var ue *adapters.UpstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %T (%v); want *adapters.UpstreamError", err, err)
	}
	return ue
}

// envelopeOf asks the adapter error for its own §7 envelope through the
// dispatch interface the kernel uses, so the test exercises the same path
// dispatch does rather than an adapter-private helper.
func envelopeOf(t *testing.T, ue *adapters.UpstreamError) *dispatch.StructuredError {
	t.Helper()
	var carrier dispatch.StructuredErrorCarrier
	if !errors.As(error(ue), &carrier) {
		t.Fatalf("%T does not implement dispatch.StructuredErrorCarrier", ue)
	}
	return carrier.AsStructuredError()
}

const workspaceAdminBlocked = `{"error":{"code":403,"status":"PERMISSION_DENIED",` +
	`"message":"Request is prohibited by organisation's policy.",` +
	`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",` +
	`"reason":"USER_BLOCKED_BY_ADMIN","domain":"googleapis.com",` +
	`"metadata":{"service":"drive.googleapis.com"}}]}}`

func TestUpstreamAdminBlockCarriesWorkspaceAdminTrust(t *testing.T) {
	ue := refuse403(t, http.StatusForbidden, workspaceAdminBlocked)

	if ue.ErrorReason != "USER_BLOCKED_BY_ADMIN" {
		t.Errorf("ErrorReason = %q; want USER_BLOCKED_BY_ADMIN", ue.ErrorReason)
	}
	if ue.ErrorDomain != "googleapis.com" {
		t.Errorf("ErrorDomain = %q; want googleapis.com", ue.ErrorDomain)
	}
	if ue.OpID != "test.op" {
		t.Errorf("OpID = %q; want test.op", ue.OpID)
	}

	se := envelopeOf(t, ue)
	if se == nil {
		t.Fatal("AsStructuredError = nil; want the §7 auth envelope")
	}
	if se.ErrCode != dispatch.ErrCodeAuthRequired {
		t.Errorf("ErrCode = %q; want AUTH_REQUIRED", se.ErrCode)
	}
	if se.Retryable {
		t.Error("Retryable = true; a retry-login loop cannot grant admin trust")
	}
	assertMissing(t, se, "workspace_admin_trust")
	if got := se.Detail["setup_command"]; got != "gum auth setup test.op" {
		t.Errorf("setup_command = %v; want the op-scoped command", got)
	}
	if got := se.Detail["auth_strategy"]; got != "byo_oauth" {
		t.Errorf("auth_strategy = %v; want byo_oauth", got)
	}
	if got := se.Detail["error_reason"]; got != "USER_BLOCKED_BY_ADMIN" {
		t.Errorf("error_reason = %v; want the upstream reason", got)
	}
}

// Drive v3 ships this refusal in the legacy error.errors[] array and emits no
// ErrorInfo detail for it, so the decode must read both shapes.
func TestUpstreamDriveDomainPolicyCarriesWorkspaceAdminTrust(t *testing.T) {
	ue := refuse403(t, http.StatusForbidden,
		`{"error":{"code":403,"message":"The domain administrators have disabled Drive apps.",`+
			`"errors":[{"domain":"global","reason":"domainPolicy",`+
			`"message":"The domain administrators have disabled Drive apps."}]}}`)

	if ue.ErrorReason != "domainPolicy" {
		t.Fatalf("ErrorReason = %q; want domainPolicy", ue.ErrorReason)
	}
	assertMissing(t, envelopeOf(t, ue), "workspace_admin_trust")
}

func TestUpstreamOrgPolicyCarriesOrgPolicyException(t *testing.T) {
	for _, reason := range []string{
		"RESOURCE_USAGE_RESTRICTION_VIOLATED",
		"ENDPOINT_USAGE_RESTRICTION_VIOLATED",
		"ORG_RESTRICTION_VIOLATION",
	} {
		t.Run(reason, func(t *testing.T) {
			ue := refuse403(t, http.StatusForbidden,
				fmt.Sprintf(`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"blocked",`+
					`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",`+
					`"reason":%q,"domain":"googleapis.com"}]}}`, reason))
			assertMissing(t, envelopeOf(t, ue), "org_policy_exception")
		})
	}
}

// Criterion 4: an unlisted reason invents nothing. IAM_PERMISSION_DENIED is a
// real 403 reason the caller fixes with an IAM grant, not an auth component.
func TestUpstreamUnmappedReasonKeepsGenericShape(t *testing.T) {
	ue := refuse403(t, http.StatusForbidden,
		`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"denied",`+
			`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",`+
			`"reason":"IAM_PERMISSION_DENIED","domain":"googleapis.com"}]}}`)

	if ue.ErrorReason != "IAM_PERMISSION_DENIED" {
		t.Errorf("ErrorReason = %q; want the reason kept on the error", ue.ErrorReason)
	}
	if se := envelopeOf(t, ue); se != nil {
		t.Fatalf("AsStructuredError = %+v; want nil so the generic shape survives", se)
	}
}

// The reason table is keyed on a 403. The same string on another status is
// not a policy refusal, so it must not borrow the envelope.
func TestUpstreamAdminReasonOnNon403KeepsGenericShape(t *testing.T) {
	ue := refuse403(t, http.StatusUnauthorized, workspaceAdminBlocked)
	if se := envelopeOf(t, ue); se != nil {
		t.Fatalf("AsStructuredError = %+v on HTTP 401; want nil", se)
	}
}

func assertMissing(t *testing.T, se *dispatch.StructuredError, want string) {
	t.Helper()
	if se == nil {
		t.Fatal("AsStructuredError = nil; want the §7 auth envelope")
	}
	got, ok := se.Detail["missing_components"].([]string)
	if !ok || len(got) != 1 || got[0] != want {
		t.Fatalf("missing_components = %v; want exactly [%s]", se.Detail["missing_components"], want)
	}
}
