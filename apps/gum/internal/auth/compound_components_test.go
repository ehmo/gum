// Compound-auth missing_components taxonomy (spec §7, test-matrix.md row 89).
//
// Row 89 requires a Google Ads Keyword Planner-like fixture whose compound
// operation names its real prerequisites: developer token, OAuth client and
// client secret, the consented scope behind the refresh token, customer id,
// the optional manager login customer id, billing and account prerequisites,
// an account-permission check, and the approved-access allowlist hint. Before
// this test the resolver answered every compound variant with a single
// "see_setup_command" marker, so only "non-empty" was provable.

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// loadCompoundCatalog returns the Keyword Planner fixture catalog. It
// validates first, so a fixture that could never ship cannot prop up the
// envelope assertions. Callers that mutate it re-run Validate themselves.
func loadCompoundCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	data, err := os.ReadFile("testdata/compound-auth-keyword-planner.json")
	if err != nil {
		t.Fatalf("read compound fixture: %v", err)
	}
	var c catalog.Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("unmarshal compound fixture: %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("compound fixture fails catalog validation: %v", err)
	}
	return &c
}

// loadCompoundFixture returns the Keyword Planner variant from that catalog.
func loadCompoundFixture(t *testing.T) *catalog.Variant {
	t.Helper()
	return &loadCompoundCatalog(t).Ops[0].Variants[0]
}

func TestCompoundAuthReportsRealMissingComponents(t *testing.T) {
	v := loadCompoundFixture(t)
	r := &CompositeResolver{}
	_, err := r.ResolveAuth(context.Background(),
		&dispatch.Invocation{OpID: "ads.keywordplanner.generate"},
		&dispatch.ResolvedVariant{Variant: v})

	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("ResolveAuth = %T (%v); want *AuthError", err, err)
	}

	want := []string{
		"developer_token",
		"oauth_client",
		"oauth_client_secret",
		"oauth_scopes",
		"customer_id",
		"login_customer_id",
		"billing_enabled",
		"manager_account",
		"account_permission",
		"service_allowlist",
	}
	if !slices.Equal(ae.MissingComponents, want) {
		t.Errorf("missing_components = %v;\n                 want %v", ae.MissingComponents, want)
	}
	if slices.Contains(ae.MissingComponents, "see_setup_command") {
		t.Error("missing_components still carries the see_setup_command marker; the declared taxonomy must replace it")
	}
	if ae.SetupCommand != "gum auth setup ads.keywordplanner.generate" {
		t.Errorf("setup_command = %q; want the per-op setup form", ae.SetupCommand)
	}
	if strings.Contains(ae.UserMessage+ae.HumanRemediation, "gum auth login") {
		t.Error("compound remediation suggests plain `gum auth login`; spec §7 forbids implying browser OAuth alone fixes it")
	}
}

// TestCompoundMissingComponentsFallsBackWhenUndeclared keeps the spec §7
// "MUST include missing_components" floor for a compound variant that
// declares none. The envelope must still be actionable.
func TestCompoundMissingComponentsFallsBackWhenUndeclared(t *testing.T) {
	r := &CompositeResolver{}
	_, err := r.ResolveAuth(context.Background(),
		&dispatch.Invocation{OpID: "ads.keywordplanner.generate"},
		&dispatch.ResolvedVariant{Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyCompound}})

	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("ResolveAuth = %T (%v); want *AuthError", err, err)
	}
	if !slices.Equal(ae.MissingComponents, []string{"see_setup_command"}) {
		t.Errorf("missing_components = %v; want [see_setup_command] for an undeclared compound variant", ae.MissingComponents)
	}
}

// TestCompoundComponentsDeduplicateAndKeepOrder pins the two properties the
// envelope reader depends on: a repeated kind appears once, and the order is
// the catalog's, so `gum auth setup` walks the steps as the curator wrote them.
func TestCompoundComponentsDeduplicateAndKeepOrder(t *testing.T) {
	v := &catalog.Variant{
		AuthStrategy: catalog.AuthStrategyCompound,
		AuthComponents: []catalog.AuthComponent{
			{Kind: catalog.AuthComponentDeveloperToken},
			{Kind: catalog.AuthComponentCustomerID},
			{Kind: catalog.AuthComponentDeveloperToken},
			{Kind: catalog.AuthComponentKind("x-ads-permissible-use"), External: true},
		},
	}
	got := compoundMissingComponents(&dispatch.ResolvedVariant{Variant: v})
	want := []string{"developer_token", "customer_id", "x-ads-permissible-use"}
	if !slices.Equal(got, want) {
		t.Errorf("compoundMissingComponents = %v; want %v", got, want)
	}
}

// TestCompoundComponentsCarryNoSecretValues pins the catalog-ABI rule that
// auth_components hold descriptors and hints only. A hint that leaked a value
// would reach an LLM through the failure envelope.
func TestCompoundComponentsCarryNoSecretValues(t *testing.T) {
	v := loadCompoundFixture(t)
	for _, comp := range v.AuthComponents {
		if comp.SetupHint == "" {
			t.Errorf("component %q has no setup_hint; the envelope reader has nothing to act on", comp.Kind)
		}
		for _, leak := range []string{"ya29.", "1//0", "AIza", "-----BEGIN"} {
			if strings.Contains(comp.SetupHint, leak) {
				t.Errorf("component %q setup_hint contains credential-shaped text %q", comp.Kind, leak)
			}
		}
	}
	if !v.AuthComponents[0].Secret {
		t.Error("developer_token is not marked secret; gum auth setup would not route it to the keychain")
	}
}
