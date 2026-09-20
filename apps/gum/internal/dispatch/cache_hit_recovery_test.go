package dispatch

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/output/profile"
)

// warmFixture pairs an isolated tee directory with a legacy MemCache so the
// second identical call is served from cache without reaching the adapter.
type warmFixture struct {
	dir      string
	dispatch Dispatcher
	calls    *atomic.Int32
}

func newWarmFixture(t *testing.T, body []byte) *warmFixture {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "default")
	var calls atomic.Int32
	adapter := &funcAdapter{
		execute: func(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
			calls.Add(1)
			return &Response{Body: append([]byte(nil), body...), Format: "json", StatusCode: 200}, nil
		},
	}
	d := NewDispatcherWithConfig(minimalCatalog("stub"), map[string]Adapter{"stub": adapter}, DispatcherConfig{
		Tee:   TeeConfig{ProfileDir: dir, RetentionHours: 24},
		Cache: cache.NewMemCache(16, time.Minute),
	})
	return &warmFixture{dir: dir, dispatch: d, calls: &calls}
}

// A cache hit runs the same step-8 pipeline as a cold call, so the same lossy
// profile fires and drops the same fields. The warm path skipped step 7c
// entirely, so the warm response reported dropped paths with no artifact to
// recover them from, and a resource_link profile emitted a response with no
// link at all. Spec §9.0 ties the artifact to the profile firing, not to
// whether the payload came from upstream.
func TestCacheHitStillWritesRecoveryArtifact(t *testing.T) {
	fx := newWarmFixture(t, []byte(`{"messages":[{"id":"m1","snippet":"s"}]}`))
	inv := func() *Invocation {
		return &Invocation{
			OpID:                   "gum.code",
			Args:                   map[string]any{},
			Format:                 "json",
			RequestID:              "warm-recovery",
			AuthSubjectFingerprint: "fp-test",
			OutputProfile:          &profile.Profile{Recovery: "local_artifact"},
		}
	}

	cold, err := fx.dispatch.Dispatch(context.Background(), inv())
	if err != nil {
		t.Fatalf("cold Dispatch: %v", err)
	}
	if cold.FullResultPath == "" {
		t.Fatal("cold call wrote no artifact; the fixture is wrong, not the warm path")
	}

	warm, err := fx.dispatch.Dispatch(context.Background(), inv())
	if err != nil {
		t.Fatalf("warm Dispatch: %v", err)
	}
	if got := fx.calls.Load(); got != 1 {
		t.Fatalf("adapter calls = %d; want 1 (the second call must be a cache hit)", got)
	}
	if warm.FullResultPath == "" {
		t.Fatal("warm FullResultPath empty; a cache hit must carry the same recovery handle as a cold call")
	}
	if _, serr := os.Stat(warm.FullResultPath); serr != nil {
		t.Errorf("warm artifact missing at %s: %v", warm.FullResultPath, serr)
	}
	if warm.Expression == nil || warm.Expression.FullResultPath == "" {
		t.Error("warm _expression carries no full_result_path; the envelope is what the agent reads")
	}
	if warm.FullResultSize == nil || *warm.FullResultSize == 0 {
		t.Error("warm FullResultSize unset; the resource_link size field comes from it")
	}
}

// A resource_link profile must emit the gum://results/<hash> URI on a warm call
// too: the link is unconditional in MCP mode and a missing one is a dead end
// for the agent.
func TestCacheHitEmitsResourceLink(t *testing.T) {
	fx := newWarmFixture(t, []byte(`{"messages":[{"id":"m1"}]}`))
	inv := func() *Invocation {
		return &Invocation{
			OpID:                   "gum.code",
			Args:                   map[string]any{},
			Format:                 "json",
			RequestID:              "warm-link",
			AuthSubjectFingerprint: "fp-test",
			OutputProfile:          &profile.Profile{Recovery: "resource_link", TeeMode: "always"},
		}
	}

	cold, err := fx.dispatch.Dispatch(context.Background(), inv())
	if err != nil {
		t.Fatalf("cold Dispatch: %v", err)
	}
	warm, err := fx.dispatch.Dispatch(context.Background(), inv())
	if err != nil {
		t.Fatalf("warm Dispatch: %v", err)
	}
	if cold.FullResultResource == "" {
		t.Fatal("cold call emitted no resource link; the fixture is wrong")
	}
	if warm.FullResultResource != cold.FullResultResource {
		t.Errorf("warm FullResultResource = %q; want the cold call's %q (same op, args and principal hash to the same artifact)",
			warm.FullResultResource, cold.FullResultResource)
	}
}
