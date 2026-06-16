package adapters_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
)

// TestUpstreamErrorError locks the (*UpstreamError).Error() format. The
// dispatch boundary surfaces this string in audit lines and structured
// envelopes; reordering or dropping fields breaks operator-facing telemetry.
func TestUpstreamErrorError(t *testing.T) {
	e := &adapters.UpstreamError{
		HTTPStatus:   429,
		GoogleStatus: "RESOURCE_EXHAUSTED",
		GoogleCode:   "429",
		Message:      "rate limit",
	}
	got := e.Error()
	for _, want := range []string{"upstream error", "429", "RESOURCE_EXHAUSTED", "rate limit"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}

// TestUpstreamErrorHTTPStatusCode verifies the dispatch.HTTPStatuser shim
// just returns the embedded HTTP status verbatim.
func TestUpstreamErrorHTTPStatusCode(t *testing.T) {
	e := &adapters.UpstreamError{HTTPStatus: 503}
	if got := e.HTTPStatusCode(); got != 503 {
		t.Errorf("HTTPStatusCode() = %d, want 503", got)
	}
}

// TestUpstreamErrorRetryAfterMs verifies the dispatch.RetryAfterMsCarrier
// shim surfaces the millis value (0 when unset).
func TestUpstreamErrorRetryAfterMs(t *testing.T) {
	t.Run("populated", func(t *testing.T) {
		e := &adapters.UpstreamError{RetryAfterMillis: 1500}
		if got := e.RetryAfterMs(); got != 1500 {
			t.Errorf("got %d, want 1500", got)
		}
	})
	t.Run("zero_when_missing", func(t *testing.T) {
		e := &adapters.UpstreamError{}
		if got := e.RetryAfterMs(); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})
}
