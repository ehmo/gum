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

// rvFor builds a ResolvedVariant bound to the google-ads-sdk adapter for the
// given keyword-planning custom method.
func rvFor(method string) *dispatch.ResolvedVariant {
	return &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{
			BackendKind: catalog.BackendKindGoogleAdsSDK,
			Binding: &catalog.Binding{
				AdapterKey: "googleads." + method,
				HTTP: &catalog.HTTPBinding{
					Method: "POST",
					Path:   "https://googleads.googleapis.com/v24/customers/{customerId}:" + method,
				},
			},
		},
		AdapterKey: "googleads." + method,
	}
}

// TestBackendKindGoogleAdsSDK is the fixture-backed executor contract test for
// the google-ads-sdk backend kind (docs/catalog-abi.md "Backend Kind" step 4).
// It also proves the security-critical invariant: the developer token is
// sourced server-side (the DevToken hook) and reaches the wire as the
// developer-token header WITHOUT ever appearing in inv.Args.
func TestBackendKindGoogleAdsSDK(t *testing.T) {
	var gotPath, gotAuth, gotDevTok, gotLoginCID, gotCT string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotDevTok = r.Header.Get("developer-token")
		gotLoginCID = r.Header.Get("login-customer-id")
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"text":"mars cruise","keywordIdeaMetrics":{"avgMonthlySearches":"74000","competition":"HIGH"}}]}`))
	}))
	defer srv.Close()

	adapter := NewAdapter(func() string { return "DEV-TOKEN-SECRET" })
	adapter.BaseURL = srv.URL

	inv := &dispatch.Invocation{
		OpID: "googleads.keywordPlanIdeas.generateKeywordIdeas",
		Args: map[string]any{
			"customerId":         "123-456-7890", // dashes must be stripped
			"loginCustomerId":    "987-654-3210",
			"keywords":           []any{"mars cruise", "space travel"},
			"geoTargetConstants": []any{"2840"},
			"language":           "1000",
		},
	}
	// Security invariant: the developer token is NOT an invocation arg.
	for k := range inv.Args {
		if strings.Contains(strings.ToLower(k), "developer") || strings.Contains(strings.ToLower(k), "token") {
			t.Fatalf("developer token must never be an invocation arg; found %q", k)
		}
	}

	creds := &dispatch.Credentials{Token: "BEARER-123"}
	resp, err := adapter.Execute(context.Background(), inv, rvFor("generateKeywordIdeas"), creds)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK || resp.Format != "json" {
		t.Fatalf("status=%d format=%q; want 200/json", resp.StatusCode, resp.Format)
	}
	if gotPath != "/customers/1234567890:generateKeywordIdeas" {
		t.Errorf("path = %q; want /customers/1234567890:generateKeywordIdeas (dashes stripped)", gotPath)
	}
	if gotAuth != "Bearer BEARER-123" {
		t.Errorf("Authorization = %q; want Bearer BEARER-123", gotAuth)
	}
	if gotDevTok != "DEV-TOKEN-SECRET" {
		t.Errorf("developer-token = %q; want DEV-TOKEN-SECRET (from hook, not args)", gotDevTok)
	}
	if gotLoginCID != "9876543210" {
		t.Errorf("login-customer-id = %q; want 9876543210", gotLoginCID)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("Content-Type = %q; want application/json", gotCT)
	}
	seed, _ := gotBody["keywordSeed"].(map[string]any)
	kws, _ := seed["keywords"].([]any)
	if len(kws) != 2 {
		t.Errorf("keywordSeed.keywords = %v; want 2 entries\nbody=%v", kws, gotBody)
	}
	if gotBody["geoTargetConstants"].([]any)[0] != "geoTargetConstants/2840" {
		t.Errorf("geoTargetConstants not normalized: %v", gotBody["geoTargetConstants"])
	}
	if gotBody["language"] != "languageConstants/1000" {
		t.Errorf("language = %v; want languageConstants/1000", gotBody["language"])
	}
}

func TestGoogleAdsRequiresOAuthToken(t *testing.T) {
	adapter := NewAdapter(func() string { return "DEV" })
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{"customerId": "1234567890", "keywords": []any{"x"}}},
		rvFor("generateKeywordIdeas"),
		&dispatch.Credentials{}, // no Token
	)
	if err == nil || !strings.Contains(err.Error(), "OAuth token") {
		t.Fatalf("err = %v; want missing OAuth token", err)
	}
}

func TestGoogleAdsRequiresDeveloperToken(t *testing.T) {
	adapter := NewAdapter(func() string { return "" }) // no dev token
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{"customerId": "1234567890", "keywords": []any{"x"}}},
		rvFor("generateKeywordIdeas"),
		&dispatch.Credentials{Token: "B"},
	)
	if err == nil || !strings.Contains(err.Error(), "developer token") {
		t.Fatalf("err = %v; want missing developer token", err)
	}
}

func TestGoogleAdsRequiresCustomerID(t *testing.T) {
	adapter := NewAdapter(func() string { return "DEV" })
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{"keywords": []any{"x"}}},
		rvFor("generateKeywordIdeas"),
		&dispatch.Credentials{Token: "B"},
	)
	if err == nil || !strings.Contains(err.Error(), "customerId") {
		t.Fatalf("err = %v; want missing customerId", err)
	}
}

func TestGoogleAdsHistoricalRequiresKeywords(t *testing.T) {
	adapter := NewAdapter(func() string { return "DEV" })
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{"customerId": "1234567890"}},
		rvFor("generateKeywordHistoricalMetrics"),
		&dispatch.Credentials{Token: "B"},
	)
	if err == nil || !strings.Contains(err.Error(), "keywords") {
		t.Fatalf("err = %v; want needs keywords", err)
	}
}

func TestGoogleAdsForecastBody(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"campaignForecastMetrics":{"clicks":1.5}}`))
	}))
	defer srv.Close()

	adapter := NewAdapter(func() string { return "DEV" })
	adapter.BaseURL = srv.URL
	adapter.now = func() time.Time { return time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC) }

	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{
			"customerId": "1234567890",
			"keywords":   []any{"mars cruise"},
		}},
		rvFor("generateKeywordForecastMetrics"),
		&dispatch.Credentials{Token: "B"},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	campaign, _ := gotBody["campaign"].(map[string]any)
	if campaign == nil {
		t.Fatalf("missing campaign in body: %v", gotBody)
	}
	bid, _ := campaign["biddingStrategy"].(map[string]any)
	mcpc, _ := bid["manualCpcBiddingStrategy"].(map[string]any)
	if mcpc["maxCpcBidMicros"].(float64) != 1_000_000 {
		t.Errorf("default maxCpcBidMicros = %v; want 1000000", mcpc["maxCpcBidMicros"])
	}
	if _, bad := campaign["keywordPlanNetwork"]; bad {
		t.Error("campaign must NOT carry keywordPlanNetwork (not a CampaignToForecast field)")
	}
	// Ad group keywords are KeywordInfo {text, matchType} under `keywords`, not
	// `biddableKeywords`, and carry no per-keyword/per-ad-group bid.
	ags, _ := campaign["adGroups"].([]any)
	if len(ags) != 1 {
		t.Fatalf("adGroups = %v; want 1", campaign["adGroups"])
	}
	ag0, _ := ags[0].(map[string]any)
	if _, bad := ag0["biddableKeywords"]; bad {
		t.Error("ad group must use `keywords` (KeywordInfo), not `biddableKeywords`")
	}
	kwList, _ := ag0["keywords"].([]any)
	if len(kwList) != 1 {
		t.Fatalf("ad group keywords = %v; want 1", ag0["keywords"])
	}
	kw0, _ := kwList[0].(map[string]any)
	if kw0["text"] != "mars cruise" || kw0["matchType"] != "BROAD" {
		t.Errorf("keyword = %v; want {text:mars cruise, matchType:BROAD}", kw0)
	}
	// forecastPeriod carries startDate/endDate directly (no dateRange wrapper).
	fp, _ := gotBody["forecastPeriod"].(map[string]any)
	if _, bad := fp["dateRange"]; bad {
		t.Error("forecastPeriod must carry startDate/endDate directly, not nested under dateRange")
	}
	if fp["startDate"] != "2026-06-06" || fp["endDate"] != "2026-07-05" {
		t.Errorf("default forecast window = %v..%v; want 2026-06-06..2026-07-05", fp["startDate"], fp["endDate"])
	}
}

