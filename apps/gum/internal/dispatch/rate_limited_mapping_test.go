// Package dispatch — gum-r35i acceptance tests.
//
// Spec §3.1 + §1635 + §1421 require: upstream 429 responses and local
// token-bucket exhaustion BOTH surface to the caller as the canonical
// `RATE_LIMITED` structured error envelope, with `retryable=true` and
// (when known) a `retry_after_ms` detail. These tests anchor the
// dispatch-boundary mapping so neither path leaks the raw adapter
// `*UpstreamError` nor the raw bucket sentinel out of `Dispatch`.
package dispatch_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// rateLimitedAdapter is an Adapter stub that always returns the supplied
// upstream error. Lets a test drive executeAdapter with a synthetic 429.
type rateLimitedAdapter struct {
	err error
}

func (a *rateLimitedAdapter) Execute(_ context.Context, _ *dispatch.Invocation, _ *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	return nil, a.err
}

// rateLimitingBucket is a TokenBucket that always returns the supplied
// error from Wait. Lets a test drive tokenBucketStep with the kernel
// rate-limit sentinel.
type rateLimitingBucket struct {
	err error
}

func (b *rateLimitingBucket) Wait(_ context.Context, _, _ string) error { return b.err }

// TestDispatchMapsUpstream429ToRateLimited asserts that when the executor
// returns a *adapters.UpstreamError with HTTPStatus=429 and a Retry-After
// hint, dispatch.Dispatch surfaces a *StructuredError with code
// RATE_LIMITED, retryable=true, and retry_after_ms equal to the upstream
// hint. The mapping happens at the dispatch boundary (spec §1635).
func TestDispatchMapsUpstream429ToRateLimited(t *testing.T) {
	c := loadKernelCatalog(t)
	upstream := &adapters.UpstreamError{
		HTTPStatus:       429,
		RetryAfterMillis: 5000,
		Message:          "Quota exceeded",
	}
	disp := dispatch.NewDispatcher(c, map[string]dispatch.Adapter{
		"code.risor": &rateLimitedAdapter{err: upstream},
	})
	inv := &dispatch.Invocation{
		OpID:      "gum.code",
		Args:      map[string]any{"language": "risor", "source": `gum_print("x")`},
		Format:    "json",
		RequestID: "test-rl-adapter-1",
	}

	_, err := disp.Dispatch(context.Background(), inv)
	if err == nil {
		t.Fatal("expected RATE_LIMITED error, got nil")
	}

	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T (%v); want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeRateLimited {
		t.Errorf("ErrCode = %q; want %q (spec §1421)", se.ErrCode, dispatch.ErrCodeRateLimited)
	}
	if !se.Retryable {
		t.Errorf("Retryable = false; want true (spec §1635)")
	}
	if got := se.Detail["retry_after_ms"]; got != int64(5000) {
		t.Errorf("Detail[retry_after_ms] = %v (%T); want 5000 (int64) — spec §1635 must preserve Retry-After hint", got, got)
	}
}

// TestDispatchMapsUpstream429WithoutRetryAfter asserts that an upstream 429
// without a Retry-After hint still produces RATE_LIMITED + retryable=true,
// but with no retry_after_ms detail (the field is OPTIONAL on the envelope
// per spec §1635 "preserve retry_after_ms when positive").
func TestDispatchMapsUpstream429WithoutRetryAfter(t *testing.T) {
	c := loadKernelCatalog(t)
	upstream := &adapters.UpstreamError{HTTPStatus: 429}
	disp := dispatch.NewDispatcher(c, map[string]dispatch.Adapter{
		"code.risor": &rateLimitedAdapter{err: upstream},
	})
	inv := &dispatch.Invocation{
		OpID:      "gum.code",
		Args:      map[string]any{"language": "risor", "source": `gum_print("x")`},
		Format:    "json",
		RequestID: "test-rl-adapter-2",
	}

	_, err := disp.Dispatch(context.Background(), inv)
	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T (%v); want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeRateLimited {
		t.Errorf("ErrCode = %q; want %q", se.ErrCode, dispatch.ErrCodeRateLimited)
	}
	if _, present := se.Detail["retry_after_ms"]; present {
		t.Errorf("Detail[retry_after_ms] present (=%v); want absent when upstream omitted Retry-After (spec §1635)", se.Detail["retry_after_ms"])
	}
}

