package adapters_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

const etagAdapterValidator = `W/"rev-17"`

// TestExecutorConditionalRequest covers the adapter half of spec §10.2. The
// dispatch tests drive a stub executor, so only this one proves that the real
// HTTP path puts the validator on the wire and treats the 304 answer as a
// success instead of a terminal 4xx.
func TestExecutorConditionalRequest(t *testing.T) {
	t.Parallel()

	t.Run("a stored validator travels as If-None-Match", func(t *testing.T) {
		t.Parallel()

		var seen atomic.Value
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen.Store(r.Header.Get("If-None-Match"))
			w.Header().Set("ETag", etagAdapterValidator)
			w.WriteHeader(http.StatusNotModified)
		}))
		t.Cleanup(srv.Close)

		inv, rv := makeTestInvAndVariant(srv.URL)
		inv.IfNoneMatch = etagAdapterValidator
		sdk := adapters.NewTypedRestSDK()
		sdk.AllowCredentialHostForTest(srv.URL)

		resp, err := sdk.Execute(context.Background(), inv, rv, &dispatch.Credentials{Token: "etag-token"})
		if err != nil {
			t.Fatalf("a 304 must not be an error: %v", err)
		}
		if got := seen.Load(); got != etagAdapterValidator {
			t.Errorf("If-None-Match = %v, want %q", got, etagAdapterValidator)
		}
		if resp.StatusCode != http.StatusNotModified {
			t.Errorf("status = %d, want 304", resp.StatusCode)
		}
		if resp.ETag != etagAdapterValidator {
			t.Errorf("ETag = %q, want %q", resp.ETag, etagAdapterValidator)
		}
		if len(resp.Body) != 0 {
			t.Errorf("a 304 carries no body, got %q", resp.Body)
		}
	})

	t.Run("no validator sends no header", func(t *testing.T) {
		t.Parallel()

		var present atomic.Bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, ok := r.Header["If-None-Match"]
			present.Store(ok)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("ETag", etagAdapterValidator)
			_, _ = w.Write([]byte(`{"items":[]}`))
		}))
		t.Cleanup(srv.Close)

		inv, rv := makeTestInvAndVariant(srv.URL)
		sdk := adapters.NewTypedRestSDK()
		sdk.AllowCredentialHostForTest(srv.URL)

		resp, err := sdk.Execute(context.Background(), inv, rv, &dispatch.Credentials{Token: "etag-token"})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if present.Load() {
			t.Error("If-None-Match was sent although the caller holds no validator")
		}
		// The 2xx arm must still harvest the validator; without it the kernel
		// has nothing to store and the cache never warms.
		if resp.ETag != etagAdapterValidator {
			t.Errorf("ETag = %q, want %q", resp.ETag, etagAdapterValidator)
		}
	})

	t.Run("a 304 after a rate-limit retry is still a success", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"slow down"}}`))
				return
			}
			w.Header().Set("ETag", etagAdapterValidator)
			w.WriteHeader(http.StatusNotModified)
		}))
		t.Cleanup(srv.Close)

		inv, rv := makeTestInvAndVariant(srv.URL)
		inv.IfNoneMatch = etagAdapterValidator
		sdk := adapters.NewTypedRestSDK()
		sdk.AllowCredentialHostForTest(srv.URL)
		sdk.SleepFn = func(context.Context, time.Duration) error { return nil }

		resp, err := sdk.Execute(context.Background(), inv, rv, &dispatch.Credentials{Token: "etag-token"})
		if err != nil {
			t.Fatalf("a 304 on the retry must not be an error: %v", err)
		}
		if resp.StatusCode != http.StatusNotModified || resp.ETag != etagAdapterValidator {
			t.Errorf("status=%d etag=%q, want 304 and %q", resp.StatusCode, resp.ETag, etagAdapterValidator)
		}
	})
}
