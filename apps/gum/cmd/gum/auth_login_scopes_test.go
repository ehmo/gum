package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
)

// withManagedClientEnv swaps the embedded managed-client identity for a test.
func withManagedClientEnv(t *testing.T, id, secret string) {
	t.Helper()
	origID, origSecret := embedded.GumOAuthClientID, embedded.GumOAuthClientSecret
	embedded.GumOAuthClientID = id
	embedded.GumOAuthClientSecret = secret
	t.Cleanup(func() {
		embedded.GumOAuthClientID = origID
		embedded.GumOAuthClientSecret = origSecret
	})
}

// TestResolveLoginScopesExplicitNormalises pins that operator-supplied --scope
// short forms are expanded to the fully-qualified URLs Google requires, so the
// user can type `--scope gmail.readonly` without guessing the full URL.
func TestResolveLoginScopesExplicitNormalises(t *testing.T) {
	got, err := resolveLoginScopes(nil, []string{"gmail.readonly", "https://www.googleapis.com/auth/calendar"}, nil, false)
	if err != nil {
		t.Fatalf("resolveLoginScopes: %v", err)
	}
	want := []string{
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/calendar",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scope[%d]=%q; want %q", i, got[i], want[i])
		}
	}
}

// scopeSelectionCatalog has one core-service op (drive) and one breadth op
// (youtube), so the default/--service/--all selection logic can be exercised.
func scopeSelectionCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		Ops: []catalog.Op{
			{OpID: "drive.files.list", Service: "drive", DefaultVariantID: "d.v1", Variants: []catalog.Variant{{
				VariantID: "d.v1", Scopes: []string{"https://www.googleapis.com/auth/drive.readonly"}}}},
			{OpID: "youtube.search.list", Service: "youtube", DefaultVariantID: "y.v1", Variants: []catalog.Variant{{
				VariantID: "y.v1", Scopes: []string{"https://www.googleapis.com/auth/youtube.readonly"}}}},
		},
	}
}

// TestResolveLoginScopesDefaultIsCore pins the lean default: with no flags the
// login requests only the core Workspace services' scopes (drive), NOT the
// breadth services (youtube) — so a typical BYO consent screen isn't asked to
// grant scopes for APIs the project never enabled.
func TestResolveLoginScopesDefaultIsCore(t *testing.T) {
	got, err := resolveLoginScopes(scopeSelectionCatalog(), nil, nil, false)
	if err != nil {
		t.Fatalf("resolveLoginScopes: %v", err)
	}
	if len(got) != 1 || got[0] != "https://www.googleapis.com/auth/drive.readonly" {
		t.Errorf("default login scopes = %v; want only the core drive scope", got)
	}
}

// TestResolveLoginScopesServiceSelects pins --service: only the named services'
// scopes are requested.
func TestResolveLoginScopesServiceSelects(t *testing.T) {
	got, err := resolveLoginScopes(scopeSelectionCatalog(), nil, []string{"youtube"}, false)
	if err != nil {
		t.Fatalf("resolveLoginScopes: %v", err)
	}
	if len(got) != 1 || got[0] != "https://www.googleapis.com/auth/youtube.readonly" {
		t.Errorf("--service youtube scopes = %v; want only the youtube scope", got)
	}
}

