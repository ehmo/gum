package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestAuthStrategyApiKey — gum-vwa acceptance. The api_key strategy must
// dispatch a request with the X-Goog-Api-Key header set to the resolved
// key, and must NOT send a Bearer Authorization header (the two surfaces
// are mutually exclusive on the wire — sending both is a Google edge red
// flag that masks misconfigured CompositeResolver wiring).
//
// The harness mirrors TestDispatchEndToEndWithFakeServer: a tiny
// httptest server records the inbound headers, the dispatcher is built
// with a CompositeResolver whose APIKey branch is wired to a fake
// resolver, and we assert the projected headers match spec §7 line 1284.
func TestAuthStrategyApiKey(t *testing.T) {
	var gotAuth, gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-Goog-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"messages":[]}`))
	}))
	defer srv.Close()

	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		Ops: []catalog.Op{{
			OpID:             "test.apikey.read",
			OpSchemaVersion:  1,
			DefaultVariantID: "test.apikey.read.v1",
			Variants: []catalog.Variant{{
				VariantID:            "test.apikey.read.v1",
				VariantSchemaVersion: 1,
				InterfaceKind:        "discovery-rest",
				BackendKind:          "typed-rest-sdk",
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyAPIKey,
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.typed-rest-sdk",
					HTTP: &catalog.HTTPBinding{
						Method: "GET",
						Path:   srv.URL + "/v1/things",
					},
				},
			}},
		}},
	}

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)

	disp := dispatch.NewDispatcherWithConfig(
		cat,
		map[string]dispatch.Adapter{
			"rest.typed-rest-sdk": ex,
		},
		dispatch.DispatcherConfig{
			Auth: &auth.CompositeResolver{
				APIKey: &auth.APIKeyResolver{Lookup: func() string { return "AIza-fake-key" }},
			},
		},
	)

	shaped, err := disp.Dispatch(context.Background(), &dispatch.Invocation{OpID: "test.apikey.read"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.Contains(string(shaped.Body), "messages") {
		t.Errorf("response body missing 'messages': %s", shaped.Body)
	}
	if gotAPIKey != "AIza-fake-key" {
		t.Errorf("X-Goog-Api-Key = %q; want AIza-fake-key", gotAPIKey)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q; want empty (api_key + Bearer must be mutually exclusive)", gotAuth)
	}
}
