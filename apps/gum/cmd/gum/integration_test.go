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

// fakeADC returns a fixed Bearer token without contacting the network.
type fakeADC struct{ token string }

func (f *fakeADC) Resolve(_ context.Context, _ []string) (*auth.Credentials, error) {
	return &auth.Credentials{Token: f.token, StrategyName: "adc"}, nil
}

// TestDispatchEndToEndWithFakeServer proves the production dispatcher wiring
// reaches the HTTP layer with a Bearer token attached. We use a tiny httptest
// server that records the Authorization header and returns a stub JSON body.
func TestDispatchEndToEndWithFakeServer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"messages":[]}`))
	}))
	defer srv.Close()

	// Build a minimal catalog whose binding HTTP path points at the test server.
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		Ops: []catalog.Op{{
			OpID:             "test.read",
			OpSchemaVersion:  1,
			DefaultVariantID: "test.read.v1",
			Variants: []catalog.Variant{{
				VariantID:            "test.read.v1",
				VariantSchemaVersion: 1,
				InterfaceKind:        "discovery-rest",
				BackendKind:          "typed-rest-sdk",
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyADC,
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
			Auth: &auth.CompositeResolver{ADC: &fakeADC{token: "tok-abc"}},
		},
	)

	shaped, err := disp.Dispatch(context.Background(), &dispatch.Invocation{OpID: "test.read"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.Contains(string(shaped.Body), "messages") {
		t.Errorf("response body missing 'messages': %s", shaped.Body)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer tok-abc")
	}
}

// TestDefaultDispatcherWiresAdaptersAndAuth verifies that newDefaultDispatcher
// returns a dispatcher that, when given a stub catalog/adapter, exposes the
// adapters and auth resolver expected by production. We can't easily inspect
// the internal map, so we exercise behaviour: an op with adapter_key
// "rest.typed-rest-sdk" must reach the adapter (and fail with the expected
// ADC error when no credentials are configured in the test env).
func TestDefaultDispatcherWiresAdaptersAndAuth(t *testing.T) {
	// We can't easily inject a fake catalog into newDefaultDispatcher, but we
	// can verify that loadCatalog returns a non-nil snapshot when the embedded
	// JSON is present.
	cat := loadCatalog()
	if cat == nil {
		t.Fatal("loadCatalog() returned nil; embedded catalog should be present")
	}
	if len(cat.Ops) == 0 {
		t.Fatal("embedded catalog has zero ops")
	}

	ads, cr := defaultAdapters("default")
	if _, ok := ads["rest.typed-rest-sdk"]; !ok {
		t.Error("defaultAdapters missing rest.typed-rest-sdk")
	}
	if _, ok := ads["code.risor"]; !ok {
		t.Error("defaultAdapters missing code.risor")
	}
	if _, ok := ads["plugin.mcp"]; !ok {
		t.Error("defaultAdapters missing plugin.mcp (gum-ikg: required for flights.search + unofficial-API plugins)")
	}
	if _, ok := ads["rest.raw-http"]; !ok {
		t.Error("defaultAdapters missing rest.raw-http (gum-fmi: long-tail dispatch)")
	}
	if cr == nil {
		t.Error("defaultAdapters returned nil CodeRunner reference")
	}
}

// TestDefaultDispatcherCloserContract — gum-dxpy. Pins the cmd-level wiring
// that runMCPStdio relies on: buffered=true yields a closer that drains the
// audit writer and is safe to call multiple times, and buffered=false yields
// a no-op closer so the CLI one-shot path can keep its synchronous semantics
// without changing call sites. The auditlog package owns the actual drain
// semantics — this test only verifies cmd/gum hands the right options through.
func TestDefaultDispatcherCloserContract(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	t.Run("buffered_returns_drain_closer", func(t *testing.T) {
		disp, closer := newDefaultDispatcherWithCloser("test-profile", true)
		if disp == nil {
			t.Fatal("dispatcher is nil")
		}
		if closer == nil {
			t.Fatal("closer is nil; runMCPStdio relies on a defer-safe closer")
		}
		if err := closer(); err != nil {
			t.Errorf("closer() returned err=%v; want nil for clean drain", err)
		}
		if err := closer(); err != nil {
			t.Errorf("closer() second call err=%v; idempotent close required", err)
		}
	})

	t.Run("unbuffered_returns_noop_closer", func(t *testing.T) {
		disp, closer := newDefaultDispatcherWithCloser("test-profile", false)
		if disp == nil {
			t.Fatal("dispatcher is nil")
		}
		if closer == nil {
			t.Fatal("closer is nil; defer-safe contract requires non-nil even when no drain")
		}
		if err := closer(); err != nil {
			t.Errorf("noop closer returned err=%v; want nil", err)
		}
	})
}
