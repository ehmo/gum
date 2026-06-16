// gum-mwe acceptance: Tier B Calendar surface (ACL, colors, freebusy,
// settings, calendars CRUD, additional events ops) so `gum search 'calendar
// freebusy'` returns relevant ops and `gum.describe_op` succeeds across the
// full Calendar resource set.

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// TestBuildCalendarTierBOpsCoverage pins op_id, risk_class, and presence of
// each Tier B Calendar op required by spec §1.1 + §5.2.
func TestBuildCalendarTierBOpsCoverage(t *testing.T) {
	want := []struct {
		opID      string
		riskClass catalog.RiskClass
	}{
		// events (additional)
		{"calendar.events.get", catalog.RiskClassRead},
		{"calendar.events.patch", catalog.RiskClassWrite},
		{"calendar.events.delete", catalog.RiskClassDestructive},
		{"calendar.events.instances", catalog.RiskClassRead},
		{"calendar.events.move", catalog.RiskClassWrite},
		{"calendar.events.quickAdd", catalog.RiskClassWrite},
		// calendars CRUD
		{"calendar.calendars.get", catalog.RiskClassRead},
		{"calendar.calendars.insert", catalog.RiskClassWrite},
		{"calendar.calendars.update", catalog.RiskClassWrite},
		{"calendar.calendars.patch", catalog.RiskClassWrite},
		{"calendar.calendars.delete", catalog.RiskClassDestructive},
		{"calendar.calendars.clear", catalog.RiskClassDestructive},
		// calendarList (additional)
		{"calendar.calendarList.get", catalog.RiskClassRead},
		{"calendar.calendarList.insert", catalog.RiskClassWrite},
		{"calendar.calendarList.update", catalog.RiskClassWrite},
		{"calendar.calendarList.patch", catalog.RiskClassWrite},
		{"calendar.calendarList.delete", catalog.RiskClassWrite},
		// ACL
		{"calendar.acl.list", catalog.RiskClassRead},
		{"calendar.acl.get", catalog.RiskClassRead},
		{"calendar.acl.insert", catalog.RiskClassWrite},
		{"calendar.acl.update", catalog.RiskClassWrite},
		{"calendar.acl.patch", catalog.RiskClassWrite},
		{"calendar.acl.delete", catalog.RiskClassDestructive},
		// colors + freebusy
		{"calendar.colors.get", catalog.RiskClassRead},
		{"calendar.freebusy.query", catalog.RiskClassRead},
		// settings
		{"calendar.settings.list", catalog.RiskClassRead},
		{"calendar.settings.get", catalog.RiskClassRead},
	}

	got := BuildCalendarTierBOps()
	byID := make(map[string]catalog.Op, len(got))
	for _, op := range got {
		byID[op.OpID] = op
	}

	for _, w := range want {
		op, ok := byID[w.opID]
		if !ok {
			t.Errorf("op %q missing from Calendar Tier B set", w.opID)
			continue
		}
		if op.Service != "calendar" {
			t.Errorf("%s: service=%q want calendar", w.opID, op.Service)
		}
		if op.ServiceFamily != "workspace" {
			t.Errorf("%s: service_family=%q want workspace", w.opID, op.ServiceFamily)
		}
		if len(op.Variants) != 1 {
			t.Errorf("%s: variants=%d want 1", w.opID, len(op.Variants))
			continue
		}
		v := op.Variants[0]
		if v.RiskClass != w.riskClass {
			t.Errorf("%s: risk_class=%q want %q", w.opID, v.RiskClass, w.riskClass)
		}
		if v.AuthStrategy != catalog.AuthStrategyBYOOAuth {
			t.Errorf("%s: auth_strategy=%q want byo_oauth", w.opID, v.AuthStrategy)
		}
		if v.InterfaceKind != catalog.InterfaceKindDiscoveryREST {
			t.Errorf("%s: interface_kind=%q want discovery-rest", w.opID, v.InterfaceKind)
		}
		if v.BackendKind != catalog.BackendKindTypedRestSDK {
			t.Errorf("%s: backend_kind=%q want typed-rest-sdk", w.opID, v.BackendKind)
		}
		if len(v.Scopes) == 0 {
			t.Errorf("%s: no scopes set", w.opID)
		}
		if v.Binding == nil || v.Binding.HTTP == nil {
			t.Errorf("%s: missing binding.http", w.opID)
			continue
		}
		if !strings.HasPrefix(v.Binding.HTTP.Path, "/calendar/v3/") {
			t.Errorf("%s: http.path=%q does not start with /calendar/v3/", w.opID, v.Binding.HTTP.Path)
		}
		if v.Binding.GoPkg != "google.golang.org/api/calendar/v3" {
			t.Errorf("%s: go_pkg=%q want google.golang.org/api/calendar/v3", w.opID, v.Binding.GoPkg)
		}
	}
}

// TestBuildCalendarTierBOpsValidates ensures the Tier B-only catalog passes
// catalog.Validate.
func TestBuildCalendarTierBOpsValidates(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops:                  BuildCalendarTierBOps(),
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("validate Calendar Tier B-only catalog: %v", err)
	}
}

// TestBuildCalendarTierBOpsRejectsGUMOAuth pins that no Tier B variant uses
// gum_oauth (disabled in v0.1.0 per bd memory gum-auth-strategy-v3).
func TestBuildCalendarTierBOpsRejectsGUMOAuth(t *testing.T) {
	for _, op := range BuildCalendarTierBOps() {
		for _, v := range op.Variants {
			if v.AuthStrategy == catalog.AuthStrategyGUMOAuth {
				t.Errorf("op %s variant %s: gum_oauth disabled in v0.1.0", op.OpID, v.VariantID)
			}
		}
	}
}

// TestBuildCalendarTierBOpsFreebusyDiscoverable pins the gum-mwe acceptance
// criterion: freebusy.query must be present so BM25 search for "freebusy"
// returns a hit.
func TestBuildCalendarTierBOpsFreebusyDiscoverable(t *testing.T) {
	for _, op := range BuildCalendarTierBOps() {
		if op.OpID == "calendar.freebusy.query" {
			if !strings.Contains(strings.ToLower(op.Summary), "free") {
				t.Errorf("calendar.freebusy.query summary lacks 'free' keyword for BM25 hit: %q", op.Summary)
			}
			return
		}
	}
	t.Fatal("calendar.freebusy.query missing from Tier B catalog; BM25 'freebusy' query will return no hits")
}
