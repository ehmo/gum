package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/plugins"
)

// PluginMCP is the dispatch.Adapter that routes plugin-bound variants
// (backend_kind=mcp-plugin) to a Shape 1 subprocess managed by
// internal/plugins.Host. Spec §8.2: every plugin-bound variant carries a
// binding with adapter_key="plugin.mcp", plugin_name, and tool_name; this
// adapter consumes that triple to spawn the subprocess (idempotently) and
// forward the MCP tools/call.
//
// The adapter caches running plugin handles by plugin_id so repeated calls in
// the same gum process reuse the subprocess instead of paying the connect
// handshake on every invocation. v0.1.0 keeps the cache process-local; the
// starter hook wires the supervisor that drives crash quarantine in production.
type PluginMCP struct {
	hostOnce sync.Once
	hostFn   func() *plugins.Host
	host     *plugins.Host
	startFn  func(context.Context, *plugins.Host, string) (*plugins.Plugin, error)

	mu      sync.Mutex
	running map[string]*plugins.Plugin
}

// NewPluginMCP returns a PluginMCP adapter that dispatches to plugins
// installed under the supplied Host's install root. Callers wire the adapter
// into the dispatcher's adapter map under the key "plugin.mcp" (matching the
// catalog binding's adapter_key — see cmd/gen-catalog/gen_flights.go).
func NewPluginMCP(host *plugins.Host) *PluginMCP {
	return &PluginMCP{
		host:    host,
		running: map[string]*plugins.Plugin{},
	}
}

// NewPluginMCPLazy defers Host construction until the first plugin-bound
// dispatch. The factory closure runs at most once, behind sync.Once. CLI
// invocations that never touch a plugin pay zero cost for host setup
// (registry walk, plugin-state.json read).
func NewPluginMCPLazy(hostFn func() *plugins.Host) *PluginMCP {
	return NewPluginMCPLazyWithStarter(hostFn, nil)
}

// NewPluginMCPLazyWithStarter defers Host construction like NewPluginMCPLazy,
// but lets production entrypoints wrap host.Start with the plugin Supervisor so
// quarantine/backoff state is enforced before a subprocess is spawned.
func NewPluginMCPLazyWithStarter(hostFn func() *plugins.Host, startFn func(context.Context, *plugins.Host, string) (*plugins.Plugin, error)) *PluginMCP {
	return &PluginMCP{
		hostFn:  hostFn,
		startFn: startFn,
		running: map[string]*plugins.Plugin{},
	}
}

func (p *PluginMCP) resolveHost() *plugins.Host {
	p.hostOnce.Do(func() {
		if p.host == nil && p.hostFn != nil {
			p.host = p.hostFn()
		}
	})
	return p.host
}

// Execute satisfies dispatch.Adapter. It pulls plugin_name + tool_name from
// the resolved variant's binding, ensures the subprocess is running, and
// dispatches the call. Errors map onto the spec §8 envelope codes through the
// host's MapPluginError path; this adapter wraps connection-time failures as
// SERVICE_DOWN (the "plugin not installed" failure shape from spec §8.2).
func (p *PluginMCP) Execute(ctx context.Context, inv *dispatch.Invocation, rv *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	if rv == nil || rv.Variant == nil || rv.Variant.Binding == nil {
		return nil, fmt.Errorf("plugin.mcp: resolved variant missing binding")
	}
	pluginID := rv.Variant.Binding.PluginName
	toolName := rv.Variant.Binding.ToolName
	if pluginID == "" || toolName == "" {
		return nil, fmt.Errorf("plugin.mcp: binding missing plugin_name or tool_name")
	}

	plug, err := p.ensureRunning(ctx, pluginID)
	if err != nil {
		// A plugin-bound op that can't start its plugin surfaces as SERVICE_DOWN
		// with the adapter_key (spec §8 line 1631), not a bare error string — so
		// an agent can branch on error_code and a human gets the plugin name. The
		// common case is "plugin not installed" (no manifest), which is expected
		// for the unofficial ops (flights/scholar/…) until their plugin is added.
		se := dispatch.NewStructuredError(dispatch.ErrCodeServiceDown,
			fmt.Sprintf("plugin %q is unavailable for %s", pluginID, inv.OpID)).
			WithDetail("adapter_key", rv.Variant.Binding.AdapterKey).
			WithDetail("plugin_name", pluginID).
			WithRetryable(false)
		if errors.Is(err, plugins.ErrManifestNotFound) {
			se = se.WithDetail("reason", "plugin_not_installed").
				WithDetail("hint", fmt.Sprintf("the %q plugin is not installed; install the plugin that provides %s before calling it", pluginID, inv.OpID))
		} else {
			se = se.WithDetail("detail", err.Error())
		}
		return nil, se
	}

	raw, callErr := plug.CallTool(ctx, toolName, inv.Args)
	if callErr != nil {
		// The plugin host already mapped the upstream envelope through
		// MapPluginError; the raw payload carries error_code + retry hints.
		// Returning the JSON body lets the dispatch lifecycle expose the
		// structured envelope rather than swallowing detail in the error
		// string. The non-nil err marks the call as failed so step 7 records
		// it as such; callers inspect Response.Body for the envelope.
		return &dispatch.Response{
			Body:   raw,
			Format: "json",
		}, callErr
	}

	// Successful tools/call returns either a single TextContent (Body
	// already plain) or a JSON-serialised content array. CallTool's
	// contract is "JSON-marshalable bytes" — we preserve them verbatim so
	// the output pipeline can apply field-mask / expression-profile shaping.
	if len(raw) > 0 && raw[0] != '{' && raw[0] != '[' {
		// Wrap non-JSON text bodies in a minimal envelope so downstream
		// JSON-aware stages (cache, tee, gain) don't choke. Plugins that
		// return free-form text get a stable JSON shape with one field.
		wrapped, _ := json.Marshal(map[string]any{"text": string(raw)})
		raw = wrapped
	}
	return &dispatch.Response{
		Body:   raw,
		Format: "json",
	}, nil
}

// ensureRunning starts the plugin subprocess on first use and reuses the
// cached handle on subsequent calls. Concurrent callers serialise on the
// adapter mutex so the second arrival waits for the first to complete the
// MCP handshake instead of racing to spawn duplicate processes.
func (p *PluginMCP) ensureRunning(ctx context.Context, pluginID string) (*plugins.Plugin, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if plug, ok := p.running[pluginID]; ok && plug != nil {
		return plug, nil
	}
	host := p.resolveHost()
	if host == nil {
		return nil, fmt.Errorf("plugin.mcp: no host configured")
	}
	start := p.startFn
	if start == nil {
		start = func(ctx context.Context, host *plugins.Host, pluginID string) (*plugins.Plugin, error) {
			return host.Start(ctx, pluginID)
		}
	}
	plug, err := start(ctx, host, pluginID)
	if err != nil {
		return nil, err
	}
	p.running[pluginID] = plug
	return plug, nil
}

// Close stops every running plugin handle the adapter started. CLI one-shots
// can ignore the return value; long-running processes (mcp --stdio) wire
// Close into the shutdown ladder so subprocess pipes get torn down cleanly.
func (p *PluginMCP) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var firstErr error
	for id, plug := range p.running {
		if err := plug.Stop(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("plugin.mcp: stop %s: %w", id, err)
		}
		delete(p.running, id)
	}
	return firstErr
}
