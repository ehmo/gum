package googleads

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestInt64Arg(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want int64
	}{
		{"int", 5, 5},
		{"int64", int64(7), 7},
		{"float64", float64(9), 9},
		{"json.Number", json.Number("11"), 11},
		{"string", "13", 13},
		{"bad string", "nope", 0},
		{"absent", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{}
			if tc.v != nil {
				args["k"] = tc.v
			}
			if got := int64Arg(args, "k"); got != tc.want {
				t.Errorf("int64Arg(%v) = %d; want %d", tc.v, got, tc.want)
			}
		})
	}
}

func TestBoolArg(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"1", true},
		{"false", false},
		{"garbage", false},
		{nil, false},
		{42, false},
	}
	for _, tc := range cases {
		args := map[string]any{}
		if tc.v != nil {
			args["k"] = tc.v
		}
		if got := boolArg(args, "k"); got != tc.want {
			t.Errorf("boolArg(%v) = %v; want %v", tc.v, got, tc.want)
		}
	}
}

func TestRawBodyArg(t *testing.T) {
	if _, ok := rawBodyArg(map[string]any{}); ok {
		t.Error("absent body should be ok=false")
	}
	if _, ok := rawBodyArg(map[string]any{"body": map[string]any{}}); ok {
		t.Error("empty map body should be ok=false")
	}
	if m, ok := rawBodyArg(map[string]any{"body": map[string]any{"x": 1}}); !ok || m["x"] != 1 {
		t.Errorf("map body = %v, ok=%v; want passthrough", m, ok)
	}
	if _, ok := rawBodyArg(map[string]any{"body": "  "}); ok {
		t.Error("blank string body should be ok=false")
	}
	if m, ok := rawBodyArg(map[string]any{"body": `{"a":2}`}); !ok || m["a"].(float64) != 2 {
		t.Errorf("json string body = %v, ok=%v; want parsed", m, ok)
	}
	if _, ok := rawBodyArg(map[string]any{"body": `not json`}); ok {
		t.Error("non-JSON string body should be ok=false")
	}
}

func TestHistoricalBodyValidation(t *testing.T) {
	a := &Adapter{}
	if _, err := a.historicalBody(map[string]any{}); err == nil {
		t.Error("missing keywords should error")
	}
	tooMany := make([]any, maxKeywords+1)
	for i := range tooMany {
		tooMany[i] = "k"
	}
	if _, err := a.historicalBody(map[string]any{"keywords": tooMany}); err == nil {
		t.Error("over-limit keywords should error")
	}
	body, err := a.historicalBody(map[string]any{
		"keywords":           []any{"shoes", "boots"},
		"geoTargetConstants": []any{"2840"},
		"languageConstant":   "1000",
	})
	if err != nil {
		t.Fatalf("historicalBody: %v", err)
	}
	if _, ok := body["keywords"]; !ok {
		t.Errorf("body missing keywords: %v", body)
	}
	if body["keywordPlanNetwork"] == nil {
		t.Errorf("body missing keywordPlanNetwork: %v", body)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("empty = %d; want 0", got)
	}
	if got := parseRetryAfter("2"); got != 2000 {
		t.Errorf("\"2\" = %d; want 2000", got)
	}
	if got := parseRetryAfter("-5"); got != 0 {
		t.Errorf("negative = %d; want 0", got)
	}
	if got := parseRetryAfter("garbage"); got != 0 {
		t.Errorf("garbage = %d; want 0", got)
	}
	// An HTTP-date in the past yields 0.
	if got := parseRetryAfter("Mon, 02 Jan 2006 15:04:05 GMT"); got != 0 {
		t.Errorf("past date = %d; want 0", got)
	}
}

func TestNewUpstreamError(t *testing.T) {
	body := []byte(`{"error":{"code":429,"message":"quota exhausted","status":"RESOURCE_EXHAUSTED"}}`)
	h := http.Header{}
	h.Set("Retry-After", "3")
	e := newUpstreamError(429, body, h)
	if e.HTTPStatusCode() != 429 {
		t.Errorf("status = %d; want 429", e.HTTPStatusCode())
	}
	if e.RetryAfterMs() != 3000 {
		t.Errorf("retryMs = %d; want 3000", e.RetryAfterMs())
	}
	if e.Error() == "" {
		t.Error("Error() empty")
	}

	// Non-JSON body falls back to a truncated raw message.
	e2 := newUpstreamError(500, []byte("internal boom"), http.Header{})
	if e2.Error() == "" {
		t.Error("fallback Error() empty")
	}
}
