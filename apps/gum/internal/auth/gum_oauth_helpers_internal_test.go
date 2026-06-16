package auth

import (
	"net/http"
	"testing"
	"time"
)

// TestGumOAuthAuthURLHonorsOverride: the override branch returns
// AuthURL verbatim. Default-branch is exercised by the other suite
// tests but covered here for symmetry; what we lock in is the
// "override wins" contract so test rigs can point at httptest servers.
func TestGumOAuthAuthURLHonorsOverride(t *testing.T) {
	g := &GumOAuth{AuthURL: "https://example/authz"}
	if got := g.authURL(); got != "https://example/authz" {
		t.Errorf("authURL=%q; want override", got)
	}

	if got := (&GumOAuth{}).authURL(); got != defaultGumOAuthAuthURL {
		t.Errorf("default authURL=%q; want %q", got, defaultGumOAuthAuthURL)
	}
}

// TestGumOAuthTokenURLHonorsOverride: symmetric to authURL — overriding
// TokenURL lets the test rig redirect the exchange to an httptest stub.
func TestGumOAuthTokenURLHonorsOverride(t *testing.T) {
	g := &GumOAuth{TokenURL: "https://example/token"}
	if got := g.tokenURL(); got != "https://example/token" {
		t.Errorf("tokenURL=%q; want override", got)
	}

	if got := (&GumOAuth{}).tokenURL(); got != defaultGumOAuthTokenURL {
		t.Errorf("default tokenURL=%q; want %q", got, defaultGumOAuthTokenURL)
	}
}

// TestGumOAuthHTTPClientHonorsOverride: caller-supplied *http.Client flows
// through (lets tests inject a Transport that records requests); nil falls
// back to a bounded default token-exchange client.
func TestGumOAuthHTTPClientHonorsOverride(t *testing.T) {
	custom := &http.Client{Timeout: time.Second}
	if got := (&GumOAuth{HTTPClient: custom}).httpClient(); got != custom {
		t.Errorf("httpClient did not return override")
	}
	if got := (&GumOAuth{}).httpClient(); got == nil || got.Timeout != defaultAuthHTTPTimeout {
		t.Errorf("default httpClient timeout=%v; want %v", got.Timeout, defaultAuthHTTPTimeout)
	}
}

// TestGumOAuthNowHonorsOverride: the Now hook lets deterministic tests
// freeze time around exchange + expiry computations.
func TestGumOAuthNowHonorsOverride(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	g := &GumOAuth{Now: func() time.Time { return fixed }}
	if got := g.now(); !got.Equal(fixed) {
		t.Errorf("now=%v; want fixed", got)
	}
	// Default path: returns a value within 1s of time.Now (sanity check
	// that the fallback uses the system clock, not a zero time).
	if got := (&GumOAuth{}).now(); time.Since(got) > time.Second {
		t.Errorf("default now=%v; want recent", got)
	}
}

// TestGumOAuthClientIDOverrideWins: ClientIDOverride beats the
// manifest's ClientPolicy.ClientID, so test rigs can stand up a
// dedicated OAuth client without round-tripping through the manifest.
func TestGumOAuthClientIDOverrideWins(t *testing.T) {
	m := &managedScopesManifest{
		ClientPolicy: managedClientPolicy{ClientID: "manifest-id"},
	}
	if got := (&GumOAuth{ClientIDOverride: "test-id"}).clientID(m); got != "test-id" {
		t.Errorf("override clientID=%q; want test-id", got)
	}
	if got := (&GumOAuth{}).clientID(m); got != "manifest-id" {
		t.Errorf("default clientID=%q; want manifest-id", got)
	}
}
