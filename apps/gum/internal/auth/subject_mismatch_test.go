package auth

// Wrong-account protection at login time (beads gum-znmd, gum-q0kd).
//
// Both interactive flows show an account chooser, so the account the operator
// picks in the browser is not necessarily the account the profile is already
// bound to. Before these tests nothing compared the two: the freshly returned
// subject silently replaced the stored one, orphaning every cache entry, tee
// artifact and gain-ledger row keyed on the old fingerprint. Spec §7's
// credential-resolution order requires the opposite - a credential is used
// "only if its auth_subject_fingerprint matches the selected profile's
// expected subject when one is recorded".

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zalando/go-keyring"
)

// newSubjectTokenServer serves authorization-code exchanges whose id_token
// carries whatever subject *sub holds at request time, so one test can drive
// two consecutive logins as two different Google accounts.
func newSubjectTokenServer(t *testing.T, sub *string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"at","refresh_token":"rt-%s","expires_in":3600,"token_type":"Bearer","id_token":%q}`, *sub, fakeIDToken(*sub))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newManagedLogin wires a GumOAuth against the fake authorization and token
// endpoints, with a manifest that promotes scope.
func newManagedLogin(t *testing.T, scope string, sub *string) *GumOAuth {
	t.Helper()

	tokenSrv := newSubjectTokenServer(t, sub)
	authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
	t.Cleanup(authSrv.Close)

	return &GumOAuth{
		Vault:            NewCredentialVault(&OSKeyring{}),
		AuthURL:          authSrv.URL,
		TokenURL:         tokenSrv.URL,
		HTTPClient:       tokenSrv.Client(),
		ManifestBody:     promotedManifest(scope),
		ClientIDOverride: "test-client-id",
		BrowserOpener: func(authURL string) error {
			go followAuthURL(authURL)
			return nil
		},
	}
}

func TestManagedLoginRefusesASecondAccountForTheSameScopes(t *testing.T) {
	keyring.MockInit()
	scope := "https://example.test/scope/a"
	sub := "subject-1"
	g := newManagedLogin(t, scope, &sub)
	scopes := []string{scope}

	first, err := g.Login(context.Background(), scopes)
	if err != nil {
		t.Fatalf("first Login: %v", err)
	}

	sub = "subject-2"
	if _, err := g.Login(context.Background(), scopes); err == nil {
		t.Fatal("second Login with a different account succeeded; it must refuse")
	} else {
		var ae *AuthError
		if !errors.As(err, &ae) {
			t.Fatalf("second Login: want *AuthError, got %T: %v", err, err)
		}
		if ae.Code != "AUTH_SUBJECT_MISMATCH" {
			t.Errorf("Code = %q; want AUTH_SUBJECT_MISMATCH", ae.Code)
		}
		if ae.Strategy != "gum_oauth" {
			t.Errorf("Strategy = %q; want gum_oauth", ae.Strategy)
		}
	}

	stored, err := g.Vault.LookupGumOAuthSubject(scopes)
	if err != nil {
		t.Fatalf("LookupGumOAuthSubject: %v", err)
	}
	if stored != first.SubjectFingerprint {
		t.Errorf("stored subject = %q; want the first account's %q - a refused login must store nothing", stored, first.SubjectFingerprint)
	}
}

func TestManagedLoginAcceptsTheSameAccountTwice(t *testing.T) {
	keyring.MockInit()
	scope := "https://example.test/scope/a"
	sub := "subject-1"
	g := newManagedLogin(t, scope, &sub)
	scopes := []string{scope}

	first, err := g.Login(context.Background(), scopes)
	if err != nil {
		t.Fatalf("first Login: %v", err)
	}
	second, err := g.Login(context.Background(), scopes)
	if err != nil {
		t.Fatalf("second Login with the same account: %v", err)
	}
	if second.SubjectFingerprint != first.SubjectFingerprint {
		t.Errorf("fingerprint moved between logins: %q then %q", first.SubjectFingerprint, second.SubjectFingerprint)
	}
}

func TestManagedLoginSwitchesAccountsWhenAsked(t *testing.T) {
	keyring.MockInit()
	scope := "https://example.test/scope/a"
	sub := "subject-1"
	g := newManagedLogin(t, scope, &sub)
	scopes := []string{scope}

	first, err := g.Login(context.Background(), scopes)
	if err != nil {
		t.Fatalf("first Login: %v", err)
	}

	sub = "subject-2"
	g.AllowSubjectChange = true
	second, err := g.Login(context.Background(), scopes)
	if err != nil {
		t.Fatalf("Login with AllowSubjectChange: %v", err)
	}
	if second.SubjectFingerprint == first.SubjectFingerprint {
		t.Fatal("fingerprint did not change; the second account was not adopted")
	}

	stored, err := g.Vault.LookupGumOAuthSubject(scopes)
	if err != nil {
		t.Fatalf("LookupGumOAuthSubject: %v", err)
	}
	if stored != second.SubjectFingerprint {
		t.Errorf("stored subject = %q; want the second account's %q", stored, second.SubjectFingerprint)
	}
}

func TestByoLoginRefusesASubjectTheProfileDidNotExpect(t *testing.T) {
	sub := "someone-else@example.test"
	tokenSrv := newSubjectTokenServer(t, &sub)
	authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
	t.Cleanup(authSrv.Close)

	kb := isoKeyring{}
	b := NewByoOAuth(ByoOAuthConfig{
		ClientID:        "test-client-id",
		Profile:         "default",
		Scopes:          []string{"https://example.test/scope/a"},
		AuthEndpoint:    authSrv.URL,
		TokenEndpoint:   tokenSrv.URL,
		ExpectedSubject: byoSubjectFingerprint("owner@example.test", ""),
	}, kb)
	b.BrowserOpener = func(authURL string) error {
		go followAuthURL(authURL)
		return nil
	}

	_, err := b.Login(context.Background())
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("Login: want *AuthError, got %T: %v", err, err)
	}
	if ae.Code != "AUTH_SUBJECT_MISMATCH" {
		t.Errorf("Code = %q; want AUTH_SUBJECT_MISMATCH", ae.Code)
	}

	// A refused login must not have swapped the stored grant.
	for key, raw := range kb {
		var grant byoGrant
		if json.Unmarshal([]byte(raw), &grant) == nil && grant.Subject == sub {
			t.Fatalf("key %q holds the refused account's grant", key)
		}
	}
}

func TestByoLoginAdoptsTheExpectedSubject(t *testing.T) {
	sub := "owner@example.test"
	tokenSrv := newSubjectTokenServer(t, &sub)
	authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
	t.Cleanup(authSrv.Close)

	b := NewByoOAuth(ByoOAuthConfig{
		ClientID:        "test-client-id",
		Profile:         "default",
		Scopes:          []string{"https://example.test/scope/a"},
		AuthEndpoint:    authSrv.URL,
		TokenEndpoint:   tokenSrv.URL,
		ExpectedSubject: byoSubjectFingerprint(sub, ""),
	}, isoKeyring{})
	b.BrowserOpener = func(authURL string) error {
		go followAuthURL(authURL)
		return nil
	}

	creds, err := b.Login(context.Background())
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if creds.SubjectFingerprint != byoSubjectFingerprint(sub, "") {
		t.Errorf("SubjectFingerprint = %q; want %q", creds.SubjectFingerprint, byoSubjectFingerprint(sub, ""))
	}
}
