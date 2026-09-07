package adapters_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
)

func TestTypedRestValidationDiagnostics(t *testing.T) {
	var violations []map[string]string
	for i := 0; i < 7; i++ {
		violations = append(violations, map[string]string{"field": fmt.Sprintf("events[%d].event_source", i), "reason": "REQUIRED_FIELD_MISSING", "description": "Required field is missing."})
	}
	payload, err := json.Marshal(map[string]any{"error": map[string]any{
		"code": 400, "status": "INVALID_ARGUMENT", "message": "There was a problem with the request.",
		"details": []any{
			map[string]any{"@type": "type.googleapis.com/google.rpc.RequestInfo", "requestId": "request-test"},
			map[string]any{"@type": "type.googleapis.com/google.rpc.BadRequest", "fieldViolations": violations},
			map[string]any{"@type": "type.googleapis.com/unknown.Type", "requestId": "ignore-unknown-type"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	ex := adapters.NewTypedRestSDK()
	ex.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(payload)))}, nil
	})}
	inv, rv := makeTestInvAndVariant("https://datamanager.googleapis.com")
	_, err = ex.Execute(context.Background(), inv, rv, nil)
	var upstream *adapters.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("want upstream error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("validation rejection retried %d times", calls)
	}
	if upstream.RequestID != "request-test" || len(upstream.Details) != 6 {
		t.Fatalf("wrong diagnostics: %+v", upstream)
	}
	for _, needle := range []string{"request-test", "events[0].event_source", "events[4].event_source", "REQUIRED_FIELD_MISSING", "2 more field violations"} {
		if !strings.Contains(err.Error(), needle) {
			t.Errorf("missing %s from %v", needle, err)
		}
	}
	for _, needle := range []string{"events[5]", "events[6]", "ignore-unknown-type"} {
		if strings.Contains(err.Error(), needle) {
			t.Errorf("unexpected detail: %s", needle)
		}
	}
}
