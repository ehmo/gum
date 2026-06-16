package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestManagedSupportedScopesIncludesSearchConsoleReadonly(t *testing.T) {
	got, err := ManagedSupportedScopes()
	if err != nil {
		t.Fatalf("ManagedSupportedScopes: %v", err)
	}
	var found bool
	for _, s := range got {
		if s == "https://www.googleapis.com/auth/webmasters.readonly" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ManagedSupportedScopes=%v; want webmasters.readonly", got)
	}
}

func TestInvalidIDTokenErrorShape(t *testing.T) {
	err := invalidIDTokenError("missing sub claim")
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T; want *AuthError", err)
	}
	if ae.Code != "GUM_OAUTH_ID_TOKEN_INVALID" {
		t.Fatalf("Code=%q; want GUM_OAUTH_ID_TOKEN_INVALID", ae.Code)
	}
	if !strings.Contains(ae.HumanRemediation, "missing sub claim") {
		t.Fatalf("HumanRemediation=%q; want detail", ae.HumanRemediation)
	}
}
