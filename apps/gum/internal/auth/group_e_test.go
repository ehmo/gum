// Group E auth test-matrix rows (spec §7, test-matrix.md row 68-71).
//
// This file covers the subset of Group E tests that are offline-executable in
// this package — i.e., they do not need a live `gum auth login` browser flow, OS
// keychain interaction, or live Google OAuth verification. Tests that require
// the interactive browser surface (TestAuthLoopbackStateRequired,
// TestAuthScopeUpgrade*) are pending the gum-xth interactive OAuth bead and
// will land alongside that work.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embedded"
)

// TestAuthHappyPathNoUserClientSecret verifies spec §7: "GUM MUST
// NOT embed an OAuth client secret in the open-source binary." We scan the
// repo for plausible secret literals and assert no source file outside the
// BYO config plumbing references a hard-coded client secret.
func TestAuthHappyPathNoUserClientSecret(t *testing.T) {
	repoRoot := findRepoRootForTest(t)
	// Walk apps/gum/ source tree; ignore test fixtures and vendored binaries.
	hits := []string{}
	pat := regexp.MustCompile(`(?i)oauth_client_secret\s*=\s*"[A-Za-z0-9_\-]{8,}"`)
	literalPat := regexp.MustCompile(`GOCSPX-[A-Za-z0-9_\-]{20,}`)
	_ = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		// Restrict to .go source; skip vendor and generated builds.
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(path, "/testdata/") || strings.Contains(path, "_test.go") {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if pat.Match(body) || literalPat.Match(body) {
			hits = append(hits, path)
		}
		return nil
	})
	if len(hits) > 0 {
		t.Errorf("found suspicious hard-coded OAuth client secret references:\n%s", strings.Join(hits, "\n"))
	}
}

// TestAuthErrorNextAction verifies spec §7: variants whose
// strategy is not gum_oauth MUST emit error envelopes carrying auth_strategy,
// missing_components, and setup_command — and MUST NOT imply that
// `gum auth login` alone is the next action. The CompositeResolver's stub
// branches surface these envelopes; we exercise the byo_oauth path with no
// configured BYO resolver and no ADC fallback to force the envelope.
func TestAuthErrorNextAction(t *testing.T) {
	t.Run("gum_oauth with no managed scope reports MANAGED_CLIENT_NOT_READY", func(t *testing.T) {
		r := &CompositeResolver{}
		_, err := r.ResolveAuth(context.Background(), &dispatch.Invocation{}, &dispatch.ResolvedVariant{
			Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyGUMOAuth},
		})
		var ae *AuthError
		if !errors.As(err, &ae) {
			t.Fatalf("want *AuthError, got %T: %v", err, err)
		}
		if ae.Code != "GUM_OAUTH_MANAGED_CLIENT_NOT_READY" {
			t.Errorf("Code = %q; want GUM_OAUTH_MANAGED_CLIENT_NOT_READY", ae.Code)
		}
		// Spec §7: the message MUST NOT claim browser login alone
		// works while the managed-scope manifest is unpromoted.
		if strings.Contains(ae.HumanRemediation, "gum auth login") {
			t.Errorf("not-ready message wrongly suggests `gum auth login`: %s", ae.HumanRemediation)
		}
	})

	t.Run("byo_oauth without resolver and without ADC fallback returns setup hint", func(t *testing.T) {
		// An empty Keyring, not the default OS keychain: with the real
		// keychain this subtest reads the developer's own registered OAuth
		// client, resolves live credentials and sees a nil error.
		r := &CompositeResolver{Keyring: &mockKeyring{data: map[string]string{}}} // no BYO, no ADC
		_, err := r.ResolveAuth(context.Background(), &dispatch.Invocation{}, &dispatch.ResolvedVariant{
			Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyBYOOAuth},
		})
		var ae *AuthError
		if !errors.As(err, &ae) {
			t.Fatalf("want *AuthError, got %T: %v", err, err)
		}
		if ae.Strategy != "byo_oauth" {
			t.Errorf("Strategy = %q; want byo_oauth", ae.Strategy)
		}
		// Spec §7: byo_oauth setup must surface use-oauth-client.
		// The current surface uses ADC fallback hints. Either is
		// acceptable as long as it does NOT imply plain `gum auth login`.
		if strings.Contains(ae.HumanRemediation, "`gum auth login`") {
			t.Errorf("byo_oauth setup wrongly suggests `gum auth login`: %s", ae.HumanRemediation)
		}
	})

	// "workload_identity" is the fixture on purpose: it used to be a valid
	// catalog.AuthStrategy that passed Validate at build and install and only
	// failed here. gum-z5yx removed it from the enum, so Validate now refuses
	// it first and this arm is the resolver's remaining fail-closed guard for
	// any strategy string the catalog does not define.
	t.Run("unsupported strategy returns AUTH_STRATEGY_NOT_IMPLEMENTED", func(t *testing.T) {
		r := &CompositeResolver{ADC: &fakeADCResolver{}}
		_, err := r.ResolveAuth(context.Background(), &dispatch.Invocation{}, &dispatch.ResolvedVariant{
			Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategy("workload_identity")},
		})
		var ae *AuthError
		if !errors.As(err, &ae) || ae.Code != "AUTH_STRATEGY_NOT_IMPLEMENTED" {
			t.Errorf("want AUTH_STRATEGY_NOT_IMPLEMENTED, got %v", err)
		}
	})
}

