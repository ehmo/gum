// Spec gum-4pfi: typed-REST Retry-After parsing MUST accept both forms:
// delta-seconds (already supported) AND HTTP-date (RFC 7231 §7.1.3,
// e.g. "Wed, 21 Oct 2015 07:28:00 GMT"). A spec-mandated 300s cap clamps
// runaway upstream values; a date in the past resolves to 0.
//
// TDD red (gum-46uq): retryAfterSeconds() only calls strconv.Atoi
// (typedrestsdk.go:351). HTTP-date inputs return 0. This bundle fails
// until gum-4pfi lands.

package adapters_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestRetryAfterHTTPDateForm asserts the executor honours an HTTP-date
// Retry-After. The server returns 429 with Retry-After set 30 seconds in
// the future, then 200 on retry. The executor must sleep ~30s (captured
// via SleepFn) before the retry.
func TestRetryAfterHTTPDateForm(t *testing.T) {
	verifyNoLeaks(t)

	future := time.Now().UTC().Add(30 * time.Second).Format(http.TimeFormat)

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", future)
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprintf(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"q"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	var slept time.Duration
	ex.SleepFn = func(_ context.Context, d time.Duration) error { slept = d; return nil }

	inv, rv := makeTestInvAndVariant(srv.URL)
	if _, err := ex.Execute(context.Background(), inv, rv, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if slept < 20*time.Second || slept > 35*time.Second {
		t.Errorf("SleepFn called with %v; want ~30s (HTTP-date Retry-After 30s in future)", slept)
	}
}

// TestRetryAfterCapAt300s asserts that an HTTP-date Retry-After set far in
// the future is clamped to the spec-mandated 300s cap.
func TestRetryAfterCapAt300s(t *testing.T) {
	verifyNoLeaks(t)

	farFuture := time.Now().UTC().Add(1 * time.Hour).Format(http.TimeFormat)

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", farFuture)
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprintf(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"q"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	var slept time.Duration
	ex.SleepFn = func(_ context.Context, d time.Duration) error { slept = d; return nil }

	inv, rv := makeTestInvAndVariant(srv.URL)
	if _, err := ex.Execute(context.Background(), inv, rv, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if slept > 300*time.Second {
		t.Errorf("SleepFn called with %v; want capped at 300s", slept)
	}
	if slept < 290*time.Second {
		t.Errorf("SleepFn called with %v; want ~300s cap engaged for far-future Retry-After", slept)
	}
}

// TestRetryAfterDateInPastReturnsZero asserts that an HTTP-date in the
// past (Retry-After delta is negative) is normalised to 0 — no sleep.
func TestRetryAfterDateInPastReturnsZero(t *testing.T) {
	verifyNoLeaks(t)

	past := time.Now().UTC().Add(-60 * time.Second).Format(http.TimeFormat)

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", past)
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprintf(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"q"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	sleepCount := 0
	ex.SleepFn = func(_ context.Context, d time.Duration) error {
		sleepCount++
		if d > 0 {
			t.Errorf("SleepFn called with %v; want 0 (Retry-After is in the past)", d)
		}
		return nil
	}

	inv, rv := makeTestInvAndVariant(srv.URL)
	creds := &dispatch.Credentials{Token: "x"}
	_, _ = ex.Execute(context.Background(), inv, rv, creds)
	// At most a zero-duration sleep is acceptable; the assertion above
	// catches any positive delay.
}
