package adapters_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// catchPanic calls fn and returns ("panic: ...", true) if fn panics.
func catchPanic(fn func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("panic: %v", r)
			panicked = true
		}
	}()
	fn()
	return "", false
}

// verifyNoLeaks registers goleak as a t.Cleanup callback BEFORE the caller
// creates any httptest.Server, so cleanup order (LIFO) is:
//
//  1. srv.Close()         ← httptest server stops (registered after this call)
//  2. goleak.VerifyNone() ← runs last; no stray goroutines
func verifyNoLeaks(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { goleak.VerifyNone(t) })
}

// makeTestInvAndVariant builds a minimal Invocation + ResolvedVariant pointing
// to baseURL for path /test/endpoint using HTTP GET.
func makeTestInvAndVariant(baseURL string) (*dispatch.Invocation, *dispatch.ResolvedVariant) {
	inv := &dispatch.Invocation{
		OpID:      "test.op",
		Args:      map[string]any{},
		Format:    "json",
		RequestID: "test-req-1",
	}
	rv := &dispatch.ResolvedVariant{
		OpID:       "test.op",
		AdapterKey: "rest.typed-rest-sdk",
		Variant: &catalog.Variant{
			VariantID:     "test.v1",
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindDiscoveryREST,
			BackendKind:   catalog.BackendKindTypedRestSDK,
			RiskClass:     catalog.RiskClassRead,
			Binding: &catalog.Binding{
				BindingSchemaVersion: 1,
				AdapterKey:           "rest.typed-rest-sdk",
				OperationKey:         "test.op",
				HTTP: &catalog.HTTPBinding{
					Method: "GET",
					Path:   "/test/endpoint",
				},
			},
		},
	}
	// Patch the path to an absolute URL rooted at the test server.
	// TypedRestSDK must support absolute URLs in HTTP.Path for testing.
	rv.Variant.Binding.HTTP.Path = baseURL + "/test/endpoint"
	return inv, rv
}

