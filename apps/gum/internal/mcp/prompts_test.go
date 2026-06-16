package mcp_test

import (
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"
)

// TestPromptRegistration is the bead-named acceptance for gum-z6w: the
// spec §13 prompts surface MUST advertise exactly the two zero-argument
// templates and every advertised name MUST be fetchable via prompts/get.
func TestPromptRegistration(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	res, err := cs.ListPrompts(ctx, &sdkmcp.ListPromptsParams{})
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	want := map[string]bool{
		"gum.summarize_workspace_for_today": false,
		"gum.audit_recent_writes":           false,
	}
	if len(res.Prompts) != len(want) {
		names := make([]string, 0, len(res.Prompts))
		for _, p := range res.Prompts {
			names = append(names, p.Name)
		}
		t.Fatalf("ListPrompts returned %d prompts (%v); want exactly %d (%v)",
			len(res.Prompts), names, len(want), keysOf(want))
	}
	for _, p := range res.Prompts {
		if _, ok := want[p.Name]; !ok {
			t.Errorf("unexpected prompt %q advertised; v0.1.0 roster is closed", p.Name)
			continue
		}
		want[p.Name] = true
		if p.Description == "" {
			t.Errorf("prompt %q has empty description", p.Name)
		}

		got, err := cs.GetPrompt(ctx, &sdkmcp.GetPromptParams{Name: p.Name})
		if err != nil {
			t.Errorf("GetPrompt(%s): %v", p.Name, err)
			continue
		}
		if len(got.Messages) != 1 {
			t.Errorf("GetPrompt(%s) returned %d messages; want 1", p.Name, len(got.Messages))
			continue
		}
		tc, ok := got.Messages[0].Content.(*sdkmcp.TextContent)
		if !ok {
			t.Errorf("GetPrompt(%s) message content type = %T; want *TextContent", p.Name, got.Messages[0].Content)
			continue
		}
		if !strings.Contains(tc.Text, "GUM MCP server") {
			t.Errorf("GetPrompt(%s) body missing 'GUM MCP server' anchor; got:\n%s", p.Name, tc.Text)
		}
	}
	for name, fetched := range want {
		if !fetched {
			t.Errorf("prompt %q missing from prompts/list", name)
		}
	}
}

// TestPromptZeroArgumentContract pins the spec §13 line 3164 invariant: both
// v0.1.0 prompts are zero-argument. Declaring an `arguments` array on the
// advertised prompt or accepting arguments at prompts/get time silently
// breaks clients that bypass the schema and pass keyword args.
func TestPromptZeroArgumentContract(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	res, err := cs.ListPrompts(ctx, &sdkmcp.ListPromptsParams{})
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	for _, p := range res.Prompts {
		if len(p.Arguments) != 0 {
			t.Errorf("prompt %q advertised %d arguments; want 0 (v0.1.0 closed roster)", p.Name, len(p.Arguments))
		}
		// Passing a stray argument MUST be rejected by the handler so a
		// client cannot use the v0.1.0 zero-argument surface as a
		// templating channel.
		_, err := cs.GetPrompt(ctx, &sdkmcp.GetPromptParams{
			Name:      p.Name,
			Arguments: map[string]string{"unexpected": "value"},
		})
		if err == nil {
			t.Errorf("GetPrompt(%s) with extra argument succeeded; want error", p.Name)
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
