// Package maps is the backend executor for catalog variants with
// backend_kind="maps-sdk" (spec §14 line 3335). It wraps
// googlemaps.github.io/maps so the dispatcher can call the Maps Web Service
// family (Directions, Geocode, Places, …) without falling through the
// raw-HTTP long-tail dispatcher.
//
// v0.1.0 implements the Directions endpoint as the canary surface; the
// other Maps endpoints follow the same adapter shape and land
// incrementally as catalog variants reference them.
package maps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	gmaps "googlemaps.github.io/maps"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/httputil"
)

// Adapter executes Maps Web Service calls for catalog variants whose
// binding.adapter_key starts with `maps.`. The binding's `endpoint`
// substring (e.g. "directions", "geocode") selects which SDK method
// runs; v0.1.0 wires "directions" only.
type Adapter struct {
	// HTTPClient is forwarded to the Maps SDK via WithHTTPClient. Tests
	// inject an httptest.Server-backed client; production leaves it nil
	// so the SDK uses its default.
	HTTPClient *http.Client
	// BaseURL, when non-empty, overrides the Maps base URL via the
	// SDK's WithBaseURL option. Required for offline tests pointing at
	// an httptest server; production leaves it empty.
	BaseURL string
}

// NewAdapter constructs a Maps adapter with production defaults.
func NewAdapter() *Adapter { return &Adapter{} }

// Execute is the dispatch.Adapter entry point. It pulls the API key from
// creds.APIKey (spec §7 auth_strategy=api_key is the canonical Maps auth
// path), constructs a *maps.Client, then routes to the SDK call indicated
// by the variant binding.
func (a *Adapter) Execute(ctx context.Context, inv *dispatch.Invocation, rv *dispatch.ResolvedVariant, creds *dispatch.Credentials) (*dispatch.Response, error) {
	if creds == nil || creds.APIKey == "" {
		return nil, errors.New("maps adapter: missing API key (run `gum auth use-api-key`)")
	}
	// Always hand the SDK a response-size-capped client: googlemaps reads bodies
	// with an unbounded json.Decoder, so an oversized/hostile upstream could OOM
	// the process otherwise. CappedClient preserves any injected test transport.
	opts := []gmaps.ClientOption{
		gmaps.WithAPIKey(creds.APIKey),
		gmaps.WithHTTPClient(httputil.CappedClient(a.HTTPClient)),
	}
	if a.BaseURL != "" {
		opts = append(opts, gmaps.WithBaseURL(a.BaseURL))
	}
	client, err := gmaps.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("maps adapter: NewClient: %w", err)
	}

	endpoint := mapsEndpoint(rv)
	switch endpoint {
	case "directions":
		return executeDirections(ctx, client, inv)
	default:
		return nil, fmt.Errorf("maps adapter: unsupported endpoint %q (v0.1.0 wires `directions` only)", endpoint)
	}
}

// mapsEndpoint extracts the endpoint discriminator from a variant binding.
// The convention is binding.adapter_key in the form "maps.<endpoint>" so
// the catalog can declare which SDK call this variant should drive.
func mapsEndpoint(rv *dispatch.ResolvedVariant) string {
	if rv == nil || rv.Variant == nil || rv.Variant.Binding == nil {
		return ""
	}
	key := rv.Variant.Binding.AdapterKey
	const prefix = "maps."
	if len(key) > len(prefix) && key[:len(prefix)] == prefix {
		return key[len(prefix):]
	}
	return ""
}

// executeDirections marshals inv.Args into a maps.DirectionsRequest, calls
// the SDK, and serialises the response routes as JSON.
func executeDirections(ctx context.Context, client *gmaps.Client, inv *dispatch.Invocation) (*dispatch.Response, error) {
	req := &gmaps.DirectionsRequest{
		Origin:      stringArg(inv.Args, "origin"),
		Destination: stringArg(inv.Args, "destination"),
		Mode:        gmaps.Mode(stringArg(inv.Args, "mode")),
	}
	if req.Origin == "" || req.Destination == "" {
		return nil, errors.New("maps.directions: origin and destination are required")
	}
	routes, geocoded, err := client.Directions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("maps.directions: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"routes":             routes,
		"geocoded_waypoints": geocoded,
	})
	if err != nil {
		return nil, fmt.Errorf("maps.directions: marshal response: %w", err)
	}
	return &dispatch.Response{
		Body:       body,
		Format:     "json",
		BytesOut:   len(body),
		StatusCode: http.StatusOK,
	}, nil
}

// stringArg pulls a string argument by key, tolerating missing entries.
func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// Compile-time check that Adapter satisfies the dispatch.Adapter interface.
var _ dispatch.Adapter = (*Adapter)(nil)
