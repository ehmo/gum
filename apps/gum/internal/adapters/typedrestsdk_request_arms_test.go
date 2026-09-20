package adapters_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/adapters"
)

// TestExecuteLeavesAnUnprovidedPathPlaceholder covers the substitution
// fallback: a {name} placeholder with no matching arg stays literal rather
// than collapsing to an empty segment, so the upstream 404 names the field
// the caller forgot.
func TestExecuteLeavesAnUnprovidedPathPlaceholder(t *testing.T) {
	verifyNoLeaks(t)

	var gotPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	inv, rv := makeTestInvAndVariant(srv.URL)
	rv.Variant.Binding.HTTP.Path = srv.URL + "/test/{id}/endpoint"

	if _, err := ex.Execute(context.Background(), inv, rv, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got, _ := gotPath.Load().(string); got != "/test/{id}/endpoint" {
		t.Errorf("request path = %q; want the literal placeholder preserved", got)
	}
}

// TestExecuteStopsOnAnAlreadyCancelledContext covers the guard at the top of
// the backoff operation: a caller that cancelled before dispatch must not
// reach the network at all.
func TestExecuteStopsOnAnAlreadyCancelledContext(t *testing.T) {
	verifyNoLeaks(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ex := adapters.NewTypedRestSDK()
	inv, rv := makeTestInvAndVariant(srv.URL)

	_, err := ex.Execute(ctx, inv, rv, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v; want context.Canceled", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server hits = %d; want 0 (cancelled before the first attempt)", n)
	}
}

// TestExecuteAbortsTheRetryWhenSleepCancels covers the doRequest context
// guard. The Retry-After wait is the one place where the context can die
// between two attempts of the same backoff operation.
func TestExecuteAbortsTheRetryWhenSleepCancels(t *testing.T) {
	verifyNoLeaks(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"slow down"}}`))
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ex := adapters.NewTypedRestSDK()
	// Cancel during the Retry-After wait but report a completed wait, so the
	// second attempt starts and has to notice the dead context itself.
	ex.SleepFn = func(_ context.Context, _ time.Duration) error {
		cancel()
		return nil
	}

	inv, rv := makeTestInvAndVariant(srv.URL)
	_, err := ex.Execute(ctx, inv, rv, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v; want context.Canceled", err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("server hits = %d; want 1 (the retry must not leave the client)", n)
	}
}

// TestExecuteSurfacesAnInvalidHTTPMethod covers the request-construction
// error arm. A binding method that net/http rejects fails before any
// connection is opened.
func TestExecuteSurfacesAnInvalidHTTPMethod(t *testing.T) {
	verifyNoLeaks(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	inv, rv := makeTestInvAndVariant(srv.URL)
	rv.Variant.Binding.HTTP.Method = "BAD METHOD"

	_, err := ex.Execute(context.Background(), inv, rv, nil)
	if err == nil {
		t.Fatal("Execute error = nil; want an invalid-method failure")
	}
	if !strings.Contains(err.Error(), "invalid method") {
		t.Errorf("Execute error = %v; want it to name the invalid method", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server hits = %d; want 0", n)
	}
}

// TestExecuteParsesRetryAfterOnAPlain4xx covers the Retry-After branch of
// parseUpstreamError reached from the terminal 4xx arm, not the 429 arm. A
// 403 quota error carries the same hint and must surface it.
func TestExecuteParsesRetryAfterOnAPlain4xx(t *testing.T) {
	verifyNoLeaks(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"quota"}}`))
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	inv, rv := makeTestInvAndVariant(srv.URL)

	_, err := ex.Execute(context.Background(), inv, rv, nil)
	var ue *adapters.UpstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("Execute error = %v; want *adapters.UpstreamError", err)
	}
	if ue.RetryAfterMillis != 2000 {
		t.Errorf("RetryAfterMillis = %d; want 2000", ue.RetryAfterMillis)
	}
}

// TestExecuteKeepsTheFirstRetryAfterWhenTheRetryOmitsIt covers the
// first-attempt header fallback. Google occasionally drops Retry-After on the
// repeat 429; the caller still needs the original hint.
func TestExecuteKeepsTheFirstRetryAfterWhenTheRetryOmitsIt(t *testing.T) {
	verifyNoLeaks(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"slow down"}}`))
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	var slept time.Duration
	ex.SleepFn = func(_ context.Context, d time.Duration) error { slept = d; return nil }

	inv, rv := makeTestInvAndVariant(srv.URL)
	_, err := ex.Execute(context.Background(), inv, rv, nil)

	var ue *adapters.UpstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("Execute error = %v; want *adapters.UpstreamError", err)
	}
	if ue.RetryAfterMillis != 1000 {
		t.Errorf("RetryAfterMillis = %d; want 1000 carried over from the first attempt", ue.RetryAfterMillis)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("server hits = %d; want 2", n)
	}
	if slept != time.Second {
		t.Errorf("SleepFn duration = %v; want 1s", slept)
	}
}