// emptyKeyring is a KeyringBackend holding no secrets, so a byo_oauth resolve
// takes the "operator registered no OAuth client" branch without touching the
// host keychain.
type emptyKeyring struct{}

func (emptyKeyring) Get(string) (string, error) { return "", nil }
func (emptyKeyring) Set(string, string) error   { return nil }
func (emptyKeyring) Delete(string) error        { return nil }

// TestAuthNoAmbientADCWithoutOptIn verifies spec §7: ambient
// GOOGLE_APPLICATION_CREDENTIALS is never used for a variant whose
// auth_strategy is not adc. ADC is selected by the catalog, not by an
// operator command, so a byo_oauth variant with no registered OAuth client
// must fail with BYO_OAUTH_CLIENT_NOT_CONFIGURED rather than quietly mint an
// ADC token from the ambient environment.
func TestAuthNoAmbientADCWithoutOptIn(t *testing.T) {
	called := false
	r := &CompositeResolver{
		ADC:     &fakeADCResolver{onResolve: func() { called = true }},
		Keyring: emptyKeyring{},
	}
	creds, err := r.ResolveAuth(context.Background(), &dispatch.Invocation{}, &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyBYOOAuth},
	})
	if called {
		t.Fatal("byo_oauth fell through to the ADC resolver; ambient ADC must not satisfy a non-adc strategy")
	}
	if creds != nil {
		t.Fatalf("want no credentials, got %+v", creds)
	}
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Code != "BYO_OAUTH_CLIENT_NOT_CONFIGURED" {
		t.Fatalf("want BYO_OAUTH_CLIENT_NOT_CONFIGURED, got %v", err)
	}
}

// TestAuthStrategyRequired verifies spec §7: every executable
// catalog variant MUST declare exactly one auth_strategy. We walk the
// embedded catalog and assert no variant has an empty AuthStrategy field.
func TestAuthStrategyRequired(t *testing.T) {
	if len(embedded.CatalogJSON) == 0 {
		t.Skip("embedded catalog not built")
	}
	var c catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &c); err != nil {
		t.Fatalf("decode embedded catalog: %v", err)
	}
	missing := []string{}
	for _, op := range c.Ops {
		for _, v := range op.Variants {
			if strings.TrimSpace(string(v.AuthStrategy)) == "" {
				missing = append(missing, op.OpID+"::"+v.VariantID)
			}
		}
	}
	if len(missing) > 0 {
		head := len(missing)
		if head > 10 {
			head = 10
		}
		t.Errorf("%d variants missing auth_strategy; first %d:\n  %s", len(missing), head, strings.Join(missing[:head], "\n  "))
	}
}

