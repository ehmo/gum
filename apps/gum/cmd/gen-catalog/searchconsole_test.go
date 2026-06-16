package main

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestBuildSearchConsoleOpsShape verifies that the hardcoded Search Console
// catalog ops are well-formed: 10 ops, expected op_ids present, correct risk
// classes, no gum_oauth, sensible HTTP methods and absolute URLs.
func TestBuildSearchConsoleOpsShape(t *testing.T) {
	ops := BuildSearchConsoleOps()
	if got, want := len(ops), 10; got != want {
		t.Fatalf("BuildSearchConsoleOps() returned %d ops, want %d", got, want)
	}

	wantByID := map[string]catalog.RiskClass{
		"searchconsole.sites.list":                  catalog.RiskClassRead,
		"searchconsole.sites.get":                   catalog.RiskClassRead,
		"searchconsole.sites.add":                   catalog.RiskClassWrite,
		"searchconsole.sites.delete":                catalog.RiskClassDestructive,
		"searchconsole.sitemaps.list":               catalog.RiskClassRead,
		"searchconsole.sitemaps.get":                catalog.RiskClassRead,
		"searchconsole.sitemaps.submit":             catalog.RiskClassWrite,
		"searchconsole.sitemaps.delete":             catalog.RiskClassDestructive,
		"searchconsole.searchanalytics.query":       catalog.RiskClassRead,
		"searchconsole.urlInspection.index.inspect": catalog.RiskClassRead,
	}

	got := map[string]catalog.RiskClass{}
	for _, op := range ops {
		if len(op.Variants) != 1 {
			t.Errorf("op %s: variants=%d, want 1", op.OpID, len(op.Variants))
			continue
		}
		v := op.Variants[0]
		if v.AuthStrategy == catalog.AuthStrategyGUMOAuth {
			t.Errorf("op %s: gum_oauth is disabled in v0.1.0", op.OpID)
		}
		if v.AuthStrategy != catalog.AuthStrategyBYOOAuth {
			t.Errorf("op %s: auth_strategy = %q, want byo_oauth", op.OpID, v.AuthStrategy)
		}
		if v.Binding == nil || v.Binding.HTTP == nil {
			t.Errorf("op %s: missing HTTP binding", op.OpID)
			continue
		}
		if !strings.HasPrefix(v.Binding.HTTP.Path, "https://searchconsole.googleapis.com/") {
			t.Errorf("op %s: HTTP path %q must be absolute on searchconsole.googleapis.com", op.OpID, v.Binding.HTTP.Path)
		}
		if len(v.Scopes) == 0 {
			t.Errorf("op %s: scopes empty; every Search Console op must declare a webmasters scope", op.OpID)
		}
		got[op.OpID] = v.RiskClass
	}
	for id, wantRisk := range wantByID {
		gotRisk, ok := got[id]
		if !ok {
			t.Errorf("missing expected op %q", id)
			continue
		}
		if gotRisk != wantRisk {
			t.Errorf("op %s: risk_class = %q, want %q", id, gotRisk, wantRisk)
		}
	}
}

// TestBuildSearchConsoleOpsValidate verifies that a catalog containing only the
// Search Console ops passes catalog.Catalog.Validate.
func TestBuildSearchConsoleOpsValidate(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-05-22T00:00:00Z",
		GeneratorVersion:     "test",
		Ops:                  BuildSearchConsoleOps(),
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("validate searchconsole-only catalog: %v", err)
	}
}

// TestSearchConsoleQueryEndpointsHaveBodyDoc verifies that searchanalytics.query
// and urlInspection.index.inspect summaries mention "args.body" so users know
// they need to supply a JSON body. This is a documentation contract test.
func TestSearchConsoleQueryEndpointsHaveBodyDoc(t *testing.T) {
	ops := BuildSearchConsoleOps()
	mustMention := map[string]bool{
		"searchconsole.searchanalytics.query":       false,
		"searchconsole.urlInspection.index.inspect": false,
	}
	for _, op := range ops {
		if _, want := mustMention[op.OpID]; !want {
			continue
		}
		if !strings.Contains(op.Summary, "body") {
			t.Errorf("op %s summary does not mention body: %q", op.OpID, op.Summary)
		}
		mustMention[op.OpID] = true
	}
	for id, found := range mustMention {
		if !found {
			t.Errorf("op %s not present", id)
		}
	}
}
