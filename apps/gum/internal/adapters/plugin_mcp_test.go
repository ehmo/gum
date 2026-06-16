package adapters_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/plugins"
)

// TestPluginMCPRejectsMissingBinding pins the precondition checks the
// adapter applies before it even contacts the Host: a nil binding or
// missing plugin_name/tool_name should fast-fail with a clear error rather
// than panic. The dispatch lifecycle invariant (every resolved variant
// carries a non-nil binding) is enforced at catalog validate time; this
// guards a regression in the validator from cascading into a runtime panic.
func TestPluginMCPRejectsMissingBinding(t *testing.T) {
	t.Parallel()
	pm := adapters.NewPluginMCP(plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()}))

	cases := []struct {
		name    string
		variant *catalog.Variant
	}{
		{
			name:    "nil binding",
			variant: &catalog.Variant{VariantID: "x"},
		},
		{
			name: "missing plugin_name",
			variant: &catalog.Variant{
				VariantID: "x",
				Binding: &catalog.Binding{
					AdapterKey: "plugin.mcp",
					ToolName:   "echo",
				},
			},
		},
		{
			name: "missing tool_name",
			variant: &catalog.Variant{
				VariantID: "x",
				Binding: &catalog.Binding{
					AdapterKey: "plugin.mcp",
					PluginName: "some-plugin",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rv := &dispatch.ResolvedVariant{
				OpID:       "x.op",
				AdapterKey: "plugin.mcp",
				Variant:    tc.variant,
			}
			inv := &dispatch.Invocation{OpID: "x.op", Args: map[string]any{}}
			_, err := pm.Execute(context.Background(), inv, rv, &dispatch.Credentials{})
			if err == nil {
				t.Fatal("expected error for malformed binding, got nil")
			}
		})
	}
}

// TestPluginMCPReportsMissingInstall pins the documented v0.1.0 "plugin not
// installed" failure shape (cmd/gen-catalog/gen_flights.go): when the
// binding points to a plugin that isn't in the install root, the adapter
// MUST surface an error containing the plugin_id so the operator can
// install it. A successful return would silently mask the missing plugin
// state and corrupt the audit log.
func TestPluginMCPReportsMissingInstall(t *testing.T) {
	t.Parallel()
	pm := adapters.NewPluginMCP(plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()}))

	rv := &dispatch.ResolvedVariant{
		OpID:       "flights.search",
		AdapterKey: "plugin.mcp",
		Variant: &catalog.Variant{
			VariantID: "flights.v1.plugin.search",
			Binding: &catalog.Binding{
				AdapterKey: "plugin.mcp",
				PluginName: "google-flights",
				ToolName:   "flights_search",
			},
		},
	}
	inv := &dispatch.Invocation{OpID: "flights.search", Args: map[string]any{}}
	_, err := pm.Execute(context.Background(), inv, rv, &dispatch.Credentials{})
	if err == nil {
		t.Fatal("expected error for missing plugin install, got nil")
	}
	// A not-installed plugin surfaces as a structured SERVICE_DOWN carrying the
	// adapter_key + plugin_name + reason (spec §8) — so an agent branches on
	// error_code and a human gets a clear "install it" hint, instead of a bare
	// wrapped error string.
	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T (%v); want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeServiceDown {
		t.Errorf("ErrCode = %q; want SERVICE_DOWN", se.ErrCode)
	}
	if se.Detail["reason"] != "plugin_not_installed" {
		t.Errorf("Detail[reason] = %v; want plugin_not_installed", se.Detail["reason"])
	}
	if se.Detail["adapter_key"] != "plugin.mcp" {
		t.Errorf("Detail[adapter_key] = %v; want plugin.mcp", se.Detail["adapter_key"])
	}
	if se.Detail["plugin_name"] != "google-flights" {
		t.Errorf("Detail[plugin_name] = %v; want google-flights", se.Detail["plugin_name"])
	}
}

// TestPluginMCPCloseIdempotent guards against the adapter leaking subprocess
// handles when the running cache is empty. Close on a freshly-constructed
// adapter must be a no-op.
func TestPluginMCPCloseIdempotent(t *testing.T) {
	t.Parallel()
	pm := adapters.NewPluginMCP(plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()}))
	if err := pm.Close(context.Background()); err != nil {
		t.Errorf("Close on empty adapter = %v; want nil", err)
	}
	// Second Close — also a no-op.
	if err := pm.Close(context.Background()); err != nil {
		t.Errorf("Close (second call) = %v; want nil", err)
	}
}
