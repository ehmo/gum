package main

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

func TestBuildGoogleAdsOps(t *testing.T) {
	ops := BuildGoogleAdsOps()
	if len(ops) != 3 {
		t.Fatalf("BuildGoogleAdsOps len = %d; want 3", len(ops))
	}

	wantIDs := map[string]string{
		"googleads.keywordPlanIdeas.generateKeywordIdeas":             "generateKeywordIdeas",
		"googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics": "generateKeywordHistoricalMetrics",
		"googleads.keywordPlanIdeas.generateKeywordForecastMetrics":   "generateKeywordForecastMetrics",
	}

	for _, op := range ops {
		method, ok := wantIDs[op.OpID]
		if !ok {
			t.Errorf("unexpected op_id %q", op.OpID)
			continue
		}
		if op.Service != "googleads" || op.ServiceFamily != "googleads" {
			t.Errorf("%s: service/family = %q/%q; want googleads/googleads", op.OpID, op.Service, op.ServiceFamily)
		}
		if len(op.Variants) != 1 {
			t.Fatalf("%s: variants = %d; want 1", op.OpID, len(op.Variants))
		}
		v := op.Variants[0]
		if v.AuthStrategy != catalog.AuthStrategyBYOOAuth {
			t.Errorf("%s: auth = %q; want byo_oauth", op.OpID, v.AuthStrategy)
		}
		if v.BackendKind != catalog.BackendKindGoogleAdsSDK {
			t.Errorf("%s: backend = %q; want google-ads-sdk", op.OpID, v.BackendKind)
		}
		if v.RiskClass != catalog.RiskClassRead {
			t.Errorf("%s: risk = %q; want read", op.OpID, v.RiskClass)
		}
		if len(v.Scopes) != 1 || v.Scopes[0] != "https://www.googleapis.com/auth/adwords" {
			t.Errorf("%s: scopes = %v; want [adwords]", op.OpID, v.Scopes)
		}
		if v.Binding == nil || v.Binding.AdapterKey != "googleads."+method {
			t.Errorf("%s: adapter_key = %v; want googleads.%s", op.OpID, v.Binding, method)
		}
		if v.Binding.HTTP == nil || v.Binding.HTTP.Method != "POST" {
			t.Errorf("%s: http method not POST", op.OpID)
		}
		if !strings.HasSuffix(v.Binding.HTTP.Path, ":"+method) {
			t.Errorf("%s: path %q must end with :%s", op.OpID, v.Binding.HTTP.Path, method)
		}
		if !strings.Contains(v.Binding.HTTP.Path, "{customerId}") {
			t.Errorf("%s: path %q must template {customerId}", op.OpID, v.Binding.HTTP.Path)
		}
		// customerId must be a required path field so dispatch enforces presence.
		var foundCustomer bool
		for _, f := range op.RequestFields {
			if f.Name == "customerId" {
				foundCustomer = true
				if f.Location != catalog.RequestFieldPath || !f.Required {
					t.Errorf("%s: customerId must be a required path field, got loc=%q required=%v", op.OpID, f.Location, f.Required)
				}
			}
		}
		if !foundCustomer {
			t.Errorf("%s: missing customerId request field", op.OpID)
		}
	}

	// The ops must pass catalog validation as part of a catalog.
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-06-05T00:00:00Z",
		GeneratorVersion:     "test",
		Ops:                  ops,
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("catalog.Validate with googleads ops: %v", err)
	}
}
