package maps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestExecuteWithCustomHTTPClientAppendsOption pins Execute's
// `a.HTTPClient != nil → opts += WithHTTPClient` arm (maps.go:51-53).
// When the adapter has a custom http.Client (e.g., one with a proxy or
// custom transport), Execute MUST plumb it through to the SDK client
// constructor — otherwise the test/staging override is silently lost.
//
// We use a custom Client whose Transport returns a canned Directions
// payload; if the option were dropped, the request would hit the
// default HTTP transport and never reach our recorded responder.
func TestExecuteWithCustomHTTPClientAppendsOption(t *testing.T) {
	const cannedResponse = `{"status":"OK","routes":[{"summary":"X"}],"geocoded_waypoints":[]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Require the key= query param so we know auth wiring fired.
		if r.URL.Query().Get("key") == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cannedResponse))
	}))
	defer srv.Close()

	// Custom client — distinct from net/http.DefaultClient so the
	// option-append path is the only way the SDK reaches our server.
	customClient := &http.Client{}
	adapter := &Adapter{
		BaseURL:    srv.URL,
		HTTPClient: customClient,
	}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{
			BackendKind: catalog.BackendKindMapsSDK,
			Binding:     &catalog.Binding{AdapterKey: "maps.directions"},
		},
	}
	inv := &dispatch.Invocation{Args: map[string]any{"origin": "A", "destination": "B"}}
	creds := &dispatch.Credentials{APIKey: "AIza-fake"}

	resp, err := adapter.Execute(context.Background(), inv, rv, creds)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode=%d; want 200 (HTTPClient option must reach server)", resp.StatusCode)
	}
}

// TestExecuteDirectionsSDKErrorWrapsWithEndpoint pins executeDirections's
// `client.Directions err → "maps.directions: %w"` wrap arm
// (maps.go:98-100). A 500 from upstream propagates through the SDK as
// an error; the wrap names the endpoint so multi-endpoint failures
// stay distinguishable in logs.
func TestExecuteDirectionsSDKErrorWrapsWithEndpoint(t *testing.T) {
	// Server returns malformed JSON so the SDK's response parser fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"INVALID_REQUEST","error_message":"forced err"}`))
	}))
	defer srv.Close()

	adapter := &Adapter{BaseURL: srv.URL}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{
			BackendKind: catalog.BackendKindMapsSDK,
			Binding:     &catalog.Binding{AdapterKey: "maps.directions"},
		},
	}
	inv := &dispatch.Invocation{Args: map[string]any{"origin": "A", "destination": "B"}}
	creds := &dispatch.Credentials{APIKey: "AIza-fake"}

	_, err := adapter.Execute(context.Background(), inv, rv, creds)
	if err == nil {
		t.Fatalf("Execute err=nil; want SDK-error wrap")
	}
	if !strings.Contains(err.Error(), "maps.directions") {
		t.Errorf("err=%v; want 'maps.directions' endpoint name in wrap", err)
	}
}