func TestGoogleAdsRawBodyPassthrough(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	adapter := NewAdapter(func() string { return "DEV" })
	adapter.BaseURL = srv.URL
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{
			"customerId": "1234567890",
			"body":       map[string]any{"custom": "shape", "keywordSeed": map[string]any{"keywords": []any{"x"}}},
		}},
		rvFor("generateKeywordIdeas"),
		&dispatch.Credentials{Token: "B"},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotBody["custom"] != "shape" {
		t.Errorf("raw body not passed through verbatim: %v", gotBody)
	}
}

func TestGoogleAdsUpstreamError429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"message":"Resource exhausted","status":"RESOURCE_EXHAUSTED"}}`))
	}))
	defer srv.Close()

	adapter := NewAdapter(func() string { return "DEV" })
	adapter.BaseURL = srv.URL
	adapter.MaxAttempts = 1 // disable retry so the test doesn't back off on 429
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{"customerId": "1234567890", "keywords": []any{"x"}}},
		rvFor("generateKeywordIdeas"),
		&dispatch.Credentials{Token: "B"},
	)
	ue, ok := err.(*upstreamError)
	if !ok {
		t.Fatalf("err = %T %v; want *upstreamError", err, err)
	}
	if ue.HTTPStatusCode() != 429 {
		t.Errorf("HTTPStatusCode = %d; want 429", ue.HTTPStatusCode())
	}
	if ue.RetryAfterMs() != 5000 {
		t.Errorf("RetryAfterMs = %d; want 5000", ue.RetryAfterMs())
	}
	if !strings.Contains(ue.Error(), "Resource exhausted") {
		t.Errorf("Error = %q; want upstream message", ue.Error())
	}
}

func TestGoogleAdsRetriesOn503(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":503,"message":"backend"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	adapter := NewAdapter(func() string { return "DEV" })
	adapter.BaseURL = srv.URL
	adapter.MaxAttempts = 3
	adapter.backoffBase = time.Millisecond // keep the test fast
	resp, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{"customerId": "1234567890", "keywords": []any{"x"}}},
		rvFor("generateKeywordIdeas"),
		&dispatch.Credentials{Token: "B"},
	)
	if err != nil {
		t.Fatalf("Execute after retries: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d; want 200 after retry", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls = %d; want 3 (two 503s then 200)", got)
	}
}

func TestGoogleAdsValidationErrors(t *testing.T) {
	adapter := NewAdapter(func() string { return "DEV" })
	creds := &dispatch.Credentials{Token: "B"}
	manyKeywords := func(n int) []any {
		out := make([]any, n)
		for i := range out {
			out[i] = "kw"
		}
		return out
	}
	cases := []struct {
		name, method, want string
		args               map[string]any
	}{
		{"short customerId", "generateKeywordIdeas", "exactly 10 digits", map[string]any{"customerId": "123", "keywords": []any{"x"}}},
		{"bad loginCustomerId", "generateKeywordIdeas", "loginCustomerId", map[string]any{"customerId": "1234567890", "loginCustomerId": "abc", "keywords": []any{"x"}}},
		{"bad network", "generateKeywordIdeas", "keywordPlanNetwork", map[string]any{"customerId": "1234567890", "keywords": []any{"x"}, "keywordPlanNetwork": "BING"}},
		{"bad matchType", "generateKeywordForecastMetrics", "matchType", map[string]any{"customerId": "1234567890", "keywords": []any{"x"}, "matchType": "FUZZY"}},
		{"too many seeds", "generateKeywordIdeas", "at most 20", map[string]any{"customerId": "1234567890", "keywords": manyKeywords(21)}},
		{"bad date format", "generateKeywordForecastMetrics", "YYYY-MM-DD", map[string]any{"customerId": "1234567890", "keywords": []any{"x"}, "forecastStartDate": "06/01/2026", "forecastEndDate": "2026-07-01"}},
		{"end before start", "generateKeywordForecastMetrics", "before", map[string]any{"customerId": "1234567890", "keywords": []any{"x"}, "forecastStartDate": "2026-07-01", "forecastEndDate": "2026-06-01"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := adapter.Execute(context.Background(),
				&dispatch.Invocation{Args: tc.args}, rvFor(tc.method), creds)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v; want substring %q", err, tc.want)
			}
		})
	}
}

func TestGoogleAdsRejectsUnknownMethod(t *testing.T) {
	adapter := NewAdapter(func() string { return "DEV" })
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{"customerId": "1234567890"}},
		rvFor("deleteEverything"),
		&dispatch.Credentials{Token: "B"},
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported method") {
		t.Fatalf("err = %v; want unsupported method", err)
	}
}

// TestGoogleAdsDevTokenNotInError proves neither the developer token nor the
// OAuth Bearer ever appears in an error surfaced to the caller.
func TestGoogleAdsDevTokenNotInError(t *testing.T) {
	const devSecret = "SUPER-SECRET-DEV-TOKEN-zzz"
	const bearerSecret = "BEARER-SECRET-yyy"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad request"}}`))
	}))
	defer srv.Close()

	adapter := NewAdapter(func() string { return devSecret })
	adapter.BaseURL = srv.URL
	adapter.MaxAttempts = 1
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{"customerId": "1234567890", "keywords": []any{"x"}}},
		rvFor("generateKeywordIdeas"),
		&dispatch.Credentials{Token: bearerSecret},
	)
	if err == nil {
		t.Fatal("want an upstream error")
	}
	if strings.Contains(err.Error(), devSecret) || strings.Contains(err.Error(), bearerSecret) {
		t.Errorf("error leaks a secret: %q", err.Error())
	}
}

