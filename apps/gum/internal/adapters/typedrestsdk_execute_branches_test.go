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

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestExecuteMissingHTTPBindingErrors pins Execute's `Variant.Binding.HTTP == nil →
// "missing HTTP binding"` arm (typedrestsdk.go:120-122). Without an HTTP binding the
// adapter cannot construct a request, so it MUST surface a clear error rather than
// dereference a nil pointer.
func TestExecuteMissingHTTPBindingErrors(t *testing.T) {
	t.Parallel()
	inv := &dispatch.Invocation{OpID: "test.op", Args: map[string]any{}}
	rv := &dispatch.ResolvedVariant{
		OpID: "test.op",
		Variant: &catalog.Variant{
			VariantID: "test.v1",
			Binding:   &catalog.Binding{}, // HTTP == nil
		},
	}
	ex := adapters.NewTypedRestSDK()
	_, err := ex.Execute(t.Context(), inv, rv, nil)
	if err == nil {
		t.Fatal("Execute(no HTTP binding) err=nil; want missing HTTP binding")
	}
	if !strings.Contains(err.Error(), "missing HTTP binding") {
		t.Errorf("err=%q; want 'missing HTTP binding'", err.Error())
	}
}

// TestExecuteAPIKeyHeaderPassthrough pins the `creds.APIKey != "" → X-Goog-Api-Key`
// arm (typedrestsdk.go:191-199). When creds carry an APIKey (auth_strategy=api_key)
// the request MUST include Google's universal X-Goog-Api-Key header.
func TestExecuteAPIKeyHeaderPassthrough(t *testing.T) {
	verifyNoLeaks(t)
	var seenKey atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenKey.Store(r.Header.Get("X-Goog-Api-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	inv, rv := makeTestInvAndVariant(srv.URL)
	creds := &dispatch.Credentials{APIKey: "test-api-key-xyz"}
	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	resp, err := ex.Execute(t.Context(), inv, rv, creds)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status=%d; want 200", resp.StatusCode)
	}
	got, _ := seenKey.Load().(string)
	if got != "test-api-key-xyz" {
		t.Errorf("X-Goog-Api-Key=%q; want 'test-api-key-xyz'", got)
	}
}

func TestExecuteRejectsCredentialedUnknownAbsoluteURLBeforeRequest(t *testing.T) {
	var hits atomic.Int32
	ex := adapters.NewTypedRestSDK()
	ex.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		hits.Add(1)
		return nil, context.Canceled
	})}

	inv, rv := makeTestInvAndVariant("https://evil.example.invalid")
	_, err := ex.Execute(t.Context(), inv, rv, &dispatch.Credentials{Token: "secret-token"})
	if err == nil {
		t.Fatal("Execute returned nil err for credentialed unknown absolute URL")
	}
	if !strings.Contains(err.Error(), "host") || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err=%v, want host not allowed", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("server received %d request(s); want 0", hits.Load())
	}
}

func TestExecuteRequiresCredentialAllowlistPortMatch(t *testing.T) {
	var hits atomic.Int32
	ex := adapters.NewTypedRestSDK()
	ex.CredentialHostAllowlist = []string{"https://api.example.test:8443"}
	ex.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		hits.Add(1)
		return nil, context.Canceled
	})}

	inv, rv := makeTestInvAndVariant("https://api.example.test:9443")
	_, err := ex.Execute(t.Context(), inv, rv, &dispatch.Credentials{Token: "secret-token"})
	if err == nil {
		t.Fatal("Execute returned nil err for credentialed URL with mismatched allowlist port")
	}
	if !strings.Contains(err.Error(), "host") || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err=%v, want host not allowed", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("transport received %d request(s); want 0", hits.Load())
	}
}

// TestExecuteQuotaProjectHeaderPassthrough pins the `creds.QuotaProjectID != "" →
// X-Goog-User-Project` arm (typedrestsdk.go:200-205). Mirrors the APIKey case —
// the header MUST be attached so Google bills against the right quota project.
func TestExecuteQuotaProjectHeaderPassthrough(t *testing.T) {
	verifyNoLeaks(t)
	var seenProj atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenProj.Store(r.Header.Get("X-Goog-User-Project"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	inv, rv := makeTestInvAndVariant(srv.URL)
	creds := &dispatch.Credentials{Token: "bearer", QuotaProjectID: "my-proj"}
	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	if _, err := ex.Execute(t.Context(), inv, rv, creds); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, _ := seenProj.Load().(string)
	if got != "my-proj" {
		t.Errorf("X-Goog-User-Project=%q; want 'my-proj'", got)
	}
}

// TestExecute4xxOtherSurfacesPermanentUpstreamError pins the `status >= 400 &&
// status != 429 → backoff.Permanent(UpstreamError)` arm (typedrestsdk.go:308-310).
// A 400 from upstream MUST surface as an UpstreamError without retries (Permanent).
func TestExecute4xxOtherSurfacesPermanentUpstreamError(t *testing.T) {
	verifyNoLeaks(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad"}}`)
	}))
	t.Cleanup(srv.Close)

	inv, rv := makeTestInvAndVariant(srv.URL)
	creds := &dispatch.Credentials{Token: "t"}
	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	_, err := ex.Execute(t.Context(), inv, rv, creds)
	if err == nil {
		t.Fatal("Execute(400) err=nil; want UpstreamError")
	}
	var ue *adapters.UpstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("err=%T %v; want *UpstreamError", err, err)
	}
	if ue.HTTPStatus != http.StatusBadRequest {
		t.Errorf("HTTPStatus=%d; want 400", ue.HTTPStatus)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls=%d; want 1 (no retry on 4xx)", got)
	}
}

// TestExecuteMarshalBodyErrorWraps pins the `marshalBody err → "marshal request
// body" wrap` arm (typedrestsdk.go:168-170). A non-marshalable body value (e.g.
// a channel) trips json.Marshal so the adapter surfaces a typed wrap.
func TestExecuteMarshalBodyErrorWraps(t *testing.T) {
	t.Parallel()
	inv := &dispatch.Invocation{
		OpID: "test.op",
		Args: map[string]any{adapters.BodyArgKey: make(chan int)},
	}
	rv := &dispatch.ResolvedVariant{
		OpID: "test.op",
		Variant: &catalog.Variant{
			VariantID: "test.v1",
			Binding: &catalog.Binding{
				HTTP: &catalog.HTTPBinding{Method: "POST", Path: "https://example.invalid/x"},
			},
		},
	}
	ex := adapters.NewTypedRestSDK()
	_, err := ex.Execute(t.Context(), inv, rv, nil)
	if err == nil {
		t.Fatal("Execute(unmarshalable body) err=nil; want marshal wrap")
	}
	if !strings.Contains(err.Error(), "marshal request body") {
		t.Errorf("err=%q; want 'marshal request body' wrap", err.Error())
	}
}
