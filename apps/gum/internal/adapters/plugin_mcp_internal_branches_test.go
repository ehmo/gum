package adapters

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// TestEnsureRunningCacheHitReturnsExisting pins the cache-hit arm of
// ensureRunning: when p.running[pluginID] is already populated, the
// helper MUST return the cached *plugins.Plugin without calling
// host.Start. This is what amortises the MCP handshake across repeated
// dispatches within a single gum process.
func TestEnsureRunningCacheHitReturnsExisting(t *testing.T) {
	pm := &PluginMCP{
		running: map[string]*plugins.Plugin{},
	}
	cached := &plugins.Plugin{} // zero-value sentinel — pointer identity is what matters
	pm.running["cached-id"] = cached

	got, err := pm.ensureRunning(context.Background(), "cached-id")
	if err != nil {
		t.Fatalf("err=%v; want nil on cache hit", err)
	}
	if got != cached {
		t.Errorf("got=%p cached=%p; want same pointer (cache hit must NOT re-Start)", got, cached)
	}
}

// TestEnsureRunningNoHostReturnsError pins the "no host configured" arm
// of ensureRunning: a PluginMCP constructed without a Host AND without
// a lazy factory must surface a clear "no host configured" error
// rather than NPE on host.Start.
func TestEnsureRunningNoHostReturnsError(t *testing.T) {
	pm := &PluginMCP{running: map[string]*plugins.Plugin{}}

	_, err := pm.ensureRunning(context.Background(), "absent")
	if err == nil {
		t.Fatal("want 'no host configured' error; got nil")
	}
	if !strings.Contains(err.Error(), "no host configured") {
		t.Errorf("err=%q; want 'no host configured' wrap", err)
	}
}

// TestEnsureRunningUsesStarterQuarantineGate pins the production seam added for
// gum-g7xr: plugin.mcp dispatch must route first start through a caller-supplied
// starter so the Supervisor can reject quarantined plugins before Host.Start
// spawns a subprocess.
func TestEnsureRunningUsesStarterQuarantineGate(t *testing.T) {
	calls := 0
	pm := NewPluginMCPLazyWithStarter(
		func() *plugins.Host { return plugins.NewHost(plugins.HostConfig{}) },
		func(context.Context, *plugins.Host, string) (*plugins.Plugin, error) {
			calls++
			return nil, plugins.ErrPluginQuarantined
		},
	)

	_, err := pm.ensureRunning(context.Background(), "quarantined")
	if err != plugins.ErrPluginQuarantined {
		t.Fatalf("err=%v; want ErrPluginQuarantined from starter", err)
	}
	if calls != 1 {
		t.Errorf("starter calls=%d; want 1", calls)
	}
	if len(pm.running) != 0 {
		t.Errorf("running has %d entries; quarantined plugin must not be cached", len(pm.running))
	}
}

// TestCloseStopsCachedPlugin pins the Close loop body: when running[]
// has entries, Close MUST iterate, call Stop on each, and clear the
// map. A zero-value *plugins.Plugin returns nil from Stop (nil
// ClientSession guard) — so this exercises the loop body without
// needing a real subprocess.
func TestCloseStopsCachedPlugin(t *testing.T) {
	pm := &PluginMCP{running: map[string]*plugins.Plugin{}}
	pm.running["one"] = &plugins.Plugin{}
	pm.running["two"] = &plugins.Plugin{}

	if err := pm.Close(context.Background()); err != nil {
		t.Errorf("err=%v; want nil (Stop on zero-value plug is a no-op)", err)
	}
	if len(pm.running) != 0 {
		t.Errorf("running has %d entries after Close; want 0", len(pm.running))
	}
}
