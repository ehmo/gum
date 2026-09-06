package googleads

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/dispatch"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type brokenResponseBody struct{ closed bool }

func (*brokenResponseBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (b *brokenResponseBody) Close() error           { b.closed = true; return nil }

func TestExecuteRejectsInvalidBindingAndBody(t *testing.T) {
	for _, tc := range []struct {
		name, path, base, want string
		body                   any
	}{
		{name: "marshal", body: map[string]any{"bad": make(chan int)}, want: "marshal request body"},
		{name: "path", path: "/other/{customerId}:generateKeywordIdeas", want: "has no /customers/ segment"},
		{name: "request", base: ":invalid", want: "build request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAdapter(func() string { return "DEV" })
			a.BaseURL = tc.base
			a.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid request reached transport")
				return nil, nil
			})}
			rv := rvFor("generateKeywordIdeas")
			if tc.path != "" {
				rv.Variant.Binding.HTTP.Path = tc.path
			}
			args := map[string]any{"customerId": "1234567890", "keywords": "one"}
			if tc.body != nil {
				args["body"] = tc.body
			}
			_, err := a.Execute(context.Background(), &dispatch.Invocation{Args: args}, rv, &dispatch.Credentials{Token: "T"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v; want %s", err, tc.want)
			}
		})
	}
	_, err := (&Adapter{}).Execute(context.Background(), nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "binding missing") {
		t.Fatalf("nil binding error = %v", err)
	}
}

func TestExecuteTransportFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		body io.ReadCloser
		err  error
		want string
	}{
		{name: "transport", err: io.ErrClosedPipe, want: "closed pipe"},
		{name: "read", body: &brokenResponseBody{}, want: "read response"},
		{name: "oversize", body: io.NopCloser(strings.NewReader("12345")), want: "exceeds 4 byte cap"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAdapter(func() string { return "DEV" })
			a.MaxResponseBytes = 4
			a.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: 200, Body: tc.body, Header: make(http.Header)}, nil
			})}
			_, err := a.Execute(context.Background(), &dispatch.Invocation{Args: map[string]any{
				"customerId": "1234567890", "query": "SELECT campaign.name FROM campaign",
			}}, rvForService("search"), &dispatch.Credentials{Token: "T"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v; want %s", err, tc.want)
			}
			if body, ok := tc.body.(*brokenResponseBody); ok && !body.closed {
				t.Error("response body not closed after read failure")
			}
		})
	}
}

func TestRetryCancellationAndRequestFailure(t *testing.T) {
	a := &Adapter{}
	_, _, _, err := a.doWithRetry(context.Background(), "search", func() (*http.Request, error) {
		return nil, io.ErrClosedPipe
	}, 100)
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("request error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
	})}
	_, _, _, err = a.doWithRetry(ctx, "search", func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, DefaultBaseURL, nil)
	}, 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retry cancellation = %v", err)
	}
}

func TestBackoffBounds(t *testing.T) {
	a := &Adapter{}
	for _, tc := range []struct {
		attempt int
		header  string
		want    time.Duration
	}{
		{1, "", defaultBackoffBase}, {2, "", time.Second},
		{1, "2", 2 * time.Second}, {8, "", maxBackoff}, {1, "90", maxBackoff},
	} {
		if got := a.backoff(tc.attempt, tc.header); got != tc.want {
			t.Errorf("backoff(%d, %q) = %v; want %v", tc.attempt, tc.header, got, tc.want)
		}
	}
	if (&Adapter{}).baseURL() != DefaultBaseURL {
		t.Fatal("default URL changed")
	}
	before := time.Now()
	if got := a.clock(); got.Before(before) || got.After(time.Now()) {
		t.Errorf("default clock = %v", got)
	}
}