// TestExecutorBackoff verifies (G3.6): three synthetic 503s then a 200 —
// the call eventually succeeds within MaxElapsedTime=60s with 3 retries.
func TestExecutorBackoff(t *testing.T) {
	verifyNoLeaks(t)

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n <= 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, `{"error":{"code":503,"status":"UNAVAILABLE","message":"try later"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"messages":[{"id":"abc123"}]}`)
	}))
	t.Cleanup(srv.Close)

	var ex *adapters.TypedRestSDK
	msg, panicked := catchPanic(func() {
		ex = adapters.NewTypedRestSDK()
	})
	if panicked {
		t.Fatalf("NewTypedRestSDK panicked: %s — green team must implement NewTypedRestSDK", msg)
	}
	ex.AllowCredentialHostForTest(srv.URL)

	inv, rv := makeTestInvAndVariant(srv.URL)
	creds := &dispatch.Credentials{Token: "fake-bearer-token"}

	var resp *dispatch.Response
	var err error
	msg, panicked = catchPanic(func() {
		resp, err = ex.Execute(t.Context(), inv, rv, creds)
	})
	if panicked {
		t.Fatalf("Execute panicked: %s — green team must implement Execute", msg)
	}
	if err != nil {
		t.Fatalf("Execute: %v (expected eventual success after 3 retries)", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if callCount.Load() != 4 {
		t.Errorf("server called %d times, want 4 (3 failures + 1 success)", callCount.Load())
	}
}

// TestTokenBucketBackoff verifies (G3.5): a Retry-After: 1 response causes the
// adapter to sleep ~1s (allow ±200ms) before the retry.
func TestTokenBucketBackoff(t *testing.T) {
	verifyNoLeaks(t)

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests) // 429
			_, _ = fmt.Fprintf(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"quota"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"messages":[]}`)
	}))
	t.Cleanup(srv.Close)

	var ex *adapters.TypedRestSDK
	msg, panicked := catchPanic(func() {
		ex = adapters.NewTypedRestSDK()
	})
	if panicked {
		t.Fatalf("NewTypedRestSDK panicked: %s — green team must implement NewTypedRestSDK", msg)
	}
	ex.AllowCredentialHostForTest(srv.URL)

	var sleptDuration time.Duration
	ex.SleepFn = func(_ context.Context, d time.Duration) error {
		sleptDuration = d
		return nil
	}

	inv, rv := makeTestInvAndVariant(srv.URL)
	creds := &dispatch.Credentials{Token: "fake-bearer-token"}

	var resp *dispatch.Response
	var err error
	msg, panicked = catchPanic(func() {
		resp, err = ex.Execute(t.Context(), inv, rv, creds)
	})
	if panicked {
		t.Fatalf("Execute panicked: %s — green team must implement Execute", msg)
	}
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}

	const want = time.Second
	const margin = 200 * time.Millisecond
	if sleptDuration < want-margin || sleptDuration > want+margin {
		t.Errorf("SleepFn called with %v, want ~%v (±%v)", sleptDuration, want, margin)
	}
	if callCount.Load() != 2 {
		t.Errorf("server called %d times, want 2", callCount.Load())
	}
}

// TestExecutor429NotRetriedAs5xx asserts that a 429 response without a
// Retry-After header is NOT fed into the exponential-backoff retry loop
// reserved for transient 5xx errors (spec §3.1 step 6, §6.3). The adapter
// performs at most one immediate inline retry attempt after the (zero) sleep,
// and on the second 429 returns a permanent error. A leak into the 5xx loop
// would manifest as ≥3 retry attempts and several seconds of backoff sleep.
func TestExecutor429NotRetriedAs5xx(t *testing.T) {
	verifyNoLeaks(t)

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprintf(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"quota"}}`)
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)

	// Replace SleepFn so the test doesn't actually sleep, and observe how many
	// times it gets called. A correctly-bounded 429 path sleeps zero times
	// (no Retry-After header) and triggers no 5xx retry attempts.
	var sleepCalls atomic.Int32
	ex.SleepFn = func(context.Context, time.Duration) error { sleepCalls.Add(1); return nil }

	inv, rv := makeTestInvAndVariant(srv.URL)
	creds := &dispatch.Credentials{Token: "fake-bearer-token"}

	start := time.Now()
	_, err := ex.Execute(context.Background(), inv, rv, creds)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error after 429s; got nil")
	}
	var ue *adapters.UpstreamError
	if !errors.As(err, &ue) || ue.HTTPStatus != http.StatusTooManyRequests {
		t.Errorf("err = %v; want UpstreamError with HTTPStatus=429", err)
	}
	// Exactly two HTTP attempts: initial + one inline post-Retry-After retry.
	// Anything ≥3 indicates the 5xx backoff loop incorrectly retried a 429.
	if got := callCount.Load(); got != 2 {
		t.Errorf("server called %d times; want 2 (initial + one inline retry, NO 5xx-loop retries)", got)
	}
	if got := sleepCalls.Load(); got != 0 {
		t.Errorf("SleepFn called %d times; want 0 (no Retry-After header → no sleep)", got)
	}
	// Defensive upper bound: even a single 5xx backoff iteration would add
	// hundreds of ms; a no-retry 429 path completes in well under a second.
	if elapsed > 2*time.Second {
		t.Errorf("Execute took %v; want <2s (suggests 5xx backoff was entered)", elapsed)
	}
}

