package adapters_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestExecutorContextPropagation pins spec §5.7 line 826: "Generated REST
// dispatch stubs in `gen/dispatch/*.go` MUST pass the incoming
// `context.Context` to the typed Google API call chain via `.Context(ctx)`
// before `.Do()`."
//
// We cannot point at the typed Google SDK in v0.1.0 (the dependency isn't in
// go.mod). Instead, we verify the equivalent contract for TypedRestSDK — the
// shared adapter every gen/dispatch stub forwards into — using
// http.NewRequestWithContext: cancelling the context of an in-flight call
// against a slow fixture HTTP server causes the HTTP round-trip to return
// within 100ms of cancellation. If TypedRestSDK ever stopped propagating ctx,
// this test would block past the 100ms threshold and fail.
func TestExecutorContextPropagation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	finished := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done() // block until the caller cancels
		close(finished)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	inv, rv := makeTestInvAndVariant(srv.URL)
	creds := &dispatch.Credentials{Token: "ctx-prop-token"}
	sdk := adapters.NewTypedRestSDK()
	sdk.AllowCredentialHostForTest(srv.URL)

	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, err := sdk.Execute(ctx, inv, rv, creds)
		done <- result{err: err}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream handler never saw request — ctx not propagated to dial")
	}

	cancelAt := time.Now()
	cancel()

	select {
	case r := <-done:
		elapsed := time.Since(cancelAt)
		if elapsed > 100*time.Millisecond {
			t.Errorf("Execute returned %v after cancel; want <100ms (ctx not propagated to http.Client)", elapsed)
		}
		if !errors.Is(r.err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Execute did not return within 2s of cancel — ctx propagation broken")
	}

	// Handler should have observed the cancellation too. Don't fail the test
	// on this — the http.Client closing the connection is sufficient — but a
	// successful close is a useful signal in CI logs.
	select {
	case <-finished:
	case <-time.After(500 * time.Millisecond):
		t.Log("upstream handler did not observe ctx cancel before timeout; non-fatal")
	}
}