func TestPlannerOptionalFields(t *testing.T) {
	a := &Adapter{}
	args := map[string]any{
		"keywords": []string{"shoes"}, "url": "https://example.test/",
		"keywordPlanNetwork": "GOOGLE_SEARCH_AND_PARTNERS", "language": "languageConstants/1000",
		"geoTargetConstants": "2840", "includeAdultKeywords": true, "pageSize": 10, "pageToken": "next",
	}
	ideas, err := a.buildBody("generateKeywordIdeas", args)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"includeAdultKeywords": true, "pageSize": 10, "pageToken": "next", "language": "languageConstants/1000",
		"keywordAndUrlSeed": map[string]any{"url": "https://example.test/", "keywords": []string{"shoes"}},
	} {
		if !reflect.DeepEqual(ideas[key], want) {
			t.Errorf("ideas[%s] = %#v; want %#v", key, ideas[key], want)
		}
	}
	delete(args, "keywords")
	ideas, err = a.ideasBody(args)
	if err != nil || !reflect.DeepEqual(ideas["urlSeed"], map[string]any{"url": "https://example.test/"}) {
		t.Fatalf("URL seed = %v, %v", ideas, err)
	}
	args["keywords"] = []string{"shoes"}
	for _, method := range []string{"generateKeywordHistoricalMetrics", "generateKeywordForecastMetrics"} {
		body, err := a.buildBody(method, args)
		if err != nil {
			t.Fatal(err)
		}
		if method == "generateKeywordForecastMetrics" {
			body = body["campaign"].(map[string]any)
			if !reflect.DeepEqual(body["languageConstants"], []string{"languageConstants/1000"}) {
				t.Errorf("forecast language = %v", body)
			}
		} else if body["language"] != "languageConstants/1000" {
			t.Errorf("historical language = %v", body)
		}
		if !reflect.DeepEqual(body["geoTargetConstants"], []string{"geoTargetConstants/2840"}) {
			t.Errorf("geo targets = %v", body)
		}
	}
}

func TestPlannerAndAccountValidationEdges(t *testing.T) {
	a := &Adapter{}
	tooMany := make([]string, maxKeywords+1)
	for i := range tooMany {
		tooMany[i] = "keyword"
	}
	for _, tc := range []struct {
		method, want string
		args         map[string]any
	}{
		{"unknown", "unsupported method", nil},
		{"generateKeywordIdeas", "needs `keywords`", nil},
		{"generateKeywordHistoricalMetrics", "keywordPlanNetwork", map[string]any{"keywords": "one", "keywordPlanNetwork": "bad"}},
		{"generateKeywordForecastMetrics", "needs `keywords`", nil},
		{"generateKeywordForecastMetrics", "at most", map[string]any{"keywords": tooMany}},
		{"generateKeywordForecastMetrics", "forecastEndDate", map[string]any{"keywords": "one", "forecastStartDate": "2026-09-10", "forecastEndDate": "invalid"}},
	} {
		if _, err := a.buildBody(tc.method, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s error = %v; want %s", tc.method, err, tc.want)
		}
	}
	if _, err := requireAccountID("abc", "customerId"); err == nil {
		t.Error("accepted non-numeric customer id")
	}
	if _, err := optionalAccountID("123", "loginCustomerId"); err == nil {
		t.Error("accepted short manager id")
	}
	if _, err := normalizeCustomerID(" - - "); err == nil {
		t.Error("accepted account id with no digits")
	}
}

func TestMutateBatchLimitsAndOptions(t *testing.T) {
	ops := make([]any, maxMutateOperations)
	for i := range ops {
		ops[i] = map[string]any{"campaignOperation": map[string]any{"remove": "customers/1234567890/campaigns/1"}}
	}
	a := &Adapter{}
	body, err := a.mutateBody(map[string]any{"mutateOperations": ops, "partialFailure": "true", "validateOnly": "false"})
	if err != nil || body["partialFailure"] != true {
		t.Fatalf("batch at limit = %v, %v", body, err)
	}
	if _, ok := body["validateOnly"]; ok {
		t.Error("false validateOnly should be omitted")
	}
	if _, err := a.mutateBody(map[string]any{"mutateOperations": append(ops, map[string]any{})}); err == nil {
		t.Error("accepted oversized mutation batch")
	}
	if _, err := a.mutateBody(map[string]any{"mutateOperations": "  "}); err == nil {
		t.Error("accepted blank mutation batch")
	}
}

func TestUpstreamErrorEdges(t *testing.T) {
	e := newUpstreamError(502, []byte(strings.Repeat("x", 600)), nil)
	if len(e.message) != 512 {
		t.Errorf("fallback message length = %d; want 512", len(e.message))
	}
	deadline := time.Now().Add(time.Minute).Truncate(time.Second)
	ms := parseRetryAfter(deadline.UTC().Format(http.TimeFormat))
	if ms <= 0 || ms > 60000 {
		t.Errorf("future Retry-After = %d ms", ms)
	}
	failure := googleAdsFailError{}
	failure.Location.FieldPathElements = []fieldPathElement{{FieldName: "operations"}}
	if got := failureLine(failure); got != "operations" {
		t.Errorf("location-only failure = %q", got)
	}
}
