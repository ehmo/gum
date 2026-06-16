package main

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestDefaultAdaptersPluginMCPHostFactoryFires pins the
// `adapters.NewPluginMCPLazy(func() *plugins.Host { return
// plugins.NewHost(plugins.HostConfig{}) })` closure body
// (root.go:233-235). defaultAdapters wires that factory but defers
// invocation until first plugin dispatch; the closure body itself
// (`plugins.NewHost(plugins.HostConfig{})`) only runs when the
// adapter's resolveHost is triggered, which requires a real Execute
// call. Without this test, the lazy-construction path is silently
// regressable — a future refactor could swap NewHost for a panic
// and nothing would notice.
//
// We force the closure to fire by invoking Execute with a binding
// whose plugin_name is fabricated. resolveHost runs the factory
// (covering line 234), then host.Start fails because no such plugin
// is installed; that failure is the expected outcome, not a bug.
func TestDefaultAdaptersPluginMCPHostFactoryFires(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	ads, _ := defaultAdapters("default")
	pm, ok := ads["plugin.mcp"].(*adapters.PluginMCP)
	if !ok {
		t.Fatalf("plugin.mcp adapter type=%T; want *adapters.PluginMCP", ads["plugin.mcp"])
	}

	rv := &dispatch.ResolvedVariant{
		OpID:       "fake.op",
		AdapterKey: "plugin.mcp",
		Variant: &catalog.Variant{
			Binding: &catalog.Binding{
				PluginName: "nonexistent.plugin",
				ToolName:   "do_something",
			},
		},
	}
	inv := &dispatch.Invocation{
		OpID: "fake.op",
		Args: map[string]any{},
	}
	_, err := pm.Execute(context.Background(), inv, rv, nil)
	// Expected: host.Start fails because the plugin isn't installed.
	// The closure at root.go:234 has already run by the time Start is
	// invoked — that's the line we're pinning. The error itself is
	// just collateral; we only assert it surfaces (i.e., the call did
	// NOT short-circuit before reaching resolveHost).
	if err == nil {
		t.Fatal("Execute(nonexistent plugin) err=nil; want start-failure")
	}
}