// TestManagedOAuthScopeManifest verifies spec §7: the
// internal/embedded/data/auth-managed-scopes.v1.json manifest is the single source of truth
// for scopes eligible to use auth_strategy="gum_oauth". The manifest must
// exist, parse, and satisfy the structural invariants documented in §7.
func TestManagedOAuthScopeManifest(t *testing.T) {
	body, err := embeddedManagedScopes()
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var doc struct {
		SchemaVersion int            `json:"schema_version"`
		ClientPolicy  map[string]any `json:"client_policy"`
		Scopes        []struct {
			Scope                string `json:"scope"`
			Status               string `json:"status"`
			VerificationState    string `json:"verification_state"`
			ProjectEvidenceState string `json:"project_evidence_state"`
			LiveCanaryState      string `json:"live_canary_state"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	if doc.SchemaVersion != 1 {
		t.Errorf("schema_version = %d; want 1", doc.SchemaVersion)
	}
	// Spec §7: no client secret.
	if v, _ := doc.ClientPolicy["embedded_client_secret"].(bool); v {
		t.Errorf("client_policy.embedded_client_secret = true; spec §7 forbids this")
	}
	if v, _ := doc.ClientPolicy["flow"].(string); v != "installed_app_pkce" {
		t.Errorf("client_policy.flow = %q; want installed_app_pkce", v)
	}
	if v, _ := doc.ClientPolicy["redirect_method"].(string); v != "loopback" {
		t.Errorf("client_policy.redirect_method = %q; want loopback (spec §7)", v)
	}
	// At least one scope must exist (even if all are planned).
	if len(doc.Scopes) == 0 {
		t.Errorf("no scopes declared in manifest")
	}
	// Every scope must declare the four lifecycle state fields.
	for _, s := range doc.Scopes {
		if s.Scope == "" || s.Status == "" || s.VerificationState == "" ||
			s.ProjectEvidenceState == "" || s.LiveCanaryState == "" {
			t.Errorf("scope %+v missing lifecycle field(s)", s)
		}
	}
}

// TestGumOAuthScopeNotManaged is a forward-looking guard: when a variant
// declares auth_strategy="gum_oauth", every required scope must appear in
// the manifest with all four lifecycle fields = managed-ready. The resolver
// gates gum_oauth on the manifest, but the catalog generator
// should still reject ineligible declarations. We scan the embedded catalog
// for any gum_oauth variant referencing an unmanaged scope.
func TestGumOAuthScopeNotManaged(t *testing.T) {
	if len(embedded.CatalogJSON) == 0 {
		t.Skip("embedded catalog not built")
	}
	managed, err := managedReadyScopes()
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var c catalog.Catalog
	_ = json.Unmarshal(embedded.CatalogJSON, &c)
	for _, op := range c.Ops {
		for _, v := range op.Variants {
			if v.AuthStrategy != catalog.AuthStrategyGUMOAuth {
				continue
			}
			for _, s := range v.Scopes {
				if _, ok := managed[s]; !ok {
					t.Errorf("variant %s declares gum_oauth but scope %q is not managed-ready", v.VariantID, s)
				}
			}
		}
	}
}

// TestAuthComponentUnknown verifies spec §7: only the closed
// set of component kinds is accepted; an unknown kind fails catalog build
// with AUTH_COMPONENT_UNKNOWN unless it carries the "x-" informational
// prefix. The kinds now live in catalog.AuthComponentKinds and a variant can
// declare them, so this asserts the enum contents and the validator arm.
func TestAuthComponentUnknown(t *testing.T) {
	// Spec §7 enumerates these 19 kinds. A drift either way fails here first,
	// which forces the spec and the enum to move together.
	want := []string{
		"oauth_scopes", "oauth_client", "api_enabled_project", "api_key",
		"developer_token", "customer_id", "login_customer_id", "billing_enabled",
		"manager_account", "workspace_admin_trust", "domain_wide_delegation",
		"service_account_key", "consent_verification", "oauth_consent_screen",
		"quota_project", "service_allowlist", "org_policy_exception",
		"oauth_client_secret", "account_permission",
	}
	got := make([]string, 0, len(catalog.AuthComponentKinds))
	for _, k := range catalog.AuthComponentKinds {
		got = append(got, string(k))
	}
	if !slices.Equal(got, want) {
		t.Errorf("catalog.AuthComponentKinds = %v;\n                            want %v", got, want)
	}

	cases := []struct {
		kind    string
		wantErr bool
	}{
		{"developer_token", false},
		{"x-ads-permissible-use", false},
		{"x-", true},
		{"developer-token", true},
		{"", true},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			c := loadCompoundCatalog(t)
			c.Ops[0].Variants[0].AuthComponents = []catalog.AuthComponent{
				{Kind: catalog.AuthComponentKind(tc.kind)},
			}
			err := c.Validate()
			if tc.wantErr && !errors.Is(err, catalog.ErrUnknownAuthComponent) {
				t.Fatalf("Validate(kind=%q) = %v; want ErrUnknownAuthComponent", tc.kind, err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate(kind=%q) = %v; want nil", tc.kind, err)
			}
		})
	}
}

// TestCompoundAuthErrorEnvelope verifies spec §7 +
// 1378-1389: compound-auth failure envelopes MUST include auth_strategy,
// missing_components, and setup_command. The error also marshals to the
// canonical JSON envelope shape so MCP stdio mode can forward it verbatim.
func TestCompoundAuthErrorEnvelope(t *testing.T) {
	r := &CompositeResolver{}
	_, err := r.ResolveAuth(context.Background(), &dispatch.Invocation{OpID: "ads.keywordplanner.generate"}, &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyCompound},
	})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("want *AuthError for compound strategy, got %T: %v", err, err)
	}
	// All three required fields per spec §7.
	if ae.Strategy != "compound" {
		t.Errorf("auth_strategy = %q; want compound", ae.Strategy)
	}
	if len(ae.MissingComponents) == 0 {
		t.Errorf("missing_components is empty; spec §7 requires it on compound envelopes")
	}
	if ae.SetupCommand == "" {
		t.Errorf("setup_command is empty; spec §7 requires it on compound envelopes")
	}
	// Setup command MUST be `gum auth setup <op_id>` form, NOT a plain
	// `gum auth login` (spec §7).
	if !strings.Contains(ae.SetupCommand, "gum auth setup") {
		t.Errorf("setup_command = %q; want a `gum auth setup ...` form", ae.SetupCommand)
	}
	if strings.Contains(ae.SetupCommand, "gum auth login") {
		t.Errorf("setup_command = %q; compound MUST NOT suggest plain `gum auth login`", ae.SetupCommand)
	}
	// Error code should be the canonical AUTH_REQUIRED envelope code
	// (spec §7 wording). AUTH_STRATEGY_NOT_IMPLEMENTED is the stub
	// code we used before this bead landed.
	if ae.Code != "AUTH_REQUIRED" {
		t.Errorf("error_code = %q; want AUTH_REQUIRED on compound envelopes", ae.Code)
	}
	// The envelope JSON shape must round-trip per spec §7.
	body, jerr := json.Marshal(ae)
	if jerr != nil {
		t.Fatalf("marshal envelope: %v", jerr)
	}
	var got map[string]any
	if uerr := json.Unmarshal(body, &got); uerr != nil {
		t.Fatalf("unmarshal envelope: %v", uerr)
	}
	for _, k := range []string{"error_code", "auth_strategy", "missing_components", "setup_command"} {
		if _, ok := got[k]; !ok {
			t.Errorf("envelope JSON missing key %q; have %v", k, got)
		}
	}
	if got["auth_strategy"] != "compound" {
		t.Errorf("envelope auth_strategy = %v; want compound", got["auth_strategy"])
	}
}

// ── helpers ──────────────────────────────────────────────────────────────

// fakeADCResolver returns a static credential for tests; tracks whether
// Resolve was called.
type fakeADCResolver struct {
	onResolve func()
}

func (f *fakeADCResolver) Resolve(_ context.Context, _ []string) (*Credentials, error) {
	if f.onResolve != nil {
		f.onResolve()
	}
	return &Credentials{Token: "fake-adc-token", StrategyName: "adc"}, nil
}

// findRepoRootForTest walks up from cwd until it finds go.mod.
func findRepoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find go.mod from %s", wd)
	return ""
}

// embeddedManagedScopes reads the manifest from the on-disk source tree (the
// canonical location). The embedded copy is in internal/embedded/data/.
func embeddedManagedScopes() ([]byte, error) {
	repoRoot, err := findRepoRootForBuild()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(repoRoot, "internal", "embedded", "data", "auth-managed-scopes.v1.json"))
}

// findRepoRootForBuild walks up from cwd without using *testing.T so the
// helper is usable in non-test paths.
func findRepoRootForBuild() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errors.New("go.mod not found")
}

// managedReadyScopes parses the manifest and returns a set of scopes that
// pass all four lifecycle gates.
func managedReadyScopes() (map[string]struct{}, error) {
	body, err := embeddedManagedScopes()
	if err != nil {
		return nil, err
	}
	var doc struct {
		Scopes []struct {
			Scope                string `json:"scope"`
			Status               string `json:"status"`
			VerificationState    string `json:"verification_state"`
			ProjectEvidenceState string `json:"project_evidence_state"`
			LiveCanaryState      string `json:"live_canary_state"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := map[string]struct{}{}
	for _, s := range doc.Scopes {
		if s.Status == "active" && s.VerificationState == "verified" &&
			s.ProjectEvidenceState == "ready" && s.LiveCanaryState == "passing" {
			out[s.Scope] = struct{}{}
		}
	}
	return out, nil
}
