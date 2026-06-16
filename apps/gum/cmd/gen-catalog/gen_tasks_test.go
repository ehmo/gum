package main

import (
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// TestBuildTasksOpsShape verifies that BuildTasksOps returns exactly 2 ops
// (tasks.tasks.list and tasks.tasks.insert) with the correct fields per bead
// gum-265 acceptance criteria.
func TestBuildTasksOpsShape(t *testing.T) {
	ops := BuildTasksOps()

	if got, want := len(ops), 12; got != want {
		t.Fatalf("BuildTasksOps() returned %d ops, want %d", got, want)
	}

	// Verify set of op IDs (tasklists CRUD + tasks list/get/insert/update/
	// delete/move/clear).
	wantIDs := map[string]bool{
		"tasks.tasklists.list":   true,
		"tasks.tasklists.get":    true,
		"tasks.tasklists.insert": true,
		"tasks.tasklists.update": true,
		"tasks.tasklists.delete": true,
		"tasks.tasks.list":       true,
		"tasks.tasks.get":        true,
		"tasks.tasks.insert":     true,
		"tasks.tasks.update":     true,
		"tasks.tasks.delete":     true,
		"tasks.tasks.move":       true,
		"tasks.tasks.clear":      true,
	}
	gotIDs := map[string]bool{}
	for _, op := range ops {
		gotIDs[op.OpID] = true
	}
	for id := range wantIDs {
		if !gotIDs[id] {
			t.Errorf("missing expected op %q", id)
		}
	}
	for id := range gotIDs {
		if !wantIDs[id] {
			t.Errorf("unexpected op %q", id)
		}
	}

	// Index ops by ID for targeted assertions.
	byID := map[string]catalog.Op{}
	for _, op := range ops {
		byID[op.OpID] = op
	}

	// Common assertions for both ops.
	for _, op := range ops {
		if op.OpSchemaVersion != 1 {
			t.Errorf("op %s: op_schema_version = %d, want 1", op.OpID, op.OpSchemaVersion)
		}
		if got := len(op.Variants); got != 1 {
			t.Errorf("op %s: variants = %d, want 1", op.OpID, got)
			continue
		}
		if op.DefaultVariantID != op.Variants[0].VariantID {
			t.Errorf("op %s: default_variant_id %q != variants[0].variant_id %q",
				op.OpID, op.DefaultVariantID, op.Variants[0].VariantID)
		}
		if op.Service != "tasks" {
			t.Errorf("op %s: service = %q, want %q", op.OpID, op.Service, "tasks")
		}
		if op.ServiceFamily != "workspace" {
			t.Errorf("op %s: service_family = %q, want %q", op.OpID, op.ServiceFamily, "workspace")
		}

		v := op.Variants[0]
		if v.AuthStrategy != catalog.AuthStrategyBYOOAuth {
			t.Errorf("op %s: auth_strategy = %q, want %q", op.OpID, v.AuthStrategy, catalog.AuthStrategyBYOOAuth)
		}
		if v.InterfaceKind != catalog.InterfaceKindDiscoveryREST {
			t.Errorf("op %s: interface_kind = %q, want %q", op.OpID, v.InterfaceKind, catalog.InterfaceKindDiscoveryREST)
		}
		if v.BackendKind != catalog.BackendKindTypedRestSDK {
			t.Errorf("op %s: backend_kind = %q, want %q", op.OpID, v.BackendKind, catalog.BackendKindTypedRestSDK)
		}
		if v.Stability != catalog.StabilityStable {
			t.Errorf("op %s: stability = %q, want %q", op.OpID, v.Stability, catalog.StabilityStable)
		}
		if v.Version != "v1" {
			t.Errorf("op %s: version = %q, want %q", op.OpID, v.Version, "v1")
		}
		if !v.Preferred {
			t.Errorf("op %s: preferred = false, want true", op.OpID)
		}
		if v.Binding == nil {
			t.Errorf("op %s: binding is nil", op.OpID)
			continue
		}
		if v.Binding.HTTP == nil {
			t.Errorf("op %s: binding.http is nil", op.OpID)
			continue
		}
		if v.Binding.GoPkg != "google.golang.org/api/tasks/v1" {
			t.Errorf("op %s: go_pkg = %q, want %q", op.OpID, v.Binding.GoPkg, "google.golang.org/api/tasks/v1")
		}
	}

	// Op-specific assertions: tasks.tasks.list.
	if lst, ok := byID["tasks.tasks.list"]; ok && len(lst.Variants) == 1 {
		v := lst.Variants[0]
		if v.VariantID != "tasks.v1.rest.tasks.list" {
			t.Errorf("tasks.tasks.list: variant_id = %q, want %q", v.VariantID, "tasks.v1.rest.tasks.list")
		}
		if v.RiskClass != catalog.RiskClassRead {
			t.Errorf("tasks.tasks.list: risk_class = %q, want %q", v.RiskClass, catalog.RiskClassRead)
		}
		wantScope := "https://www.googleapis.com/auth/tasks.readonly"
		if len(v.Scopes) != 1 || v.Scopes[0] != wantScope {
			t.Errorf("tasks.tasks.list: scopes = %v, want [%q]", v.Scopes, wantScope)
		}
		if v.Binding != nil && v.Binding.HTTP != nil {
			if v.Binding.HTTP.Method != "GET" {
				t.Errorf("tasks.tasks.list: http.method = %q, want GET", v.Binding.HTTP.Method)
			}
			wantPath := "https://www.googleapis.com/tasks/v1/lists/{tasklist}/tasks"
			if v.Binding.HTTP.Path != wantPath {
				t.Errorf("tasks.tasks.list: http.path = %q, want %q", v.Binding.HTTP.Path, wantPath)
			}
		}
		if v.Binding != nil && v.Binding.GoCall != "Tasks.List" {
			t.Errorf("tasks.tasks.list: go_call = %q, want %q", v.Binding.GoCall, "Tasks.List")
		}
	}

	// Op-specific assertions: tasks.tasks.insert.
	if ins, ok := byID["tasks.tasks.insert"]; ok && len(ins.Variants) == 1 {
		v := ins.Variants[0]
		if v.VariantID != "tasks.v1.rest.tasks.insert" {
			t.Errorf("tasks.tasks.insert: variant_id = %q, want %q", v.VariantID, "tasks.v1.rest.tasks.insert")
		}
		if v.RiskClass != catalog.RiskClassWrite {
			t.Errorf("tasks.tasks.insert: risk_class = %q, want %q", v.RiskClass, catalog.RiskClassWrite)
		}
		wantScope := "https://www.googleapis.com/auth/tasks"
		if len(v.Scopes) != 1 || v.Scopes[0] != wantScope {
			t.Errorf("tasks.tasks.insert: scopes = %v, want [%q]", v.Scopes, wantScope)
		}
		if v.Binding != nil && v.Binding.HTTP != nil {
			if v.Binding.HTTP.Method != "POST" {
				t.Errorf("tasks.tasks.insert: http.method = %q, want POST", v.Binding.HTTP.Method)
			}
			wantPath := "https://www.googleapis.com/tasks/v1/lists/{tasklist}/tasks"
			if v.Binding.HTTP.Path != wantPath {
				t.Errorf("tasks.tasks.insert: http.path = %q, want %q", v.Binding.HTTP.Path, wantPath)
			}
		}
		if v.Binding != nil && v.Binding.GoCall != "Tasks.Insert" {
			t.Errorf("tasks.tasks.insert: go_call = %q, want %q", v.Binding.GoCall, "Tasks.Insert")
		}
	}
}

// TestBuildTasksOpsValidates builds a minimal catalog containing only the Tasks
// ops and asserts it passes catalog.Validate.
func TestBuildTasksOpsValidates(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops:                  BuildTasksOps(),
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("validate tasks-only catalog: %v", err)
	}
}

// TestBuildTasksOpsRejectsGUMOAuth asserts that no Tasks variant uses
// gum_oauth, which is disabled in v0.1.0 per bd memory gum-auth-strategy-v3.
func TestBuildTasksOpsRejectsGUMOAuth(t *testing.T) {
	for _, op := range BuildTasksOps() {
		for _, v := range op.Variants {
			if v.AuthStrategy == catalog.AuthStrategyGUMOAuth {
				t.Errorf("op %s variant %s: gum_oauth is disabled in v0.1.0", op.OpID, v.VariantID)
			}
		}
	}
}
