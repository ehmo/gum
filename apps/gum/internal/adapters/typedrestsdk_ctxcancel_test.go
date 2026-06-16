package adapters_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestRetryAfterSleepHonorsContextCancel pins gum-eewm: a context cancelled
// DURING a long Retry-After wait must return promptly, not after the full
// (up-to-300s) sleep. SleepFn blocks for the test's lifetime to simulate the
// long wait; cancelling ctx must unblock Execute via the ctx-aware select.
func TestRetryAfterSleepHonorsContextCancel(t *testing.T) {
	// No goroutine should outlive the cancelled call (review gum-vcvt): the
	// timer-based wait leaves nothing sleeping in the background.
	verifyNoLeaks(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "300")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)

	// SleepFn now honors ctx (so does the production default); it blocks until
	// the context is cancelled, standing in for the 300s Retry-After wait.
	// Execute must return promptly on cancel with no goroutine left sleeping.
	ex.SleepFn = func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	inv, rv := makeTestInvAndVariant(srv.URL)
	creds := &dispatch.Credentials{Token: "fake-bearer-token"}

	// Cancel shortly after Execute enters the wait.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := ex.Execute(ctx, inv, rv, creds)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("want an error after context cancellation")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Execute took %v; cancelling ctx during the Retry-After wait must return promptly", elapsed)
	}
}
