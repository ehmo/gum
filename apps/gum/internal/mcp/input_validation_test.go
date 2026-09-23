package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Every Tier A tool advertises a closed inputSchema, but the SDK validates
// tool input only on its generic AddTool[In, Out] path. gum registers raw
// handlers, so nothing checked the arguments: a closed enum, a required field,
// a declared type and `additionalProperties: false` were all advisory, and a
// bad value reached the handler as a silent zero value.

// callMetaTool drives a meta tool the way the registered handler is driven,
// with args marshalled the way a client sends them.
func callMetaTool(t *testing.T, srv *Server, tool string, args map[string]any) *sdkmcp.CallToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	res, err := srv.makeMetaToolHandler(tool)(context.Background(), &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Name: tool, Arguments: raw},
	})
	if err != nil {
		t.Fatalf("%s handler returned a transport error: %v", tool, err)
	}
	return res
}

// resultText returns the first text content of a tool result.
func resultText(t *testing.T, res *sdkmcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("nil CallToolResult")
	}
	if len(res.Content) == 0 {
		return ""
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want TextContent", res.Content[0])
	}
	return tc.Text
}

// assertInvalidArgs fails unless res is an INVALID_ARGS error envelope.
func assertInvalidArgs(t *testing.T, res *sdkmcp.CallToolResult, why string) {
	t.Helper()
	if !res.IsError {
		t.Fatalf("%s: result is not an error; want INVALID_ARGS", why)
	}
	text := resultText(t, res)
	var env map[string]any
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("%s: error content is not a JSON envelope: %q", why, text)
	}
	if env["error_code"] != "INVALID_ARGS" {
		t.Errorf("%s: error_code = %v, want INVALID_ARGS (content %q)", why, env["error_code"], text)
	}
}

func TestMetaToolInputSchemaIsEnforced(t *testing.T) {
	srv := NewServer(noopDispatcher{})

	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
		why  string
	}{
		{
			name: "closed_format_enum",
			tool: "gum.read",
			args: map[string]any{"op_id": "gmail.users.messages.list", "format": "yaml"},
			why:  "format is a closed enum of toon|csv|json|markdown",
		},
		{
			name: "missing_required_op_id",
			tool: "gum.read",
			args: map[string]any{"format": "json"},
			why:  "op_id is required",
		},
		{
			name: "additional_property",
			tool: "gum.describe_op",
			args: map[string]any{"op_id": "gmail.users.messages.list", "nope": 1},
			why:  "additionalProperties is false",
		},
		{
			name: "wrong_type",
			tool: "gum.read",
			args: map[string]any{"op_id": "gmail.users.messages.list", "page_size": "ten"},
			why:  "page_size is an integer",
		},
		{
			name: "below_minimum",
			tool: "gum.search_apis",
			args: map[string]any{"query": "gmail", "k": 0},
			why:  "k has minimum 1",
		},
		{
			name: "above_maximum",
			tool: "gum.search_apis",
			args: map[string]any{"query": "gmail", "k": 50},
			why:  "k has maximum 20",
		},
		{
			name: "language_outside_v01_enum",
			tool: "gum.code",
			args: map[string]any{"language": "python", "source": "print(1)"},
			why:  "language is the closed enum risor",
		},
		{
			name: "destructive_budget_above_cap",
			tool: "gum.code",
			args: map[string]any{"language": "risor", "source": "1", "destructive_budget": 25},
			why:  "destructive_budget is capped at 20 (spec §6.1.1)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertInvalidArgs(t, callMetaTool(t, srv, tc.tool, tc.args), tc.why)
		})
	}
}

// TestValidInputStillReachesHandler pins that the gate rejects only what the
// schema rejects: a well-formed call must still run.
func TestValidInputStillReachesHandler(t *testing.T) {
	srv := NewServer(noopDispatcher{})
	res := callMetaTool(t, srv, "gum.search_apis", map[string]any{"query": "gmail labels", "k": 3})
	if res.IsError {
		t.Fatalf("a schema-valid gum.search_apis call was rejected: %q", resultText(t, res))
	}
}

// TestEveryRegisteredInputSchemaResolves keeps the gate from silently opening:
// a schema that cannot be resolved validates nothing, so the registration
// surface is checked here rather than discovered on a live call.
// The convenience roster is included even though its handlers are not gated
// yet (see makeConvenienceHandler): the schemas still have to resolve, and the
// gate goes on as soon as they describe what the kernel accepts.
func TestEveryRegisteredInputSchemaResolves(t *testing.T) {
	defs := append(MetaToolDefs(), TierAConvenienceToolDefs()...)
	if len(defs) == 0 {
		t.Fatal("no tool definitions to check")
	}
	for _, def := range defs {
		if _, err := resolveInputSchema(def.Schema); err != nil {
			t.Errorf("%s inputSchema does not resolve: %v", def.Name, err)
		}
	}
	for name, schema := range map[string]json.RawMessage{
		"skills_list": skillsListSchema(),
		"skills_get":  skillsGetSchema(),
	} {
		if _, err := resolveInputSchema(schema); err != nil {
			t.Errorf("%s inputSchema does not resolve: %v", name, err)
		}
	}
}

// TestUnresolvableSchemaFailsClosed pins the failure direction: a tool whose
// schema cannot be resolved rejects calls instead of accepting unvalidated
// input.
func TestUnresolvableSchemaFailsClosed(t *testing.T) {
	broken := json.RawMessage(`{"type":"object","properties":{"a":{"$ref":"#/$defs/missing"}}}`)
	h := validatedHandler("broken.tool", broken, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		t.Error("handler ran despite an unresolvable schema")
		return nil, nil
	})
	res, err := h(context.Background(), &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Name: "broken.tool", Arguments: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("a tool with an unresolvable schema accepted a call")
	}
	if !strings.Contains(resultText(t, res), "error_code") {
		t.Errorf("failure is not an error envelope: %q", resultText(t, res))
	}
}
