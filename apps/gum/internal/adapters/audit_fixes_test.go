package adapters_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestExecutorPostNotRetriedOn5xx pins the audit fix: a 5xx on a non-idempotent
// method (POST) must NOT be retried — the request may have already taken effect
// server-side, so a retry would duplicate the write/send.
func TestExecutorPostNotRetriedOn5xx(t *testing.T) {
	verifyNoLeaks(t)

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintf(w, `{"error":{"code":503,"status":"UNAVAILABLE","message":"down"}}`)
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	inv, rv := makeTestInvAndVariant(srv.URL)
	rv.Variant.Binding.HTTP.Method = "POST"
	creds := &dispatch.Credentials{Token: "fake-bearer-token"}

	_, err := ex.Execute(t.Context(), inv, rv, creds)
	if err == nil {
		t.Fatal("expected an error from the 503 POST")
	}
	if got := callCount.Load(); got != 1 {
		t.Errorf("POST called %d times on 503; want exactly 1 (no retry on non-idempotent method)", got)
	}
}

// TestExecutorGet5xxStillRetried guards that the method-guard didn't disable
// retries for idempotent GETs (the original resilience must be preserved).
func TestExecutorGet5xxStillRetried(t *testing.T) {
	verifyNoLeaks(t)

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, `{"error":{"code":503,"status":"UNAVAILABLE","message":"down"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	inv, rv := makeTestInvAndVariant(srv.URL) // GET by default
	creds := &dispatch.Credentials{Token: "fake-bearer-token"}

	resp, err := ex.Execute(t.Context(), inv, rv, creds)
	if err != nil {
		t.Fatalf("Execute: %v (GET should retry through transient 503s)", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if got := callCount.Load(); got != 3 {
		t.Errorf("GET called %d times; want 3 (2 failures + 1 success)", got)
	}
}

// TestExecutorLargeIntegerQueryParamNotScientific pins the audit fix: a large
// integer query arg (arriving as float64 from JSON) must render as plain digits,
// not scientific notation (which Google REST APIs reject with 400).
func TestExecutorLargeIntegerQueryParamNotScientific(t *testing.T) {
	verifyNoLeaks(t)

	var gotOffset string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOffset = r.URL.Query().Get("offset")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	inv, rv := makeTestInvAndVariant(srv.URL)
	inv.Args = map[string]any{"offset": float64(1500000000)} // 1.5e9 as JSON number
	creds := &dispatch.Credentials{Token: "fake-bearer-token"}

	if _, err := ex.Execute(t.Context(), inv, rv, creds); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotOffset != "1500000000" {
		t.Errorf("offset query param = %q, want \"1500000000\" (no scientific notation)", gotOffset)
	}
}
