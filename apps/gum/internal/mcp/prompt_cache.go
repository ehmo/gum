// Spec §10.1: Tier A + Tier C schemas are emitted with provider-specific
// prompt-cache hints when the transport supports them. The MCP protocol
// does not standardize a `cache_control` field on Tool definitions, so GUM
// surfaces the hint via the `_meta` extension namespace — the convention
// used by Anthropic's first-party clients. Non-Anthropic clients ignore
// the unknown `_meta` keys and the tools work normally.
//
// `gum.cache_stats` reports the prompt layer's `supported` flag based on
// the connected client's Implementation.Name advertised at `initialize`.
// Any client whose name contains "claude" (case-insensitive) is treated as
// Anthropic-backed.

package mcp

import (
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// promptCacheHintMeta returns the `_meta` map attached to every Tier A and
// convenience tool definition. Anthropic clients translate
// `cache_control.type = "ephemeral"` into a 5-minute prompt-cache breakpoint
// on the tool-definitions block of the system prompt (Anthropic prompt-cache
// API). The key path mirrors Anthropic's native cache_control object so the
// hint passes through MCP→Anthropic glue layers without remapping.
func promptCacheHintMeta() sdkmcp.Meta {
	return sdkmcp.Meta{
		"cache_control": map[string]any{
			"type": "ephemeral",
		},
	}
}

// clientSupportsPromptCache reports whether the connected client is known to
// honour `_meta.cache_control` hints. v0.1.0 heuristic: substring "claude" in
// the client's Implementation.Name (case-insensitive). When the session or
// the InitializeParams are nil (CLI mode, pre-initialize, test stubs), the
// answer is false — spec §10.1 requires `supported` to be reported, not
// guessed.
func clientSupportsPromptCache(session *sdkmcp.ServerSession) bool {
	if session == nil {
		return false
	}
	params := session.InitializeParams()
	if params == nil || params.ClientInfo == nil {
		return false
	}
	return strings.Contains(strings.ToLower(params.ClientInfo.Name), "claude")
}