func TestCustomMethod(t *testing.T) {
	cases := map[string]string{
		"https://googleads.googleapis.com/v24/customers/{customerId}:generateKeywordIdeas": "generateKeywordIdeas",
		"/customers/1:generateKeywordHistoricalMetrics":                                    "generateKeywordHistoricalMetrics",
		"no-colon":  "",
		"trailing:": "",
	}
	for in, want := range cases {
		if got := customMethod(in); got != want {
			t.Errorf("customMethod(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestNormalizeCustomerID(t *testing.T) {
	ok := map[string]string{"123-456-7890": "1234567890", "1234567890": "1234567890", " 12 34 ": "1234", "": ""}
	for in, want := range ok {
		got, err := normalizeCustomerID(in)
		if err != nil {
			t.Errorf("normalizeCustomerID(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("normalizeCustomerID(%q) = %q; want %q", in, got, want)
		}
	}
	if _, err := normalizeCustomerID("12ab34"); err == nil {
		t.Error("normalizeCustomerID(12ab34): want error for non-digits")
	}
}

func TestArgCoercion(t *testing.T) {
	args := map[string]any{
		"keywords": []any{"a", "", " b "},
		"geoCSV":   "2840, geoTargetConstants/2826",
	}
	if got := stringSliceArg(args, "keywords"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("stringSliceArg trimmed = %v; want [a b]", got)
	}
	geo := geoConstantsArg(map[string]any{"geoTargetConstants": "2840, geoTargetConstants/2826"})
	if len(geo) != 2 || geo[0] != "geoTargetConstants/2840" || geo[1] != "geoTargetConstants/2826" {
		t.Errorf("geoConstantsArg = %v; want normalized resource names", geo)
	}
	if n, err := networkArg(map[string]any{}); err != nil || n != "GOOGLE_SEARCH" {
		t.Errorf("networkArg default = %q, %v; want GOOGLE_SEARCH, nil", n, err)
	}
	if languageConstantArg(map[string]any{"language": "1000"}) != "languageConstants/1000" {
		t.Error("languageConstantArg should normalize bare id")
	}
}
