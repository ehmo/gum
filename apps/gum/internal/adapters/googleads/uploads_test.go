package googleads

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/dispatch"
)

func TestClickConversionsRequestAndPartialFailure(t *testing.T) {
	const reply = `{"results":[{}, {"gclid":"accepted"}],"partialFailureError":{"code":3,"message":"one rejected conversion"},"jobId":"123"}`
	srv, path, body := captureServer(t, reply)
	a := NewAdapter(func() string { return "DEV" })
	a.BaseURL = srv.URL
	conversions := []any{
		map[string]any{"gclid": "rejected", "conversionAction": "customers/1234567890/conversionActions/1", "conversionDateTime": "2026-09-01 12:00:00+00:00"},
		map[string]any{"gclid": "accepted", "conversionAction": "customers/1234567890/conversionActions/1", "conversionDateTime": "2026-09-01 13:00:00+00:00"},
	}
	resp, err := a.Execute(context.Background(), &dispatch.Invocation{Args: map[string]any{
		"customerId": "1234567890", "conversions": conversions, "validateOnly": true,
	}}, rvFor("uploadClickConversions"), &dispatch.Credentials{Token: "T"})
	if err != nil {
		t.Fatal(err)
	}
	if *path != "/customers/1234567890:uploadClickConversions" {
		t.Errorf("path = %s", *path)
	}
	if !reflect.DeepEqual(*body, map[string]any{"conversions": conversions, "partialFailure": true, "validateOnly": true}) {
		t.Errorf("request body = %#v", *body)
	}
	if string(resp.Body) != reply {
		t.Fatalf("partial failure response lost: %s", resp.Body)
	}
}

func TestClickConversionsValidation(t *testing.T) {
	a := &Adapter{}
	conversions := make([]any, maxClickConversions)
	for i := range conversions {
		conversions[i] = map[string]any{"gclid": "click"}
	}
	if _, err := a.buildBody("uploadClickConversions", map[string]any{"conversions": conversions}); err != nil {
		t.Fatalf("batch at limit: %v", err)
	}
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{nil, "`conversions` is required"},
		{map[string]any{"conversions": "bad"}, "not a JSON array"},
		{map[string]any{"conversions": []any{nil}}, "must be an object"},
		{map[string]any{"conversions": append(conversions, map[string]any{})}, "limit is"},
		{map[string]any{"conversions": conversions, "validateOnly": "yes"}, "must be a boolean"},
		{map[string]any{"body": map[string]any{"conversions": conversions}, "validateOnly": true}, "does not accept `body`"},
	} {
		if _, err := a.buildBody("uploadClickConversions", tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("error = %v; want %s", err, tc.want)
		}
	}
}

func TestClickConversionsDoNotRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	a := NewAdapter(func() string { return "DEV" })
	a.BaseURL = srv.URL
	a.backoffBase = time.Nanosecond
	_, err := a.Execute(context.Background(), &dispatch.Invocation{Args: map[string]any{
		"customerId": "1234567890", "conversions": []any{map[string]any{"gclid": "click"}},
	}}, rvFor("uploadClickConversions"), &dispatch.Credentials{Token: "T"})
	if err == nil || calls.Load() != 1 {
		t.Fatalf("calls = %d, error = %v; want one failed attempt", calls.Load(), err)
	}
}
