package adapters

import (
	"sync/atomic"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// TestNewPluginMCPLazyDefersHostConstruction pins NewPluginMCPLazy:
// the factory closure MUST NOT run at constructor time — CLI
// invocations that never touch a plugin pay zero cost for host setup
// (registry walk, plugin-state.json read). The closure should only
// fire on the first resolveHost call, and at most once even under
// repeated resolution.
func TestNewPluginMCPLazyDefersHostConstruction(t *testing.T) {
	var calls atomic.Int32
	hostFn := func() *plugins.Host {
		calls.Add(1)
		return plugins.NewHost(plugins.HostConfig{})
	}

	pm := NewPluginMCPLazy(hostFn)
	if pm == nil {
		t.Fatal("NewPluginMCPLazy returned nil")
	}
	if pm.hostFn == nil {
		t.Error("hostFn nil after constructor; want stored")
	}
	if pm.host != nil {
		t.Error("host populated at constructor time; want deferred")
	}
	if pm.running == nil {
		t.Error("running map nil after constructor; want initialised")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("hostFn calls=%d at constructor time; want 0 (deferred)", got)
	}

	// First resolveHost MUST fire the factory exactly once; a second
	// call MUST be a no-op (sync.Once guard).
	if h := pm.resolveHost(); h == nil {
		t.Error("resolveHost returned nil; want lazy-constructed host")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("hostFn calls=%d after first resolveHost; want 1", got)
	}
	if h := pm.resolveHost(); h == nil {
		t.Error("resolveHost returned nil on second call; want cached host")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("hostFn calls=%d after second resolveHost; want 1 (sync.Once)", got)
	}
}
