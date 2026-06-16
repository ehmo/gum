package auth

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCallbackHandleUserDeniedSurfacesUserDeniedError pins the
// q.Get("error") != "" arm: when the IdP redirects back with
// error=access_denied (the canonical "user clicked Deny"), the handler
// MUST emit a GUM_OAUTH_USER_DENIED on cb.done with the error and
// error_description echoed into HumanRemediation so the operator can
// diagnose without re-running with -v.
func TestCallbackHandleUserDeniedSurfacesUserDeniedError(t *testing.T) {
	cb := &callbackServer{
		expectState: "expected-state",
		done:        make(chan callbackResult, 1),
	}
	req := httptest.NewRequest(
		"GET",
		"/oauth/callback?state=expected-state&error=access_denied&error_description=user+clicked+deny",
		nil,
	)
	rec := httptest.NewRecorder()
	cb.handle(rec, req)

	res := <-cb.done
	if res.err == nil {
		t.Fatal("want err on user-denied callback; got nil")
	}
	var ae *AuthError
	if !errors.As(res.err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", res.err, res.err)
	}
	if ae.Code != "GUM_OAUTH_USER_DENIED" {
		t.Errorf("Code=%q; want GUM_OAUTH_USER_DENIED", ae.Code)
	}
	if !strings.Contains(ae.HumanRemediation, "access_denied") {
		t.Errorf("HumanRemediation=%q; want 'access_denied' echoed", ae.HumanRemediation)
	}
	if !strings.Contains(ae.HumanRemediation, "user clicked deny") {
		t.Errorf("HumanRemediation=%q; want error_description echoed", ae.HumanRemediation)
	}
}

// TestCallbackHandleMissingCodeSurfacesCallbackInvalid pins the
// `code == ""` arm: a callback URL with matching state, no error, but
// no code parameter (malformed redirect) MUST surface
// GUM_OAUTH_CALLBACK_INVALID rather than silently completing as a
// successful auth.
func TestCallbackHandleMissingCodeSurfacesCallbackInvalid(t *testing.T) {
	cb := &callbackServer{
		expectState: "s",
		done:        make(chan callbackResult, 1),
	}
	req := httptest.NewRequest("GET", "/oauth/callback?state=s", nil)
	rec := httptest.NewRecorder()
	cb.handle(rec, req)

	res := <-cb.done
	var ae *AuthError
	if !errors.As(res.err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", res.err, res.err)
	}
	if ae.Code != "GUM_OAUTH_CALLBACK_INVALID" {
		t.Errorf("Code=%q; want GUM_OAUTH_CALLBACK_INVALID", ae.Code)
	}
}
