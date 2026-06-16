package adapters_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// makePOSTInvAndVariant produces an Invocation + ResolvedVariant whose binding
// targets baseURL+"/v1/things" with HTTP POST. Used by body-support tests.
func makePOSTInvAndVariant(baseURL string, args map[string]any) (*dispatch.Invocation, *dispatch.ResolvedVariant) {
	inv := &dispatch.Invocation{
		OpID:      "test.write",
		Args:      args,
		Format:    "json",
		RequestID: "test-req-body",
	}
	rv := &dispatch.ResolvedVariant{
		OpID:       "test.write",
		AdapterKey: "rest.typed-rest-sdk",
		Variant: &catalog.Variant{
			VariantID:     "test.write.v1",
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindDiscoveryREST,
			BackendKind:   catalog.BackendKindTypedRestSDK,
			RiskClass:     catalog.RiskClassWrite,
			Binding: &catalog.Binding{
				BindingSchemaVersion: 1,
				AdapterKey:           "rest.typed-rest-sdk",
				OperationKey:         "test.write",
				HTTP: &catalog.HTTPBinding{
					Method: "POST",
					Path:   baseURL + "/v1/things",
				},
			},
		},
	}
	return inv, rv
}

// TestTypedRestSDKSendsJSONBody verifies that a POST invocation with
// args["body"] = map[...]any sends the marshalled JSON to the server, sets
// Content-Type: application/json, and that the reserved key is NOT smuggled into
// the query string.
func TestTypedRestSDKSendsJSONBody(t *testing.T) {
	verifyNoLeaks(t)

	var gotContentType string
	var gotQuery string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotQuery = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	inv, rv := makePOSTInvAndVariant(srv.URL, map[string]any{
		"body": map[string]any{
			"startDate":  "2026-04-01",
			"endDate":    "2026-04-30",
			"dimensions": []string{"query", "page"},
			"rowLimit":   100,
		},
		"alt": "json", // query param — must NOT be swallowed by body key
	})
	creds := &dispatch.Credentials{Token: "fake-tok"}

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	resp, err := ex.Execute(t.Context(), inv, rv, creds)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotQuery != "alt=json" {
		t.Errorf("RawQuery = %q, want alt=json (body must not leak to query)", gotQuery)
	}
	var decoded map[string]any
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("decode received body: %v\nbody=%s", err, gotBody)
	}
	if decoded["startDate"] != "2026-04-01" {
		t.Errorf("body.startDate = %v, want 2026-04-01", decoded["startDate"])
	}
	if decoded["rowLimit"].(float64) != 100 {
		t.Errorf("body.rowLimit = %v, want 100", decoded["rowLimit"])
	}
}

// TestTypedRestSDKBodyIgnoredForGET verifies that "body" is reserved even for
// GET — it never becomes a query parameter and no request body is sent.
func TestTypedRestSDKBodyIgnoredForGET(t *testing.T) {
	verifyNoLeaks(t)

	var gotQuery string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	inv := &dispatch.Invocation{
		OpID:   "test.read",
		Args:   map[string]any{"body": map[string]any{"x": 1}, "fields": "items"},
		Format: "json",
	}
	rv := &dispatch.ResolvedVariant{
		OpID: "test.read",
		Variant: &catalog.Variant{
			VariantID:     "test.read.v1",
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindDiscoveryREST,
			BackendKind:   catalog.BackendKindTypedRestSDK,
			RiskClass:     catalog.RiskClassRead,
			Binding: &catalog.Binding{
				BindingSchemaVersion: 1,
				AdapterKey:           "rest.typed-rest-sdk",
				OperationKey:         "test.read",
				HTTP: &catalog.HTTPBinding{
					Method: "GET",
					Path:   srv.URL + "/v1/things",
				},
			},
		},
	}

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	if _, err := ex.Execute(t.Context(), inv, rv, &dispatch.Credentials{Token: "x"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotQuery != "fields=items" {
		t.Errorf("query = %q, want fields=items (body must not appear)", gotQuery)
	}
	if len(gotBody) != 0 {
		t.Errorf("expected empty request body for GET, got %q", gotBody)
	}
}

// TestTypedRestSDKSendsQuotaProjectHeader verifies that when
// dispatch.Credentials.QuotaProjectID is non-empty the adapter forwards it as
// X-Goog-User-Project. Search Console + several other Google APIs require this
// when the caller is using user ADC.
func TestTypedRestSDKSendsQuotaProjectHeader(t *testing.T) {
	verifyNoLeaks(t)

	var gotQP string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQP = r.Header.Get("X-Goog-User-Project")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	inv, rv := makePOSTInvAndVariant(srv.URL, map[string]any{"body": map[string]any{"q": "x"}})
	creds := &dispatch.Credentials{Token: "tok", QuotaProjectID: "my-gcp-project"}

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	if _, err := ex.Execute(t.Context(), inv, rv, creds); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotQP != "my-gcp-project" {
		t.Errorf("X-Goog-User-Project = %q, want my-gcp-project", gotQP)
	}
}

// TestTypedRestSDKBodyAsString verifies that args["body"] supplied as a
// pre-serialised JSON string is sent verbatim.
func TestTypedRestSDKBodyAsString(t *testing.T) {
	verifyNoLeaks(t)

	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	raw := `{"inspectionUrl":"https://example.com/page","siteUrl":"sc-domain:example.com"}`
	inv, rv := makePOSTInvAndVariant(srv.URL, map[string]any{"body": raw})

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	if _, err := ex.Execute(t.Context(), inv, rv, &dispatch.Credentials{Token: "x"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(gotBody) != raw {
		t.Errorf("body = %q, want %q", gotBody, raw)
	}
}
