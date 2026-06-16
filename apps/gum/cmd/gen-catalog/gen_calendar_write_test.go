package main

import (
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// TestBuildCalendarWriteOpsShape verifies that BuildCalendarWriteOps returns
// exactly 2 ops (calendar.events.insert and calendar.events.update) with the
// correct fields per bead gum-7tuq.2 acceptance criteria.
func TestBuildCalendarWriteOpsShape(t *testing.T) {
	ops := BuildCalendarWriteOps()

	if got, want := len(ops), 2; got != want {
		t.Fatalf("BuildCalendarWriteOps() returned %d ops, want %d", got, want)
	}

	// Verify set of op IDs.
	wantIDs := map[string]bool{
		"calendar.events.insert": true,
		"calendar.events.update": true,
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
		if op.Service != "calendar" {
			t.Errorf("op %s: service = %q, want %q", op.OpID, op.Service, "calendar")
		}
		if op.ServiceFamily != "workspace" {
			t.Errorf("op %s: service_family = %q, want %q", op.OpID, op.ServiceFamily, "workspace")
		}

		v := op.Variants[0]
		if v.RiskClass != catalog.RiskClassWrite {
			t.Errorf("op %s: risk_class = %q, want %q", op.OpID, v.RiskClass, catalog.RiskClassWrite)
		}
		if v.AuthStrategy != catalog.AuthStrategyBYOOAuth {
			t.Errorf("op %s: auth_strategy = %q, want %q", op.OpID, v.AuthStrategy, catalog.AuthStrategyBYOOAuth)
		}
		wantScope := "https://www.googleapis.com/auth/calendar"
		if len(v.Scopes) != 1 || v.Scopes[0] != wantScope {
			t.Errorf("op %s: scopes = %v, want [%q]", op.OpID, v.Scopes, wantScope)
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
		if v.Version != "v3" {
			t.Errorf("op %s: version = %q, want %q", op.OpID, v.Version, "v3")
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
		if v.Binding.GoPkg != "google.golang.org/api/calendar/v3" {
			t.Errorf("op %s: go_pkg = %q, want %q", op.OpID, v.Binding.GoPkg, "google.golang.org/api/calendar/v3")
		}
	}

	// Op-specific assertions: calendar.events.insert.
	if ins, ok := byID["calendar.events.insert"]; ok && len(ins.Variants) == 1 {
		v := ins.Variants[0]
		if v.VariantID != "calendar.v3.rest.events.insert" {
			t.Errorf("calendar.events.insert: variant_id = %q, want %q", v.VariantID, "calendar.v3.rest.events.insert")
		}
		if v.Binding != nil && v.Binding.HTTP != nil {
			if v.Binding.HTTP.Method != "POST" {
				t.Errorf("calendar.events.insert: http.method = %q, want POST", v.Binding.HTTP.Method)
			}
			wantPath := "https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events"
			if v.Binding.HTTP.Path != wantPath {
				t.Errorf("calendar.events.insert: http.path = %q, want %q", v.Binding.HTTP.Path, wantPath)
			}
		}
		if v.Binding != nil && v.Binding.GoCall != "Events.Insert" {
			t.Errorf("calendar.events.insert: go_call = %q, want %q", v.Binding.GoCall, "Events.Insert")
		}
	}

	// Op-specific assertions: calendar.events.update.
	if upd, ok := byID["calendar.events.update"]; ok && len(upd.Variants) == 1 {
		v := upd.Variants[0]
		if v.VariantID != "calendar.v3.rest.events.update" {
			t.Errorf("calendar.events.update: variant_id = %q, want %q", v.VariantID, "calendar.v3.rest.events.update")
		}
		if v.Binding != nil && v.Binding.HTTP != nil {
			if v.Binding.HTTP.Method != "PUT" {
				t.Errorf("calendar.events.update: http.method = %q, want PUT", v.Binding.HTTP.Method)
			}
			wantPath := "https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events/{eventId}"
			if v.Binding.HTTP.Path != wantPath {
				t.Errorf("calendar.events.update: http.path = %q, want %q", v.Binding.HTTP.Path, wantPath)
			}
		}
		if v.Binding != nil && v.Binding.GoCall != "Events.Update" {
			t.Errorf("calendar.events.update: go_call = %q, want %q", v.Binding.GoCall, "Events.Update")
		}
	}
}

// TestBuildCalendarWriteOpsValidates builds a minimal catalog containing only
// the calendar write ops and asserts it passes catalog.Validate.
func TestBuildCalendarWriteOpsValidates(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops:                  BuildCalendarWriteOps(),
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("validate calendar-write-only catalog: %v", err)
	}
}

// TestBuildCalendarWriteOpsRejectsGUMOAuth asserts that no calendar write
// variant uses gum_oauth, which is disabled in v0.1.0 per bd memory
// gum-auth-strategy-v3.
func TestBuildCalendarWriteOpsRejectsGUMOAuth(t *testing.T) {
	for _, op := range BuildCalendarWriteOps() {
		for _, v := range op.Variants {
			if v.AuthStrategy == catalog.AuthStrategyGUMOAuth {
				t.Errorf("op %s variant %s: gum_oauth is disabled in v0.1.0", op.OpID, v.VariantID)
			}
		}
	}
}
