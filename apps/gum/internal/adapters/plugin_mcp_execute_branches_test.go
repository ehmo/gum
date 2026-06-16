package adapters

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/plugins"
)

// TestExecuteCallToolErrorReturnsEnvelope pins the post-ensureRunning
// callErr arm: when plug.CallTool returns an error (here: zero-value
// Plugin → "plugin not running" sentinel from CallTool's nil-cs guard),
// Execute MUST return a *dispatch.Response carrying the raw payload
// + the callErr unchanged, so the dispatch lifecycle surfaces the
// structured envelope rather than swallowing it as a bare error.
func TestExecuteCallToolErrorReturnsEnvelope(t *testing.T) {
	pm := &PluginMCP{running: map[string]*plugins.Plugin{}}
	// Pre-populate the cache so ensureRunning's cache-hit arm returns
	// our zero-value *plugins.Plugin without hitting host.Start.
	pm.running["pid"] = &plugins.Plugin{}

	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{
			Binding: &catalog.Binding{
				PluginName: "pid",
				ToolName:   "tool",
			},
		},
	}
	inv := &dispatch.Invocation{Args: map[string]any{}}

	resp, err := pm.Execute(context.Background(), inv, rv, nil)
	if err == nil {
		t.Fatal("want CallTool error; got nil")
	}
	if !strings.Contains(err.Error(), "plugin not running") {
		t.Errorf("err=%v; want 'plugin not running' (from zero-value CallTool guard)", err)
	}
	if resp == nil {
		t.Fatal("resp=nil; Execute MUST return a *Response even on callErr")
	}
	if resp.Format != "json" {
		t.Errorf("resp.Format=%q; want json", resp.Format)
	}
}
