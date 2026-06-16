package auth

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

// TestCallbackServerStrategyLabel pins the audit fix: the shared callback server
// labels AuthError.Strategy with the CALLER's strategy ("byo_oauth" here), not a
// hardcoded "gum_oauth" — error routing/remediation keys on Strategy.
func TestCallbackServerStrategyLabel(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	cb := newCallbackServer(lis, "http://127.0.0.1:0/oauth/callback", "expected-state", "byo_oauth")
	// A callback with a mismatched state triggers an AuthError on cb.done.
	req := httptest.NewRequest("GET", "/oauth/callback?state=wrong", nil)
	cb.handle(httptest.NewRecorder(), req)

	res := <-cb.done
	ae, ok := res.err.(*AuthError)
	if !ok {
		t.Fatalf("callback error type %T, want *AuthError", res.err)
	}
	if ae.Strategy != "byo_oauth" {
		t.Errorf("AuthError.Strategy = %q, want byo_oauth (shared callback must carry the caller's strategy)", ae.Strategy)
	}
}

// TestCallbackAwaitTimeoutSurfacesTypedErr pins await's
// `timer.C → GUM_OAUTH_TIMEOUT` arm (gum_oauth.go:483-488). Reached
// when no callback arrives within the supplied timeout (browser
// hung on consent screen, network partition). await MUST surface a
// typed AuthError carrying the timeout duration so operators see
// a recoverable surface rather than a generic context.DeadlineExceeded.
//
// Tests the loopback listener is also cleaned up via cb.shutdown(),
// even though await's failure path doesn't itself close the server —
// the caller's `defer cb.shutdown()` covers it. Here we close
// directly to match the lifecycle.
func TestCallbackAwaitTimeoutSurfacesTypedErr(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	cb := newCallbackServer(lis, "http://127.0.0.1:0/oauth/callback", "state-xyz", gumOAuthStrategyName)
	// Don't call cb.serve — we want await to time out without any
	// callback firing. Use a 10ms timeout to keep the test fast.

	start := time.Now()
	res, awErr := cb.await(context.Background(), 10*time.Millisecond)
	elapsed := time.Since(start)

	if awErr == nil {
		t.Fatalf("await(10ms, no callback)=%+v nil err; want GUM_OAUTH_TIMEOUT", res)
	}
	if elapsed < 10*time.Millisecond {
		t.Errorf("elapsed=%v; want >= 10ms (timer fired prematurely)", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("elapsed=%v; want < 500ms (timer overran)", elapsed)
	}

	var ae *AuthError
	if !errors.As(awErr, &ae) {
		t.Fatalf("err type=%T %v; want *AuthError", awErr, awErr)
	}
	if ae.Code != "GUM_OAUTH_TIMEOUT" {
		t.Errorf("Code=%q; want GUM_OAUTH_TIMEOUT", ae.Code)
	}
}

// TestCallbackAwaitContextCancelSurfacesCtxErr pins await's
// `ctx.Done → ctx.Err()` arm (line 481-482). Reached when the
// caller's context is cancelled (parent process exiting, user
// hitting Ctrl-C). await MUST surface ctx.Err() verbatim — NOT a
// typed AuthError — because callers (cli) already special-case
// context.Canceled for clean exits.
//
// Already covered by 481-482 in cov output, but pinning it
// alongside the timeout arm keeps the await suite symmetric.
func TestCallbackAwaitContextCancelSurfacesCtxErr(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	cb := newCallbackServer(lis, "http://127.0.0.1:0/oauth/callback", "state-xyz", gumOAuthStrategyName)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so the select picks ctx.Done immediately

	_, awErr := cb.await(ctx, 10*time.Second)
	if awErr == nil {
		t.Fatal("await(cancelled ctx)=nil err; want context.Canceled")
	}
	if !errors.Is(awErr, context.Canceled) {
		t.Errorf("err=%v; want errors.Is(err, context.Canceled) — NOT wrapped as AuthError", awErr)
	}
}
