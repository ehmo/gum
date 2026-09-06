package googleads

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// rvForService builds a ResolvedVariant for a GoogleAdsService method. Unlike
// the Keyword Planner methods, these hang off the customer as a sub-resource,
// so the request path is /customers/{id}/googleAds:<method>.
func rvForService(method string) *dispatch.ResolvedVariant {
	return &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{
			BackendKind: catalog.BackendKindGoogleAdsSDK,
			Binding: &catalog.Binding{
				AdapterKey: "googleads." + method,
				HTTP: &catalog.HTTPBinding{
					Method: "POST",
					Path:   "https://googleads.googleapis.com/v24/customers/{customerId}/googleAds:" + method,
				},
			},
		},
		AdapterKey: "googleads." + method,
	}
}

// captureServer records the path and decoded body of one request and replies
// with the given JSON.
func captureServer(t *testing.T, reply string) (srv *httptest.Server, path *string, body *map[string]any) {
	t.Helper()
	var gotPath string
	gotBody := map[string]any{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, &gotPath, &gotBody
}

func TestGoogleAdsSearchRequest(t *testing.T) {
	srv, gotPath, gotBody := captureServer(t, `{"results":[{"campaign":{"name":"Core"}}],"nextPageToken":"tok2"}`)

	adapter := NewAdapter(func() string { return "DEV" })
	adapter.BaseURL = srv.URL

	inv := &dispatch.Invocation{
		OpID: "googleads.googleAds.search",
		Args: map[string]any{
			"customerId": "123-456-7890",
			"query":      "SELECT campaign.name FROM campaign",
			"pageToken":  "tok1",
		},
	}
	resp, err := adapter.Execute(context.Background(), inv, rvForService("search"), &dispatch.Credentials{Token: "T"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	if *gotPath != "/customers/1234567890/googleAds:search" {
		t.Errorf("path = %q; want /customers/1234567890/googleAds:search", *gotPath)
	}
	if (*gotBody)["query"] != "SELECT campaign.name FROM campaign" {
		t.Errorf("query = %v", (*gotBody)["query"])
	}
	if (*gotBody)["pageToken"] != "tok1" {
		t.Errorf("pageToken = %v; want tok1", (*gotBody)["pageToken"])
	}
	// page_size is not configurable from v19 on; the adapter must not send one.
	if _, ok := (*gotBody)["pageSize"]; ok {
		t.Errorf("body must not carry pageSize: %v", *gotBody)
	}
}

func TestGoogleAdsSearchRequiresQuery(t *testing.T) {
	adapter := NewAdapter(func() string { return "DEV" })
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{"customerId": "1234567890"}},
		rvForService("search"),
		&dispatch.Credentials{Token: "T"},
	)
	if err == nil || !strings.Contains(err.Error(), "`query` is required") {
		t.Fatalf("err = %v; want query required", err)
	}
}

func TestGoogleAdsMutateRequest(t *testing.T) {
	srv, gotPath, gotBody := captureServer(t, `{"mutateOperationResponses":[{"campaignBudgetResult":{"resourceName":"customers/1/campaignBudgets/2"}}]}`)

	adapter := NewAdapter(func() string { return "DEV" })
	adapter.BaseURL = srv.URL

	ops := []any{
		map[string]any{"campaignBudgetOperation": map[string]any{
			"create": map[string]any{"resourceName": "customers/1234567890/campaignBudgets/-1", "amountMicros": "18000000"},
		}},
		map[string]any{"campaignOperation": map[string]any{
			"create": map[string]any{"name": "Search | US | iOS | Core", "campaignBudget": "customers/1234567890/campaignBudgets/-1"},
		}},
	}
	inv := &dispatch.Invocation{
		OpID: "googleads.googleAds.mutate",
		Args: map[string]any{
			"customerId":          "1234567890",
			"mutateOperations":    ops,
			"validateOnly":        true,
			"responseContentType": "MUTABLE_RESOURCE",
		},
	}
	if _, err := adapter.Execute(context.Background(), inv, rvForService("mutate"), &dispatch.Credentials{Token: "T"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if *gotPath != "/customers/1234567890/googleAds:mutate" {
		t.Errorf("path = %q; want /customers/1234567890/googleAds:mutate", *gotPath)
	}
	sent, _ := (*gotBody)["mutateOperations"].([]any)
	if len(sent) != 2 {
		t.Fatalf("mutateOperations = %v; want 2 entries", (*gotBody)["mutateOperations"])
	}
	// Operations pass through untouched, including the temp resource id that
	// links the campaign to the budget created earlier in the same batch.
	first, _ := sent[0].(map[string]any)
	budget, _ := first["campaignBudgetOperation"].(map[string]any)
	create, _ := budget["create"].(map[string]any)
	if create["resourceName"] != "customers/1234567890/campaignBudgets/-1" {
		t.Errorf("temp resource id not preserved: %v", create)
	}
	if (*gotBody)["validateOnly"] != true {
		t.Errorf("validateOnly = %v; want true", (*gotBody)["validateOnly"])
	}
	if (*gotBody)["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("responseContentType = %v", (*gotBody)["responseContentType"])
	}
	// partialFailure defaults off so the batch stays atomic.
	if _, ok := (*gotBody)["partialFailure"]; ok {
		t.Errorf("partialFailure must be omitted when false: %v", *gotBody)
	}
}

func TestGoogleAdsMutateValidation(t *testing.T) {
	adapter := NewAdapter(func() string { return "DEV" })
	creds := &dispatch.Credentials{Token: "T"}

	cases := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{
			name:    "missing operations",
			args:    map[string]any{"customerId": "1234567890"},
			wantErr: "`mutateOperations` is required",
		},
		{
			name:    "empty operations",
			args:    map[string]any{"customerId": "1234567890", "mutateOperations": []any{}},
			wantErr: "`mutateOperations` is required",
		},
		{
			name:    "non-object operation",
			args:    map[string]any{"customerId": "1234567890", "mutateOperations": []any{"nope"}},
			wantErr: "`mutateOperations`[0] must be an object",
		},
		{
			name:    "not an array",
			args:    map[string]any{"customerId": "1234567890", "mutateOperations": 7},
			wantErr: "`mutateOperations` must be an array of objects",
		},
		{
			name:    "malformed json string",
			args:    map[string]any{"customerId": "1234567890", "mutateOperations": "[{"},
			wantErr: "`mutateOperations` is not a JSON array",
		},
		{
			name: "bad response content type",
			args: map[string]any{
				"customerId":          "1234567890",
				"mutateOperations":    []any{map[string]any{"campaignOperation": map[string]any{}}},
				"responseContentType": "EVERYTHING",
			},
			wantErr: "`responseContentType` must be",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := adapter.Execute(context.Background(),
				&dispatch.Invocation{Args: tc.args}, rvForService("mutate"), creds)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v; want %q", err, tc.wantErr)
			}
		})
	}
}

