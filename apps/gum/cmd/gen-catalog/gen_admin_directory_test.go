// gum-fmi acceptance: long-tail raw-http surface for the Admin SDK
// Directory API. Each op must be (read-class + raw-http) so the kernel's
// spec §5.7 read-only allowlist escape hatch can fire.

package main

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestBuildAdminDirectoryOpsShape pins backend_kind=raw-http, risk=read,
// and the canonical HTTP path for every op in the §5.7 long-tail surface.
//
// A regression in backend_kind would break the §5.7 allowlist gate
// (applyReadOnlyAllowlist requires raw-http or discovery-rest); a
// regression in risk_class would force callers through the gum.write tool.
func TestBuildAdminDirectoryOpsShape(t *testing.T) {
	want := []struct {
		opID       string
		httpMethod string
		httpPath   string
	}{
		{"admin.directory.users.list", "GET", "/admin/directory/v1/users"},
		{"admin.directory.users.get", "GET", "/admin/directory/v1/users/{userKey}"},
		{"admin.directory.groups.list", "GET", "/admin/directory/v1/groups"},
	}

	got := BuildAdminDirectoryOps()
	byID := make(map[string]catalog.Op, len(got))
	for _, op := range got {
		byID[op.OpID] = op
	}

	for _, w := range want {
		op, ok := byID[w.opID]
		if !ok {
			t.Errorf("op %q missing from Admin Directory long-tail set", w.opID)
			continue
		}
		if op.Service != "admin" {
			t.Errorf("%s: service=%q want admin", w.opID, op.Service)
		}
		if op.ServiceFamily != "workspace" {
			t.Errorf("%s: service_family=%q want workspace", w.opID, op.ServiceFamily)
		}
		if len(op.Variants) != 1 {
			t.Errorf("%s: variants=%d want 1", w.opID, len(op.Variants))
			continue
		}
		v := op.Variants[0]
		if v.RiskClass != catalog.RiskClassRead {
			t.Errorf("%s: risk_class=%q want read (§5.7 allowlist requires read)", w.opID, v.RiskClass)
		}
		if v.BackendKind != catalog.BackendKindRawHTTP {
			t.Errorf("%s: backend_kind=%q want raw-http (§5.7 allowlist requires raw-http)", w.opID, v.BackendKind)
		}
		if v.AuthStrategy != catalog.AuthStrategyBYOOAuth {
			t.Errorf("%s: auth_strategy=%q want byo_oauth", w.opID, v.AuthStrategy)
		}
		if v.Binding == nil {
			t.Errorf("%s: nil binding", w.opID)
			continue
		}
		if v.Binding.AdapterKey != "rest.raw-http" {
			t.Errorf("%s: adapter_key=%q want rest.raw-http", w.opID, v.Binding.AdapterKey)
		}
		if v.Binding.HTTP == nil {
			t.Errorf("%s: nil http binding", w.opID)
			continue
		}
		if v.Binding.HTTP.Method != w.httpMethod {
			t.Errorf("%s: method=%q want %q", w.opID, v.Binding.HTTP.Method, w.httpMethod)
		}
		if v.Binding.HTTP.Path != w.httpPath {
			t.Errorf("%s: path=%q want %q", w.opID, v.Binding.HTTP.Path, w.httpPath)
		}
		if len(v.Scopes) == 0 {
			t.Errorf("%s: empty scopes (need at least one admin.directory.* scope)", w.opID)
		}
	}
}

func TestAdminDirectoryWriteOpsCarryFixturePolicy(t *testing.T) {
	for _, op := range BuildAdminDirectoryOps() {
		if op.Service != "admin" {
			continue
		}
		v := op.Variants[0]
		if v.RiskClass == catalog.RiskClassRead {
			if v.AdminPolicy != nil {
				t.Errorf("%s: read variant unexpectedly has admin_policy", op.OpID)
			}
			continue
		}
		if v.AdminPolicy == nil {
			t.Fatalf("%s: Admin write/destructive variant missing admin_policy", op.OpID)
		}
		if v.AdminPolicy.BlastRadius != catalog.AdminBlastRadiusFixtureWrite {
			t.Errorf("%s: blast_radius=%q want %q", op.OpID, v.AdminPolicy.BlastRadius, catalog.AdminBlastRadiusFixtureWrite)
		}
		if !v.AdminPolicy.FixtureOwnershipRequired {
			t.Errorf("%s: fixture_ownership_required=false", op.OpID)
		}
		if v.AdminPolicy.FixtureMarkerPrefix != catalog.AdminFixtureMarkerPrefix {
			t.Errorf("%s: fixture_marker_prefix=%q want %q", op.OpID, v.AdminPolicy.FixtureMarkerPrefix, catalog.AdminFixtureMarkerPrefix)
		}
		if len(v.AdminPolicy.FixtureResourceKeys) == 0 {
			t.Errorf("%s: fixture_resource_keys empty", op.OpID)
		}
	}
}

func TestAdminHighBlastRadiusOpsAreExplicitlyExcluded(t *testing.T) {
	if len(adminHighBlastRadiusExcludedOps) == 0 {
		t.Fatal("adminHighBlastRadiusExcludedOps is empty")
	}
	for opID, blast := range adminHighBlastRadiusExcludedOps {
		if blast != catalog.AdminBlastRadiusHighBlast {
			t.Errorf("%s: blast=%q want %q", opID, blast, catalog.AdminBlastRadiusHighBlast)
		}
		for _, op := range BuildAdminDirectoryOps() {
			if op.OpID == opID {
				t.Fatalf("high-blast Admin op %s was emitted by BuildAdminDirectoryOps", opID)
			}
		}
	}
}
