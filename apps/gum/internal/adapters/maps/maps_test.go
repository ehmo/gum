package maps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestBackendKindMapsSDK pins spec §14 line 3335: the maps-sdk backend kind
// is dispatchable via internal/adapters/maps using
// googlemaps.github.io/maps. We stand up an httptest server that returns
// a canned Directions JSON payload, point the SDK at it via
// Adapter.BaseURL, and assert the executor returns a 200 Response whose
// body carries the expected route summary.
func TestBackendKindMapsSDK(t *testing.T) {
	const cannedResponse = `{
		"status": "OK",
		"routes": [
			{
				"summary": "US-101 N",
				"legs": [
					{
						"distance": {"text": "5 mi", "value": 8046},
						"duration": {"text": "10 mins", "value": 600},
						"start_address": "1 Telegraph Hill Blvd, San Francisco, CA",
						"end_address": "Golden Gate Bridge, San Francisco, CA"
					}
				]
			}
		],
		"geocoded_waypoints": [
			{"geocoder_status":"OK","place_id":"start"},
			{"geocoder_status":"OK","place_id":"end"}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Confirm the SDK actually hit the directions endpoint.
		if !strings.Contains(r.URL.Path, "directions") {
			http.Error(w, "wrong path: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		// Confirm the API key flows through as the `key` query param —
		// googlemaps.github.io/maps signs requests with ?key=... rather
		// than the X-Goog-Api-Key header used by typed-rest-sdk variants.
		if r.URL.Query().Get("key") == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, cannedResponse)
	}))
	defer srv.Close()

	adapter := &Adapter{BaseURL: srv.URL}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{
			BackendKind: catalog.BackendKindMapsSDK,
			Binding:     &catalog.Binding{AdapterKey: "maps.directions"},
		},
		AdapterKey: "maps.directions",
	}
	inv := &dispatch.Invocation{
		OpID: "maps.directions.compute",
		Args: map[string]any{
			"origin":      "San Francisco, CA",
			"destination": "Golden Gate Bridge",
			"mode":        "driving",
		},
	}
	creds := &dispatch.Credentials{APIKey: "AIza-fake-maps-key"}

	resp, err := adapter.Execute(context.Background(), inv, rv, creds)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d; want 200", resp.StatusCode)
	}
	if resp.Format != "json" {
		t.Errorf("Format = %q; want json", resp.Format)
	}
	var got map[string]any
	if err := json.Unmarshal(resp.Body, &got); err != nil {
		t.Fatalf("response body is not JSON: %v\n%s", err, resp.Body)
	}
	routes, _ := got["routes"].([]any)
	if len(routes) != 1 {
		t.Fatalf("routes len = %d; want 1\nbody=%s", len(routes), resp.Body)
	}
}

// TestMapsAdapterRequiresAPIKey verifies the auth wiring: Maps Web Service
// uses api_key (spec §7), so an empty Credentials.APIKey must surface as
// an error rather than silently proceeding.
func TestMapsAdapterRequiresAPIKey(t *testing.T) {
	adapter := NewAdapter()
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{"origin": "A", "destination": "B"}},
		&dispatch.ResolvedVariant{Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "maps.directions"}}},
		&dispatch.Credentials{},
	)
	if err == nil {
		t.Fatal("expected error for missing APIKey, got nil")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Errorf("error = %q; want hint about API key", err.Error())
	}
}

// TestMapsAdapterUnsupportedEndpoint verifies the dispatch surface fails
// loud on unrecognised endpoint discriminators — Maps has ~10 SDK methods
// and we want a new variant referencing an unwired one to get a typed
// error rather than a nil dereference.
func TestMapsAdapterUnsupportedEndpoint(t *testing.T) {
	adapter := NewAdapter()
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{},
		&dispatch.ResolvedVariant{Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "maps.streetview"}}},
		&dispatch.Credentials{APIKey: "AIza-fake"},
	)
	if err == nil {
		t.Fatal("expected error for unsupported endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported endpoint") {
		t.Errorf("error = %q; want `unsupported endpoint` hint", err.Error())
	}
}
