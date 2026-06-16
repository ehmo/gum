// Spec gum-1n1t: dispatch request_id MUST be unique across concurrent
// invocations. UnixNano resolution (1ns) is below typical goroutine wakeup
// granularity, so 256 parallel dispatches reliably hit collisions in CI.
// The fix is crypto/rand-derived IDs.
//
// TDD red (gum-46uq): lifecycle.go:283 uses fmt.Sprintf("req-%d",
// time.Now().UnixNano()). This test fails until gum-1n1t lands.

package dispatch_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// concurrentCapturingHandler is a thread-safe slog.Handler that records
// every request_id attribute observed across goroutines.
type concurrentCapturingHandler struct {
	mu  sync.Mutex
	ids map[string]int
}

func (h *concurrentCapturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *concurrentCapturingHandler) Handle(_ context.Context, r slog.Record) error {
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "request_id" {
			h.mu.Lock()
			h.ids[a.Value.String()]++
			h.mu.Unlock()
		}
		return true
	})
	return nil
}
func (h *concurrentCapturingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *concurrentCapturingHandler) WithGroup(_ string) slog.Handler      { return h }

// TestDispatchConcurrentRequestIDsUnique fires 256 parallel dispatches
// (each leaving Invocation.RequestID empty so the lifecycle generates one)
// and asserts every emitted request_id is unique.
func TestDispatchConcurrentRequestIDsUnique(t *testing.T) {
	const N = 256

	c := loadKernelCatalog(t)
	runner := adapters.NewCodeRunner()
	disp := dispatch.NewDispatcher(c, map[string]dispatch.Adapter{
		"code.risor": runner,
	})

	h := &concurrentCapturingHandler{ids: map[string]int{}}
	logger := slog.New(h)
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			inv := &dispatch.Invocation{
				OpID:   "gum.code",
				Args:   map[string]any{"language": "risor", "source": `gum_print("x")`},
				Format: "json",
				// RequestID intentionally empty: forces lifecycle generation.
			}
			_, _ = disp.Dispatch(context.Background(), inv)
		}()
	}
	wg.Wait()

	// Each dispatch emits the 9 lifecycle events with the same request_id, so
	// counts above 1 are expected per id. The invariant is: distinct ids
	// observed == N.
	h.mu.Lock()
	defer h.mu.Unlock()
	if got := len(h.ids); got != N {
		t.Errorf("distinct request_ids = %d; want %d (collisions detected — UnixNano resolution insufficient under concurrency)", got, N)
	}
}