// TestResolveLoginScopesAllUnion pins --all: the whole catalog union.
func TestResolveLoginScopesAllUnion(t *testing.T) {
	got, err := resolveLoginScopes(scopeSelectionCatalog(), nil, nil, true)
	if err != nil {
		t.Fatalf("resolveLoginScopes: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("--all scopes = %v; want both drive + youtube", got)
	}
}

// TestResolveLoginScopesEmbeddedCatalogNonEmpty pins that the real shipped
// catalog yields a non-empty, fully-qualified scope set so `gum login` with no
// flags is actually useful out of the box.
func TestResolveLoginScopesEmbeddedCatalogNonEmpty(t *testing.T) {
	got, err := resolveLoginScopes(loadCatalog(), nil, nil, false)
	if err != nil {
		t.Fatalf("resolveLoginScopes(embedded): %v", err)
	}
	if len(got) == 0 {
		t.Fatal("embedded catalog derived no scopes")
	}
	for _, s := range got {
		if !strings.HasPrefix(s, "https://") {
			t.Errorf("catalog scope %q is not a fully-qualified URL", s)
		}
	}
}

// TestResolveLoginScopesNilCatalogErrors pins the defensive guard: a missing
// catalog with no explicit --scope is an actionable error, not a panic.
func TestResolveLoginScopesNilCatalogErrors(t *testing.T) {
	if _, err := resolveLoginScopes(nil, nil, nil, false); err == nil {
		t.Fatal("nil catalog + no --scope = nil err; want error")
	}
}

// TestResolveLoginScopesEmptyCatalogErrors pins the scopeless-catalog guard.
func TestResolveLoginScopesEmptyCatalogErrors(t *testing.T) {
	if _, err := resolveLoginScopes(&catalog.Catalog{}, nil, nil, false); err == nil {
		t.Fatal("empty catalog + no --scope = nil err; want error")
	}
}

// TestRunLoginWithConfiguredClientRunsFlow pins the happy orchestration: a
// registered OAuth client + explicit scopes drives the login core (stubbed)
// and emits the non-secret credential JSON. The stub captures the scopes so we
// confirm the operator's --scope reached the loopback flow normalised.
func TestRunLoginWithConfiguredClientRunsFlow(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)
	if err := auth.StoreByoClient(auth.NewOSKeyring(), auth.DefaultAPIKeyProfile, auth.ByoClient{ClientID: "cid-1"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}

	var gotCfg auth.ByoOAuthConfig
	orig := interactiveByoLogin
	t.Cleanup(func() { interactiveByoLogin = orig })
	interactiveByoLogin = func(_ context.Context, cfg auth.ByoOAuthConfig, _ func(string) error) (*auth.Credentials, error) {
		gotCfg = cfg
		return &auth.Credentials{
			Token:        "access-xyz",
			StrategyName: "byo_oauth",
			Scopes:       cfg.Scopes,
			ExpiresAt:    time.Now().Add(time.Hour),
		}, nil
	}

	cmd := newAuthLoginCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--scope", "webmasters.readonly"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotCfg.ClientID != "cid-1" {
		t.Errorf("login cfg ClientID=%q; want cid-1", gotCfg.ClientID)
	}
	if len(gotCfg.Scopes) != 1 || gotCfg.Scopes[0] != "https://www.googleapis.com/auth/webmasters.readonly" {
		t.Errorf("login cfg Scopes=%v; want normalised webmasters.readonly", gotCfg.Scopes)
	}
	out := stdout.String()
	if !strings.Contains(out, `"strategy"`) || !strings.Contains(out, "byo_oauth") {
		t.Errorf("stdout missing credential JSON: %q", out)
	}
	if strings.Contains(out, "access-xyz") {
		t.Errorf("stdout leaked the access token: %q", out)
	}
}

// TestRunLoginIgnoresInjectedManagedClient pins the v1 auth posture: even when
// a build has managed OAuth values injected, `gum login` does not use them.
// The operator must register their own OAuth client first.
func TestRunLoginIgnoresInjectedManagedClient(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)
	withManagedClientEnv(t, "managed-id.apps.googleusercontent.com", "GOCSPX-managed-injected")

	orig := interactiveByoLogin
	t.Cleanup(func() { interactiveByoLogin = orig })
	interactiveByoLogin = func(_ context.Context, cfg auth.ByoOAuthConfig, _ func(string) error) (*auth.Credentials, error) {
		t.Fatalf("interactiveByoLogin called with cfg=%+v; want setup error before browser flow", cfg)
		return &auth.Credentials{
			Token:        "access-xyz",
			StrategyName: "byo_oauth",
			Scopes:       cfg.Scopes,
			ExpiresAt:    time.Now().Add(time.Hour),
		}, nil
	}

	cmd := newAuthLoginCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(nil) // no --scope
	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute nil error; want no OAuth client configured")
	}
	if !strings.Contains(err.Error(), "no OAuth client configured") {
		t.Fatalf("Execute err=%v; want no OAuth client configured", err)
	}
	if !strings.Contains(err.Error(), "--secret-stdin") {
		t.Fatalf("Execute err=%v; want Desktop client secret guidance", err)
	}
}

// TestManagedSupportedScopesIncludesWebmasters pins that the managed-supported
// scope set (what the built-in client may request by default) is non-empty and
// includes the Search Console read scope that motivated Path B.
func TestManagedSupportedScopesIncludesWebmasters(t *testing.T) {
	got, err := auth.ManagedSupportedScopes()
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
		t.Errorf("managed-supported scopes %v missing webmasters.readonly", got)
	}
}

// TestTopLevelLoginAliasRegistered pins that `gum login` exists at the root as
// a one-keystroke alias for `gum auth login`.
func TestTopLevelLoginAliasRegistered(t *testing.T) {
	root := newRootCmd()
	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "login" {
			found = true
			break
		}
	}
	if !found {
		t.Error("top-level `gum login` command not registered on root")
	}
}
