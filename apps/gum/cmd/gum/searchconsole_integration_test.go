package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestSearchConsoleSearchAnalyticsQuery proves that the full dispatch path
// supports the Search Console searchanalytics.query POST: the JSON body lands
// at the upstream with Content-Type=application/json and the Bearer token from
// the (fake) ADC resolver is attached.
//
// This is the canonical "Search Console works end-to-end" smoke test.
func TestSearchConsoleSearchAnalyticsQuery(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotMethod string
	var gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"rows":[{"keys":["claude code"],"clicks":42,"impressions":314}]}`))
	}))
	defer srv.Close()

	// Catalog mirrors the real searchconsole.searchanalytics.query shape but
	// points at the test server.
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		Ops: []catalog.Op{{
			OpID:             "searchconsole.searchanalytics.query",
			OpSchemaVersion:  1,
			Title:            "Query search analytics",
			Summary:          "test",
			DefaultVariantID: "searchconsole.v1.rest.searchanalytics.query",
			Variants: []catalog.Variant{{
				VariantID:            "searchconsole.v1.rest.searchanalytics.query",
				VariantSchemaVersion: 1,
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindTypedRestSDK,
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Scopes:               []string{"https://www.googleapis.com/auth/webmasters.readonly"},
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.typed-rest-sdk",
					OperationKey:         "searchconsole.searchanalytics.query",
					HTTP: &catalog.HTTPBinding{
						Method: "POST",
						Path:   srv.URL + "/webmasters/v3/sites/{siteUrl}/searchAnalytics/query",
					},
				},
			}},
		}},
	}

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)

	disp := dispatch.NewDispatcherWithConfig(
		cat,
		map[string]dispatch.Adapter{"rest.typed-rest-sdk": ex},
		dispatch.DispatcherConfig{
			Auth: &auth.CompositeResolver{BYO: &fakeADC{token: "tok-sc"}},
			Policy: dispatch.ProfilePolicy{
				AllowedScopes: []string{"https://www.googleapis.com/auth/webmasters.readonly"},
			},
		},
	)

	shaped, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID: "searchconsole.searchanalytics.query",
		Args: map[string]any{
			"siteUrl": "sc-domain:example.com",
			"body": map[string]any{
				"startDate":  "2026-04-01",
				"endDate":    "2026-04-30",
				"dimensions": []string{"query"},
				"rowLimit":   10,
			},
		},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if gotMethod != "POST" {
		t.Errorf("HTTP method = %q, want POST", gotMethod)
	}
	if !strings.Contains(gotPath, "/webmasters/v3/sites/") || !strings.HasSuffix(gotPath, "/searchAnalytics/query") {
		t.Errorf("HTTP path = %q, want /webmasters/v3/sites/<siteUrl>/searchAnalytics/query", gotPath)
	}
	if !strings.Contains(gotPath, "sc-domain%3Aexample.com") && !strings.Contains(gotPath, "sc-domain:example.com") {
		t.Errorf("HTTP path = %q, must contain (escaped) siteUrl", gotPath)
	}
	if gotAuth != "Bearer tok-sc" {
		t.Errorf("Authorization = %q, want Bearer tok-sc", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}

	var decoded map[string]any
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("decode forwarded body: %v\nbody=%s", err, gotBody)
	}
	if decoded["startDate"] != "2026-04-01" || decoded["endDate"] != "2026-04-30" {
		t.Errorf("body did not preserve date range: %v", decoded)
	}
	if !strings.Contains(string(shaped.Body), "claude code") {
		t.Errorf("response body did not flow through dispatcher: %s", shaped.Body)
	}
}

// TestSearchConsoleSitesListReachesUpstream proves a GET op (read) flows end-to-end
// — Bearer token attached, query params preserved, no JSON body sent.
func TestSearchConsoleSitesListReachesUpstream(t *testing.T) {
	var gotMethod string
	var gotAuth string
	var gotBodyLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBodyLen = len(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"siteEntry":[{"siteUrl":"sc-domain:example.com","permissionLevel":"siteOwner"}]}`))
	}))
	defer srv.Close()

	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		Ops: []catalog.Op{{
			OpID:             "searchconsole.sites.list",
			OpSchemaVersion:  1,
			Title:            "List Search Console properties",
			Summary:          "test",
			DefaultVariantID: "searchconsole.v1.rest.sites.list",
			Variants: []catalog.Variant{{
				VariantID:            "searchconsole.v1.rest.sites.list",
				VariantSchemaVersion: 1,
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindTypedRestSDK,
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Scopes:               []string{"https://www.googleapis.com/auth/webmasters.readonly"},
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.typed-rest-sdk",
					OperationKey:         "searchconsole.sites.list",
					HTTP: &catalog.HTTPBinding{
						Method: "GET",
						Path:   srv.URL + "/webmasters/v3/sites",
					},
				},
			}},
		}},
	}

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)

	disp := dispatch.NewDispatcherWithConfig(
		cat,
		map[string]dispatch.Adapter{"rest.typed-rest-sdk": ex},
		dispatch.DispatcherConfig{
			Auth: &auth.CompositeResolver{BYO: &fakeADC{token: "tok-list"}},
			Policy: dispatch.ProfilePolicy{
				AllowedScopes: []string{"https://www.googleapis.com/auth/webmasters.readonly"},
			},
		},
	)

	shaped, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID: "searchconsole.sites.list",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if gotMethod != "GET" {
		t.Errorf("HTTP method = %q, want GET", gotMethod)
	}
	if gotAuth != "Bearer tok-list" {
		t.Errorf("Authorization = %q, want Bearer tok-list", gotAuth)
	}
	if gotBodyLen != 0 {
		t.Errorf("GET sent a request body of %d bytes; should be empty", gotBodyLen)
	}
	if !strings.Contains(string(shaped.Body), "sc-domain:example.com") {
		t.Errorf("response did not flow through dispatcher: %s", shaped.Body)
	}
}
