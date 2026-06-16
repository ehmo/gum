// gum-99f: spec §10.1 — Tier A and convenience tool definitions emit
// provider-specific prompt-cache hints (Anthropic ephemeral) via the MCP
// `_meta.cache_control` field, and `gum.cache_stats` reports `prompt.supported`
// based on the connected client's Implementation.Name.

package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	gummcp "github.com/ehmo/gum/internal/mcp"
)

// TestToolsListEmitsAnthropicCacheControl asserts that every registered
// tool (meta-tools + convenience tools) carries `_meta.cache_control.type =
// "ephemeral"` in its definition. The hint is constant across clients
// because MCP `_meta` keys are ignored by clients that do not understand
// them, and tools/list is a single global broadcast.
func TestToolsListEmitsAnthropicCacheControl(t *testing.T) {
	srv := gummcp.NewServer(stubDispatcher{})
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "gum-99f-tools-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	res, err := cs.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("ListTools returned zero tools")
	}
	for _, tool := range res.Tools {
		if tool.Meta == nil {
			t.Errorf("tool %q has nil Meta; expected _meta.cache_control", tool.Name)
			continue
		}
		cc, ok := tool.Meta["cache_control"].(map[string]any)
		if !ok {
			t.Errorf("tool %q: _meta.cache_control missing or wrong type (%T)", tool.Name, tool.Meta["cache_control"])
			continue
		}
		if cc["type"] != "ephemeral" {
			t.Errorf("tool %q: _meta.cache_control.type=%v; want \"ephemeral\"", tool.Name, cc["type"])
		}
	}
}

// TestCacheStatsPromptSupportedReflectsClient asserts that `gum.cache_stats`
// returns `prompt.supported = true` when the connected client identifies as
// Claude, and `false` otherwise.
func TestCacheStatsPromptSupportedReflectsClient(t *testing.T) {
	cases := []struct {
		name       string
		clientName string
		want       bool
	}{
		{"claude_code", "claude-code", true},
		{"claude_ai_pascal", "Claude", true},
		{"mixed_case_claude_desktop", "Claude Desktop", true},
		{"non_anthropic", "openai-gpt-cli", false},
		{"empty_name_falls_back_false", "", false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			srv := gummcp.NewServer(stubDispatcher{})
			srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { _ = srv.Run(ctx, srvTransport) }()

			// SDK requires a non-empty client name to satisfy Implementation
			// validation; substitute a neutral placeholder for the empty-name
			// case and assert "false" via the substring rule.
			clientName := c.clientName
			if clientName == "" {
				clientName = "anonymous-client"
			}
			client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: clientName, Version: "0.0.1"}, nil)
			cs, err := client.Connect(ctx, clientTransport, nil)
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			defer func() { _ = cs.Close() }()

			result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{Name: "gum.cache_stats"})
			if err != nil {
				t.Fatalf("CallTool gum.cache_stats: %v", err)
			}
			if result.IsError {
				t.Fatalf("gum.cache_stats returned IsError: %s", contentText(result))
			}
			envelope := parseCacheStatsEnvelope(t, result)
			prompt, ok := envelope["prompt"].(map[string]any)
			if !ok {
				t.Fatalf("envelope.prompt missing or wrong type: %v", envelope["prompt"])
			}
			supported, ok := prompt["supported"].(bool)
			if !ok {
				t.Fatalf("prompt.supported missing or non-bool: %v", prompt["supported"])
			}
			if supported != c.want {
				t.Errorf("client=%q supported=%v; want %v", clientName, supported, c.want)
			}
			if _, ok := prompt["hits_estimate"]; !ok {
				t.Error("prompt.hits_estimate missing (spec §3035 requires the key, value may be null)")
			}
		})
	}
}

func parseCacheStatsEnvelope(t *testing.T, result *sdkmcp.CallToolResult) map[string]any {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result.Content empty")
	}
	tc, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T; want *TextContent", result.Content[0])
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &env); err != nil {
		t.Fatalf("envelope unmarshal failed (text=%q): %v", tc.Text, err)
	}
	return env
}

func contentText(result *sdkmcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
