package auth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embedded"
)

// Matrix row 96. Spec §7 requires that a Workspace or org-policy refusal be
// actionable: the caller learns which prerequisite is missing and that no
// amount of re-logging-in will fix it.
//
// What ships is the PRE-FLIGHT half. A compound-auth variant declares its
// prerequisites as catalog auth_components; CompositeResolver.ResolveAuth
// refuses before the call and names them in missing_components, with
// retryable=false and a setup_command. This test pins that envelope for the
// two Workspace kinds: workspace_admin_trust and org_policy_exception.
//
// The RESPONSE half ships too, in internal/adapters: a 403 whose Google
// reason names a policy gap carries the same component out through
// UpstreamError.AsStructuredError. Its tests live next to that code, in
// internal/adapters/policy_refusal_test.go and
// internal/dispatch/policy_refusal_boundary_test.go, because they drive a
// real upstream response rather than the resolver.
func TestWorkspaceAdminPolicyAuthEnvelope(t *testing.T) {
	t.Run("declared Workspace components reach missing_components", func(t *testing.T) {
		for _, kind := range []catalog.AuthComponentKind{
			catalog.AuthComponentWorkspaceAdminTrust,
			catalog.AuthComponentOrgPolicyException,
		} {
			err := resolveCompound(t, "admin.users.delete", catalog.AuthComponent{Kind: kind})
			if got := err.MissingComponents; len(got) != 1 || got[0] != string(kind) {
				t.Fatalf("missing_components=%v; want exactly [%s]", got, kind)
			}
			if err.Code != "AUTH_REQUIRED" {
				t.Errorf("error_code=%q; want AUTH_REQUIRED", err.Code)
			}
			if err.Retryable {
				t.Errorf("%s envelope is retryable; a retry-login loop cannot grant admin trust", kind)
			}
			if err.SetupCommand != "gum auth setup admin.users.delete" {
				t.Errorf("setup_command=%q; want the op-scoped setup command", err.SetupCommand)
			}
			if err.OpID != "admin.users.delete" {
				t.Errorf("op_id=%q; want admin.users.delete", err.OpID)
			}
		}
	})

	t.Run("an optional component is still reported", func(t *testing.T) {
		// Nothing resolves components per account, so hiding an optional one
		// would hide a step the operator still has to decide about.
		err := resolveCompound(t, "admin.users.delete",
			catalog.AuthComponent{Kind: catalog.AuthComponentWorkspaceAdminTrust},
			catalog.AuthComponent{Kind: catalog.AuthComponentOrgPolicyException, Optional: true},
		)
		want := []string{"workspace_admin_trust", "org_policy_exception"}
		if !equalStrings(err.MissingComponents, want) {
			t.Fatalf("missing_components=%v; want %v in catalog order", err.MissingComponents, want)
		}
	})

	t.Run("the envelope survives JSON marshalling", func(t *testing.T) {
		err := resolveCompound(t, "admin.groups.delete",
			catalog.AuthComponent{Kind: catalog.AuthComponentWorkspaceAdminTrust})
		raw, mErr := json.Marshal(err)
		if mErr != nil {
			t.Fatalf("Marshal: %v", mErr)
		}
		var env map[string]any
		if uErr := json.Unmarshal(raw, &env); uErr != nil {
			t.Fatalf("Unmarshal: %v", uErr)
		}
		comps, ok := env["missing_components"].([]any)
		if !ok || len(comps) != 1 || comps[0] != "workspace_admin_trust" {
			t.Fatalf("wire missing_components=%v; want [workspace_admin_trust]", env["missing_components"])
		}
		if _, present := env["retryable"]; present {
			t.Errorf("retryable is on the wire; omitempty should drop the false value")
		}
		if !strings.Contains(env["setup_command"].(string), "admin.groups.delete") {
			t.Errorf("setup_command=%v; want the op id", env["setup_command"])
		}
	})

	t.Run("a variant declaring no components falls back to the setup marker", func(t *testing.T) {
		err := resolveCompound(t, "admin.users.delete")
		if !equalStrings(err.MissingComponents, []string{"see_setup_command"}) {
			t.Fatalf("missing_components=%v; want [see_setup_command]", err.MissingComponents)
		}
	})

	t.Run("no shipped variant pre-declares a Workspace block", func(t *testing.T) {
		// Deliberate, not a gap (gum-ixky). A catalog auth_component is a
		// static claim about every caller of the op. Whether a Workspace
		// administrator trusts this client varies per domain, so declaring
		// workspace_admin_trust on the Admin SDK ops would tell every
		// unblocked administrator that something is missing when nothing
		// is. The response path reports the block only where it is true.
		//
		// The declaration would also be inert: auth_components is read only
		// by CompositeResolver, all 14 shipped Admin SDK variants are
		// byo_oauth, and `gum auth setup` does not read the catalog.
		//
		// A curator who lands genuine compound variants should replace this
		// count with an assertion about those specific ops.
		var cat catalog.Catalog
		if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
			t.Fatalf("unmarshal embedded catalog: %v", err)
		}
		declared := 0
		for _, op := range cat.Ops {
			for _, v := range op.Variants {
				for _, comp := range v.AuthComponents {
					if comp.Kind == catalog.AuthComponentWorkspaceAdminTrust ||
						comp.Kind == catalog.AuthComponentOrgPolicyException {
						declared++
					}
				}
			}
		}
		if declared != 0 {
			t.Fatalf("%d shipped variants declare a Workspace component; a static declaration must name the ops it is true for", declared)
		}
	})
}

// resolveCompound runs the real CompositeResolver against a compound variant
// carrying the given components and returns the refusal envelope.
func resolveCompound(t *testing.T, opID string, comps ...catalog.AuthComponent) *AuthError {
	t.Helper()
	c := &CompositeResolver{}
	_, err := c.ResolveAuth(context.Background(),
		&dispatch.Invocation{OpID: opID},
		&dispatch.ResolvedVariant{
			OpID: opID,
			Variant: &catalog.Variant{
				VariantID:      opID + ".v1",
				AuthStrategy:   "compound",
				AuthComponents: comps,
			},
		})
	if err == nil {
		t.Fatal("compound variant resolved credentials; want an AUTH_REQUIRED refusal")
	}
	authErr, ok := err.(*AuthError)
	if !ok {
		t.Fatalf("err is %T; want *AuthError", err)
	}
	return authErr
}