// The CLI hands array args through as text, so a JSON string must parse.
func TestObjectSliceArgAcceptsJSONString(t *testing.T) {
	got, err := objectSliceArg(map[string]any{
		"mutateOperations": `[{"adGroupOperation":{"create":{"name":"Vault intent"}}}]`,
	}, "mutateOperations")
	if err != nil {
		t.Fatalf("objectSliceArg: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d; want 1", len(got))
	}
	if _, ok := got[0]["adGroupOperation"]; !ok {
		t.Errorf("parsed operation = %v", got[0])
	}
}

func TestResourceURL(t *testing.T) {
	a := &Adapter{BaseURL: "https://example.test/v24"}

	cases := []struct {
		path string
		want string
	}{
		{
			// Keyword Planner shape must be byte-identical to the old builder.
			path: "https://googleads.googleapis.com/v24/customers/{customerId}:generateKeywordIdeas",
			want: "https://example.test/v24/customers/1234567890:generateKeywordIdeas",
		},
		{
			path: "https://googleads.googleapis.com/v24/customers/{customerId}/googleAds:search",
			want: "https://example.test/v24/customers/1234567890/googleAds:search",
		},
	}
	for _, tc := range cases {
		got, err := a.resourceURL(tc.path, "1234567890")
		if err != nil {
			t.Fatalf("resourceURL(%q): %v", tc.path, err)
		}
		if got != tc.want {
			t.Errorf("resourceURL(%q) = %q; want %q", tc.path, got, tc.want)
		}
	}

	if _, err := a.resourceURL("https://googleads.googleapis.com/v24/keywordPlans:generate", "1234567890"); err == nil {
		t.Error("path without /customers/ must fail closed")
	}
	if _, err := a.resourceURL("https://googleads.googleapis.com/v24/customers/{customerId}/adGroups/{adGroupId}:x", "1234567890"); err == nil {
		t.Error("unresolved template parameter must fail closed")
	}
}

