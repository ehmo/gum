// gum-6ne acceptance: Drive Tier A convenience tools.
// Spec §4.1 — three ops: drive.files.list (read, drive_find), drive.files.get
// (read, drive_get_file), drive.permissions.create (write w/ confirmation,
// drive_share). The convenience handlers in internal/mcp/tier_a_abi.go
// already point at these op_ids; this bead ships the catalog entries.

package main

import (
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// TestBuildDriveOpsShape pins op_id, variant_id, risk_class, scope, HTTP
// method, host prefix, go_pkg, go_call for every Drive Tier A op.
func TestBuildDriveOpsShape(t *testing.T) {
	want := []expectedWorkspaceOp{
		{
			OpID:       "drive.files.list",
			VariantID:  "drive.v3.rest.files.list",
			Service:    "drive",
			RiskClass:  catalog.RiskClassRead,
			Scope:      "https://www.googleapis.com/auth/drive.readonly",
			Method:     "GET",
			PathPrefix: "https://www.googleapis.com/drive/v3/files",
			GoPkg:      "google.golang.org/api/drive/v3",
			GoCall:     "Files.List",
		},
		{
			OpID:       "drive.files.get",
			VariantID:  "drive.v3.rest.files.get",
			Service:    "drive",
			RiskClass:  catalog.RiskClassRead,
			Scope:      "https://www.googleapis.com/auth/drive.readonly",
			Method:     "GET",
			PathPrefix: "https://www.googleapis.com/drive/v3/files/",
			GoPkg:      "google.golang.org/api/drive/v3",
			GoCall:     "Files.Get",
		},
		{
			OpID:       "drive.permissions.create",
			VariantID:  "drive.v3.rest.permissions.create",
			Service:    "drive",
			RiskClass:  catalog.RiskClassWrite,
			Scope:      "https://www.googleapis.com/auth/drive",
			Method:     "POST",
			PathPrefix: "https://www.googleapis.com/drive/v3/files/",
			GoPkg:      "google.golang.org/api/drive/v3",
			GoCall:     "Permissions.Create",
		},
	}
	got := BuildDriveOps()
	byID := map[string]catalog.Op{}
	for _, op := range got {
		byID[op.OpID] = op
	}
	// Full expected Drive op_id set — update when the surface grows.
	wantIDs := []string{
		"drive.files.list", "drive.files.get", "drive.files.create", "drive.files.update",
		"drive.files.copy", "drive.files.delete", "drive.files.export",
		"drive.permissions.list", "drive.permissions.get", "drive.permissions.create",
		"drive.permissions.update", "drive.permissions.delete",
		"drive.drives.list", "drive.drives.get", "drive.about.get",
	}
	if len(got) != len(wantIDs) {
		t.Fatalf("BuildDriveOps returned %d ops; want %d", len(got), len(wantIDs))
	}
	for _, id := range wantIDs {
		if _, ok := byID[id]; !ok {
			t.Errorf("missing Drive op %q", id)
		}
	}
	// Every Drive op is well-formed (the shared discovery-rest/typed-rest-sdk shape).
	for _, op := range got {
		if op.Service != "drive" || op.ServiceFamily != "workspace" {
			t.Errorf("%s: service=%q family=%q; want drive/workspace", op.OpID, op.Service, op.ServiceFamily)
		}
		if len(op.Variants) != 1 {
			t.Errorf("%s: variants=%d want 1", op.OpID, len(op.Variants))
			continue
		}
		v := op.Variants[0]
		if v.InterfaceKind != catalog.InterfaceKindDiscoveryREST || v.BackendKind != catalog.BackendKindTypedRestSDK ||
			v.AuthStrategy != catalog.AuthStrategyBYOOAuth || !v.Preferred {
			t.Errorf("%s: variant shape (iface=%q backend=%q auth=%q preferred=%v)", op.OpID, v.InterfaceKind, v.BackendKind, v.AuthStrategy, v.Preferred)
		}
		if v.Binding == nil || v.Binding.HTTP == nil || v.Binding.AdapterKey != "rest.typed-rest-sdk" {
			t.Errorf("%s: binding shape", op.OpID)
		}
		switch v.RiskClass {
		case catalog.RiskClassRead, catalog.RiskClassWrite, catalog.RiskClassDestructive:
		default:
			t.Errorf("%s: unexpected risk_class %q", op.OpID, v.RiskClass)
		}
	}
	// Detailed contract checks for the spec-pinned convenience-tool-backing ops.
	for _, w := range want {
		op, ok := byID[w.OpID]
		if !ok {
			t.Errorf("op %q missing from Drive Tier A set", w.OpID)
			continue
		}
		if op.Service != w.Service {
			t.Errorf("%s: service=%q want %q", w.OpID, op.Service, w.Service)
		}
		if op.ServiceFamily != "workspace" {
			t.Errorf("%s: service_family=%q want workspace", w.OpID, op.ServiceFamily)
		}
		if op.DefaultVariantID != w.VariantID {
			t.Errorf("%s: default_variant_id=%q want %q", w.OpID, op.DefaultVariantID, w.VariantID)
		}
		if len(op.Variants) != 1 {
			t.Errorf("%s: variants=%d want 1", w.OpID, len(op.Variants))
			continue
		}
		v := op.Variants[0]
		if v.RiskClass != w.RiskClass {
			t.Errorf("%s: risk_class=%q want %q", w.OpID, v.RiskClass, w.RiskClass)
		}
		if v.InterfaceKind != catalog.InterfaceKindDiscoveryREST {
			t.Errorf("%s: interface_kind=%q want discovery-rest", w.OpID, v.InterfaceKind)
		}
		if v.BackendKind != catalog.BackendKindTypedRestSDK {
			t.Errorf("%s: backend_kind=%q want typed-rest-sdk", w.OpID, v.BackendKind)
		}
		if v.AuthStrategy != catalog.AuthStrategyBYOOAuth {
			t.Errorf("%s: auth_strategy=%q want byo_oauth", w.OpID, v.AuthStrategy)
		}
		if !v.Preferred {
			t.Errorf("%s: preferred=false want true", w.OpID)
		}
		if len(v.Scopes) != 1 || v.Scopes[0] != w.Scope {
			t.Errorf("%s: scopes=%v want [%q]", w.OpID, v.Scopes, w.Scope)
		}
		if v.Binding == nil {
			t.Errorf("%s: binding is nil", w.OpID)
			continue
		}
		if v.Binding.AdapterKey != "rest.typed-rest-sdk" {
			t.Errorf("%s: adapter_key=%q want rest.typed-rest-sdk", w.OpID, v.Binding.AdapterKey)
		}
		if v.Binding.GoPkg != w.GoPkg {
			t.Errorf("%s: go_pkg=%q want %q", w.OpID, v.Binding.GoPkg, w.GoPkg)
		}
		if v.Binding.GoCall != w.GoCall {
			t.Errorf("%s: go_call=%q want %q", w.OpID, v.Binding.GoCall, w.GoCall)
		}
		if v.Binding.HTTP == nil {
			t.Errorf("%s: binding.http is nil", w.OpID)
			continue
		}
		if v.Binding.HTTP.Method != w.Method {
			t.Errorf("%s: http.method=%q want %q", w.OpID, v.Binding.HTTP.Method, w.Method)
		}
		if got := v.Binding.HTTP.Path; len(got) < len(w.PathPrefix) || got[:len(w.PathPrefix)] != w.PathPrefix {
			t.Errorf("%s: http.path=%q does not start with %q", w.OpID, got, w.PathPrefix)
		}
	}
}

// TestBuildDriveOpsValidates ensures the Drive-only catalog passes
// catalog.Validate.
func TestBuildDriveOpsValidates(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops:                  BuildDriveOps(),
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("validate Drive-only catalog: %v", err)
	}
}

// TestBuildDriveOpsCoverConvenienceABI proves every drive_* convenience tool
// in internal/mcp/tier_a_abi.go now has a backing catalog op.
func TestBuildDriveOpsCoverConvenienceABI(t *testing.T) {
	want := map[string]bool{
		"drive.files.list":         false,
		"drive.files.get":          false,
		"drive.permissions.create": false,
	}
	for _, op := range BuildDriveOps() {
		if _, ok := want[op.OpID]; ok {
			want[op.OpID] = true
		}
	}
	for id, present := range want {
		if !present {
			t.Errorf("convenience tool target %q has no catalog op; convenience handler will surface OP_NOT_FOUND", id)
		}
	}
}

// TestBuildDriveOpsRejectsGUMOAuth pins that no Drive variant uses the
// v0.1.0-disabled gum_oauth strategy (bd memory gum-auth-strategy-v3).
func TestBuildDriveOpsRejectsGUMOAuth(t *testing.T) {
	for _, op := range BuildDriveOps() {
		for _, v := range op.Variants {
			if v.AuthStrategy == catalog.AuthStrategyGUMOAuth {
				t.Errorf("op %s variant %s: gum_oauth disabled in v0.1.0", op.OpID, v.VariantID)
			}
		}
	}
}