// TestDispatchMapsTokenBucketExhaustionToRateLimited asserts that when the
// wired TokenBucket returns dispatch.ErrRateLimited from Wait, Dispatch
// surfaces a *StructuredError with code RATE_LIMITED and retryable=true.
// Local bucket exhaustion has no upstream Retry-After hint, so
// retry_after_ms is absent from the envelope.
func TestDispatchMapsTokenBucketExhaustionToRateLimited(t *testing.T) {
	c := loadKernelCatalog(t)
	disp := dispatch.NewDispatcherWithConfig(
		c,
		map[string]dispatch.Adapter{"code.risor": &rateLimitedAdapter{}},
		dispatch.DispatcherConfig{
			RateLimiter: &rateLimitingBucket{err: dispatch.ErrRateLimited},
		},
	)
	inv := &dispatch.Invocation{
		OpID:      "gum.code",
		Args:      map[string]any{"language": "risor", "source": `gum_print("x")`},
		Format:    "json",
		RequestID: "test-rl-bucket-1",
	}

	_, err := disp.Dispatch(context.Background(), inv)
	if err == nil {
		t.Fatal("expected RATE_LIMITED error, got nil")
	}

	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T (%v); want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeRateLimited {
		t.Errorf("ErrCode = %q; want %q (spec §1421)", se.ErrCode, dispatch.ErrCodeRateLimited)
	}
	if !se.Retryable {
		t.Errorf("Retryable = false; want true (spec §1635)")
	}
	if _, present := se.Detail["retry_after_ms"]; present {
		t.Errorf("Detail[retry_after_ms] present (=%v); want absent for local bucket exhaustion", se.Detail["retry_after_ms"])
	}
}

// TestDispatchPassesThroughNon429UpstreamErrors guards against the mapper
// over-reaching: a 503 or other non-429 UpstreamError MUST NOT be coerced
// to RATE_LIMITED — that path is reserved for SERVICE_DOWN.
func TestDispatchPassesThroughNon429UpstreamErrors(t *testing.T) {
	c := loadKernelCatalog(t)
	upstream := &adapters.UpstreamError{HTTPStatus: 503, Message: "backend down"}
	disp := dispatch.NewDispatcher(c, map[string]dispatch.Adapter{
		"code.risor": &rateLimitedAdapter{err: upstream},
	})
	inv := &dispatch.Invocation{
		OpID:      "gum.code",
		Args:      map[string]any{"language": "risor", "source": `gum_print("x")`},
		Format:    "json",
		RequestID: "test-rl-passthrough-1",
	}

	_, err := disp.Dispatch(context.Background(), inv)
	if err == nil {
		t.Fatal("expected upstream 503 error, got nil")
	}
	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("503 should surface as a StructuredError, got %T: %v", err, err)
	}
	// A 5xx must NOT be RATE_LIMITED (that's 429-only), but it must carry a
	// machine-readable SERVICE_DOWN code so the agent isn't handed an opaque
	// "upstream error HTTP 503..." string (audit 6th pass).
	if se.ErrCode == dispatch.ErrCodeRateLimited {
		t.Errorf("503 was coerced to RATE_LIMITED — the mapper must scope to 429 only (spec §1635 / §1638)")
	}
	if se.ErrCode != dispatch.ErrCodeServiceDown {
		t.Errorf("ErrCode=%q; want SERVICE_DOWN for an exhausted 5xx", se.ErrCode)
	}
}

// envelopeAdapter mimics the plugin adapter: it returns BOTH a Response.Body
// carrying a structured error envelope AND a non-nil error.
type envelopeAdapter struct {
	body []byte
	err  error
}

func (a *envelopeAdapter) Execute(_ context.Context, _ *dispatch.Invocation, _ *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	return &dispatch.Response{Body: a.body, Format: "json"}, a.err
}

// TestDispatchSurfacesAdapterEnvelopeError is the audit regression: when an
// adapter returns (resp-with-envelope, err), the dispatch lifecycle must surface
// the envelope's error_code/retryable, not discard resp and pass only the opaque
// error string. Before the fix the plugin's RATE_LIMITED + retry hint were lost.
func TestDispatchSurfacesAdapterEnvelopeError(t *testing.T) {
	c := loadKernelCatalog(t)
	env := []byte(`{"error_code":"RATE_LIMITED","retryable":true,"retry_after_ms":1500}`)
	disp := dispatch.NewDispatcher(c, map[string]dispatch.Adapter{
		"code.risor": &envelopeAdapter{body: env, err: errors.New("plugin CallTool: tool returned error")},
	})
	inv := &dispatch.Invocation{
		OpID:      "gum.code",
		Args:      map[string]any{"language": "risor", "source": `gum_print("x")`},
		Format:    "json",
		RequestID: "test-envelope-1",
	}
	_, err := disp.Dispatch(context.Background(), inv)
	if err == nil {
		t.Fatal("expected the plugin envelope error, got nil")
	}
	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err is %T; want *StructuredError reconstructed from the envelope", err)
	}
	if se.ErrCode != dispatch.ErrCodeRateLimited {
		t.Errorf("ErrCode=%q; want RATE_LIMITED (from the envelope)", se.ErrCode)
	}
	if !se.Retryable {
		t.Error("Retryable=false; want true (envelope said retryable)")
	}
}
