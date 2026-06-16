package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/auth"
)

// TestServiceAccountResolverNilReceiverSurfacesNotConfigured pins the
// nil-receiver guard: a caller dereferencing through a half-built
// dispatcher (composite.SA == nil that was then assigned from a
// returning nil) must NOT panic. Instead the helper surfaces
// AUTH_SA_KEY_NOT_CONFIGURED with a clear remediation pointer.
func TestServiceAccountResolverNilReceiverSurfacesNotConfigured(t *testing.T) {
	var r *auth.ServiceAccountResolver // explicitly nil
	_, err := r.Resolve(context.Background(), nil)
	var ae *auth.AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "AUTH_SA_KEY_NOT_CONFIGURED" {
		t.Errorf("Code=%q; want AUTH_SA_KEY_NOT_CONFIGURED", ae.Code)
	}
	if ae.Strategy != "service_account_key" {
		t.Errorf("Strategy=%q; want service_account_key", ae.Strategy)
	}
}
