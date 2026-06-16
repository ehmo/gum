package auth

import (
	"errors"
	"testing"
)

// TestCanStartGumOAuthTestingModeAllowsOptedInScope is the gum-ytdd tracer
// bullet: when the GUM-managed OAuth project is in Google "testing" publishing
// status, a scope the operator has explicitly opted in (testing_allowed=true)
// becomes eligible for gum_oauth even though it has NOT completed full
// verification (status still planned / states still pending). This is the
// "ship testing now, verify in parallel" rollout path — works immediately for
// the ≤100-user cap behind the unverified-app warning.
func TestCanStartGumOAuthTestingModeAllowsOptedInScope(t *testing.T) {
	const scope = "https://www.googleapis.com/auth/webmasters.readonly"
	m := &managedScopesManifest{
		SchemaVersion:  1,
		ManagedProject: managedProject{PublishingStatus: "testing"},
		Scopes: []managedScopeEntry{{
			Scope:                scope,
			Status:               "planned",
			VerificationState:    "pending",
			ProjectEvidenceState: "pending",
			LiveCanaryState:      "pending",
			TestingAllowed:       true,
		}},
	}
	if err := canStartGumOAuth(m, []string{scope}); err != nil {
		t.Fatalf("testing-mode opted-in scope should be eligible, got: %v", err)
	}
}

// TestCanStartGumOAuthTestingModeRequiresOptIn pins that the testing path is an
// explicit, per-scope opt-in — production-honest by default. A scope under a
// testing-status project that has NOT set testing_allowed stays gated, so
// merely flipping the project to "testing" never silently exposes every listed
// scope.
func TestCanStartGumOAuthTestingModeRequiresOptIn(t *testing.T) {
	const scope = "https://www.googleapis.com/auth/gmail.readonly"
	m := &managedScopesManifest{
		SchemaVersion:  1,
		ManagedProject: managedProject{PublishingStatus: "testing"},
		Scopes: []managedScopeEntry{{
			Scope:                scope,
			Status:               "planned",
			VerificationState:    "pending",
			ProjectEvidenceState: "pending",
			LiveCanaryState:      "pending",
			TestingAllowed:       false,
		}},
	}
	err := canStartGumOAuth(m, []string{scope})
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "GUM_OAUTH_MANAGED_CLIENT_NOT_READY" {
		t.Fatalf("non-opted-in scope under testing must stay gated, got: %v", err)
	}
}

// TestCanStartGumOAuthTestingFlagInertOutsideTestingStatus pins the other half
// of the gate: testing_allowed is only honored while the project is actually in
// "testing" publishing status. Once the project is "planned" (or any non-testing
// status) the flag is inert, so a scope that is neither fully promoted nor in an
// active testing window stays gated. This keeps production strict regardless of
// stale per-scope flags.
func TestCanStartGumOAuthTestingFlagInertOutsideTestingStatus(t *testing.T) {
	const scope = "https://www.googleapis.com/auth/webmasters.readonly"
	m := &managedScopesManifest{
		SchemaVersion:  1,
		ManagedProject: managedProject{PublishingStatus: "planned"},
		Scopes: []managedScopeEntry{{
			Scope:                scope,
			Status:               "planned",
			VerificationState:    "pending",
			ProjectEvidenceState: "pending",
			LiveCanaryState:      "pending",
			TestingAllowed:       true,
		}},
	}
	err := canStartGumOAuth(m, []string{scope})
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "GUM_OAUTH_MANAGED_CLIENT_NOT_READY" {
		t.Fatalf("testing_allowed must be inert outside testing status, got: %v", err)
	}
}

// TestCanStartGumOAuthEmbeddedSecretRejected pins the embedded-secret guard
// directly at the gate (distinct from the loadManagedScopesManifest decode
// arm): a manifest that declares embedded_client_secret=true MUST fail with
// GUM_OAUTH_MANIFEST_INVALID even when scopes are otherwise eligible. GUM never
// ships a client secret in a distributed binary (spec §7).
func TestCanStartGumOAuthEmbeddedSecretRejected(t *testing.T) {
	const scope = "https://www.googleapis.com/auth/webmasters.readonly"
	m := &managedScopesManifest{
		SchemaVersion:  1,
		ClientPolicy:   managedClientPolicy{EmbeddedClientSecret: true},
		ManagedProject: managedProject{PublishingStatus: "testing"},
		Scopes: []managedScopeEntry{{
			Scope:          scope,
			Status:         "planned",
			TestingAllowed: true,
		}},
	}
	err := canStartGumOAuth(m, []string{scope})
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "GUM_OAUTH_MANIFEST_INVALID" {
		t.Fatalf("embedded_client_secret=true must fail GUM_OAUTH_MANIFEST_INVALID, got: %v", err)
	}
}
