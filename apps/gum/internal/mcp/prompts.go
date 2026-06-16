package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// staticPrompt is the host-side declaration of one v0.1.0 prompt: name,
// human description, and the assembled message body. Spec §13 line 3164
// pins the v0.1.0 prompt roster to exactly two zero-argument templates;
// adding or removing one requires a minor-version spec PR (tracked under
// gum-z6w).
type staticPrompt struct {
	Name        string
	Title       string
	Description string
	Body        string
}

// staticPrompts is the closed v0.1.0 prompt roster. Both templates are
// zero-argument: they bake in deterministic instructions for the host LLM
// rather than templating user-provided arguments. The first call to
// prompts/get returns the prompt verbatim; the client decides whether to
// inject it as a system or user message.
var staticPrompts = []staticPrompt{
	{
		Name:        "gum.summarize_workspace_for_today",
		Title:       "Summarize my workspace for today",
		Description: "Surveys Gmail, Calendar, and Drive for today and produces a one-screen briefing. Zero-argument; intended as a session-start kickoff.",
		Body: `You have access to the GUM MCP server. Build a one-screen briefing of the user's workspace for today (the host's local date) by:

1. Call gum.search_apis with query="calendar events today" and pick gum.read targets from the results.
2. Call gum.search_apis with query="gmail unread today" and gum.read the highest-priority threads.
3. Call gum.search_apis with query="drive files modified today" and surface the top changed documents.

Render the result as three short sections (Calendar, Inbox, Drive). Each row is one line: title, time/sender/owner, and a one-sentence action hint. Do NOT call gum.write or gum.destructive. If a section is empty, say so explicitly rather than padding.`,
	},
	{
		Name:        "gum.audit_recent_writes",
		Title:       "Audit recent writes",
		Description: "Walks the last 24h of write/destructive operations from the audit trail and the gain ledger. Zero-argument; intended for the end-of-session review surface.",
		Body: `You have access to the GUM MCP server. Audit the last 24 hours of write/destructive operations by:

1. Call gum.cache_stats to confirm the audit subsystem is not broken (audit_broken=false).
2. Call gum.gain with timeframe="last_24h" to inventory operations and their gain scores.
3. Filter to risk_class in {write, destructive}; surface each row's op_id, variant_id, timestamp, and gain delta.

Render the result as a chronological list. For each entry note: (a) whether a confirmation token was issued and consumed, (b) whether the gain delta is positive (the operation was useful) or negative (the operation cost more than it returned). Flag any destructive operation that lacks a paired confirmation token as a session-level alert.`,
	},
}

// registerPrompts wires the v0.1.0 static prompt roster into the SDK. The
// SDK auto-advertises the prompts capability the first time AddPrompt is
// called, so this method MUST run before Server.Run.
func (s *Server) registerPrompts() {
	for _, p := range staticPrompts {
		prompt := p // capture
		s.sdkSrv.AddPrompt(
			&sdkmcp.Prompt{
				Name:        prompt.Name,
				Title:       prompt.Title,
				Description: prompt.Description,
			},
			func(_ context.Context, req *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
				if len(req.Params.Arguments) != 0 {
					return nil, fmt.Errorf("prompt %s: zero-argument; got %d arguments", prompt.Name, len(req.Params.Arguments))
				}
				return &sdkmcp.GetPromptResult{
					Description: prompt.Description,
					Messages: []*sdkmcp.PromptMessage{
						{
							Role:    sdkmcp.Role("user"),
							Content: &sdkmcp.TextContent{Text: prompt.Body},
						},
					},
				}, nil
			},
		)
	}
}