// TestExecutorRespectsContextCancel ensures that if the context is cancelled
// during a retry backoff, Execute returns ctx.Err() promptly.
func TestExecutorRespectsContextCancel(t *testing.T) {
	verifyNoLeaks(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintf(w, `{"error":{"code":503,"status":"UNAVAILABLE","message":"always down"}}`)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())

	var ex *adapters.TypedRestSDK
	msg, panicked := catchPanic(func() {
		ex = adapters.NewTypedRestSDK()
	})
	if panicked {
		t.Fatalf("NewTypedRestSDK panicked: %s — green team must implement NewTypedRestSDK", msg)
	}
	ex.AllowCredentialHostForTest(srv.URL)

	// Make SleepFn cancel the context so we escape the backoff loop.
	ex.SleepFn = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}

	inv, rv := makeTestInvAndVariant(srv.URL)
	creds := &dispatch.Credentials{Token: "fake-bearer-token"}

	var err error
	msg, panicked = catchPanic(func() {
		_, err = ex.Execute(ctx, inv, rv, creds)
	})
	if panicked {
		t.Fatalf("Execute panicked: %s — green team must implement Execute", msg)
	}
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// TestExecutor429Then5xxRetriesForIdempotent is the audit regression: a 5xx that
// arrives on the inline post-Retry-After retry of a 429 must be treated as a
// transient 5xx (retryable for idempotent methods), NOT made permanent. Before
// the fix the 429 arm returned backoff.Permanent for any non-2xx second
// response, so a 429→503 failed immediately instead of entering the 5xx loop.
func TestExecutor429Then5xxRetriesForIdempotent(t *testing.T) {
	verifyNoLeaks(t)

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch callCount.Add(1) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprintf(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"quota"}}`)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, `{"error":{"code":503,"status":"UNAVAILABLE","message":"transient"}}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"messages":[{"id":"abc123"}]}`)
		}
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	ex.SleepFn = func(context.Context, time.Duration) error { return nil } // no real sleep on the (absent) Retry-After

	inv, rv := makeTestInvAndVariant(srv.URL) // GET → idempotent
	creds := &dispatch.Credentials{Token: "fake-bearer-token"}

	resp, err := ex.Execute(context.Background(), inv, rv, creds)
	if err != nil {
		t.Fatalf("Execute: %v; want eventual success (429→503 must retry, not go permanent)", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d; want 200", resp.StatusCode)
	}
	// 1 (429) + 1 (inline 503) + >=1 (5xx-backoff retry → 200).
	if got := callCount.Load(); got < 3 {
		t.Errorf("server called %d times; want >=3 (429, inline 503, then 5xx-retry to 200)", got)
	}
}

// TestExecutePathReservedExpansionPreservesSlashes pins the {+param} support:
// a {+resourceName} placeholder (RFC 6570 reserved expansion) keeps '/' in the
// value (people/c123 → /people/c123), while a plain {resourceName} escapes it.
// Needed for resource-name APIs (People, Drive revisions/comments, etc.).
func TestExecutePathReservedExpansionPreservesSlashes(t *testing.T) {
	verifyNoLeaks(t)
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	inv, rv := makeTestInvAndVariant(srv.URL)
	creds := &dispatch.Credentials{Token: "x"}

	rv.Variant.Binding.HTTP.Path = srv.URL + "/{+resourceName}"
	inv.Args = map[string]any{"resourceName": "people/c123"}
	if _, err := ex.Execute(context.Background(), inv, rv, creds); err != nil {
		t.Fatalf("Execute(+): %v", err)
	}
	if gotURI != "/people/c123" {
		t.Errorf("{+resourceName} URI = %q; want /people/c123 (slash preserved)", gotURI)
	}

	rv.Variant.Binding.HTTP.Path = srv.URL + "/{resourceName}"
	inv.Args = map[string]any{"resourceName": "people/c123"}
	if _, err := ex.Execute(context.Background(), inv, rv, creds); err != nil {
		t.Fatalf("Execute(no +): %v", err)
	}
	if gotURI != "/people%2Fc123" {
		t.Errorf("{resourceName} URI = %q; want /people%%2Fc123 (slash escaped)", gotURI)
	}
}

// TestExecuteHeaderParamsRouteToHeaders pins the Binding.HeaderParams routing:
// an arg named in HeaderParams is sent as the mapped HTTP header (X-Goog-FieldMask)
// and is NOT leaked into the query string, while other args still go to the query.
// Needed for Places (New) / Routes which require the field-mask header.
func TestExecuteHeaderParamsRouteToHeaders(t *testing.T) {
	verifyNoLeaks(t)
	var gotMask, gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMask = r.Header.Get("X-Goog-FieldMask")
		gotURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	inv, rv := makeTestInvAndVariant(srv.URL)
	rv.Variant.Binding.HTTP.Path = srv.URL + "/v1/places:searchText"
	rv.Variant.Binding.HTTP.HeaderParams = map[string]string{"fieldMask": "X-Goog-FieldMask"}
	inv.Args = map[string]any{"fieldMask": "places.displayName", "extra": "q1"}

	if _, err := ex.Execute(context.Background(), inv, rv, &dispatch.Credentials{APIKey: "k"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMask != "places.displayName" {
		t.Errorf("X-Goog-FieldMask header = %q; want places.displayName", gotMask)
	}
	if strings.Contains(gotURI, "fieldMask") {
		t.Errorf("fieldMask leaked into query string: %q", gotURI)
	}
	if !strings.Contains(gotURI, "extra=q1") {
		t.Errorf("non-header arg missing from query: %q", gotURI)
	}
}