func TestGoogleAdsMutateDoesNotRetry(t *testing.T) {
	for _, status := range []int{429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"ambiguous outcome"}}`))
			}))
			defer srv.Close()
			a := NewAdapter(func() string { return "DEV" })
			a.BaseURL = srv.URL
			a.backoffBase = time.Nanosecond
			_, err := a.Execute(context.Background(), &dispatch.Invocation{Args: map[string]any{
				"customerId": "1234567890",
				"mutateOperations": []any{map[string]any{"campaignOperation": map[string]any{
					"remove": "customers/1234567890/campaigns/1",
				}}},
			}}, rvForService("mutate"), &dispatch.Credentials{Token: "T"})
			if err == nil || !strings.Contains(err.Error(), "ambiguous outcome") {
				t.Fatalf("Execute error = %v", err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("mutation sent %d times; want exactly 1", got)
			}
		})
	}
}

func TestGoogleAdsMutateRejectsAmbiguousSafetyFlags(t *testing.T) {
	a := &Adapter{}
	for _, key := range []string{"validateOnly", "partialFailure"} {
		for _, value := range []any{"yes", "", nil, 1, map[string]any{}} {
			args := map[string]any{"mutateOperations": []any{map[string]any{}}, key: value}
			if _, err := a.buildBody("mutate", args); err == nil {
				t.Errorf("%s=%#v must fail before sending a mutation", key, value)
			}
		}
	}
	for _, raw := range []any{map[string]any{"mutateOperations": []any{map[string]any{}}}, `{"mutateOperations":[{}]}`, nil} {
		if _, err := a.buildBody("mutate", map[string]any{"body": raw, "validateOnly": true}); err == nil {
			t.Errorf("raw body %#v must not bypass validateOnly", raw)
		}
	}
}

func TestGoogleAdsUploadClickConversionsRequest(t *testing.T) {
	srv, gotPath, gotBody := captureServer(t, `{"results":[{"gclid":"Cj0KabC"}]}`)

	adapter := NewAdapter(func() string { return "DEV" })
	adapter.BaseURL = srv.URL

	conversions := []any{
		map[string]any{
			"conversionAction":   "customers/1234567890/conversionActions/42",
			"gclid":              "Cj0KabC",
			"conversionDateTime": "2026-09-10 14:05:00+00:00",
			"conversionValue":    1,
			"currencyCode":       "USD",
			"orderId":            "0192f0",
		},
	}
	inv := &dispatch.Invocation{
		OpID: "googleads.conversionUploads.uploadClickConversions",
		Args: map[string]any{"customerId": "1234567890", "conversions": conversions},
	}
	if _, err := adapter.Execute(context.Background(), inv, rvFor("uploadClickConversions"), &dispatch.Credentials{Token: "T"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The custom method hangs off the customer itself, not off a service
	// sub-resource the way googleAds:search and googleAds:mutate do.
	if *gotPath != "/customers/1234567890:uploadClickConversions" {
		t.Errorf("path = %q; want /customers/1234567890:uploadClickConversions", *gotPath)
	}
	// Google documents partialFailure as required here, so the adapter sets it
	// even though the caller never asked. One rejected gclid must not discard
	// the rest of the day.
	if (*gotBody)["partialFailure"] != true {
		t.Errorf("partialFailure = %v; want true", (*gotBody)["partialFailure"])
	}
	sent, _ := (*gotBody)["conversions"].([]any)
	if len(sent) != 1 {
		t.Fatalf("conversions = %v; want 1 entry", (*gotBody)["conversions"])
	}
	first, _ := sent[0].(map[string]any)
	if first["gclid"] != "Cj0KabC" || first["orderId"] != "0192f0" {
		t.Errorf("conversion fields not passed through: %v", first)
	}
	if _, ok := (*gotBody)["validateOnly"]; ok {
		t.Errorf("validateOnly must be omitted when unset: %v", *gotBody)
	}
}

func TestGoogleAdsUploadClickConversionsValidation(t *testing.T) {
	adapter := NewAdapter(func() string { return "DEV" })
	creds := &dispatch.Credentials{Token: "T"}

	cases := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{
			name:    "missing conversions",
			args:    map[string]any{"customerId": "1234567890"},
			wantErr: "`conversions` is required",
		},
		{
			name:    "empty conversions",
			args:    map[string]any{"customerId": "1234567890", "conversions": []any{}},
			wantErr: "`conversions` is required",
		},
		{
			name:    "non-object conversion",
			args:    map[string]any{"customerId": "1234567890", "conversions": []any{"nope"}},
			wantErr: "`conversions`[0] must be an object",
		},
		{
			name: "ambiguous validateOnly",
			args: map[string]any{
				"customerId":   "1234567890",
				"conversions":  []any{map[string]any{"gclid": "x"}},
				"validateOnly": "sure",
			},
			wantErr: "`validateOnly` must be a boolean",
		},
		{
			name: "raw body bypass",
			args: map[string]any{
				"customerId": "1234567890",
				"body":       map[string]any{"conversions": []any{map[string]any{"gclid": "x"}}},
			},
			wantErr: "does not accept `body`",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := adapter.Execute(context.Background(),
				&dispatch.Invocation{Args: tc.args}, rvFor("uploadClickConversions"), creds)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v; want %q", err, tc.wantErr)
			}
		})
	}
}

// A conversion upload is not idempotent: a retried POST can double-count. The
// adapter must send it once, exactly like a mutate.
func TestGoogleAdsUploadClickConversionsDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"ambiguous outcome"}}`))
	}))
	defer srv.Close()

	a := NewAdapter(func() string { return "DEV" })
	a.BaseURL = srv.URL
	a.backoffBase = time.Nanosecond
	_, err := a.Execute(context.Background(), &dispatch.Invocation{Args: map[string]any{
		"customerId":  "1234567890",
		"conversions": []any{map[string]any{"gclid": "Cj0KabC"}},
	}}, rvFor("uploadClickConversions"), &dispatch.Credentials{Token: "T"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous outcome") {
		t.Fatalf("Execute error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("upload sent %d times; want exactly 1", got)
	}
}
