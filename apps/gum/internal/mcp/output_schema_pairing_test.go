package mcp

// Output-schema / structuredContent pairing gate (bead gum-1itj).
//
// MCP 2025-11-25 and spec §13 bind the two together: a tool that registers
// an `outputSchema` owes the client a `structuredContent` value that validates
// against it. The pinned go-sdk's low-level AddTool documents validation as
// "the caller's responsibility" and performs none, so a handler that returns
// only text ships the violation silently.
//
// Spec anchors, all §13 except the roster:
//   - structuredContent MUST validate against the registered outputSchema.
//   - isError envelopes are exempt, and a confirmation-required response
//     is one of them (REQUIRES_CONFIRMATION is a terminal §7 error code).
//   - every Tier A tool ships outputSchema + validating structuredContent.
//   - §9.4 roster: gum.gain → GainResult, gum.cache_stats → CacheStatsResult,
//     gum.describe_op → DescribeOpResult.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	skillreg "github.com/ehmo/gum/internal/skills"
)

// pairingDispatcher returns one §13-complete ShapedResponse for every
// invocation, so a tool that routes through dispatchAndShape emits a real
// envelope. A stub that omits Expression would make those tools emit no
// structuredContent for a reason that is the stub's fault, not the handler's.
type pairingDispatcher struct{}

func (pairingDispatcher) Dispatch(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	variant := inv.OpID + ".v1"
	return &dispatch.ShapedResponse{
		Body:              []byte("messages[1]{id}:\n  m1\n"),
		Format:            "toon",
		StructuredContent: map[string]any{"messages": []any{map[string]any{"id": "m1"}}},
		Expression: &dispatch.ExpressionMeta{
			Profile:     "test.pairing",
			OpID:        inv.OpID,
			VariantID:   &variant,
			Lossy:       false,
			ResultCount: 1,
		},
	}, nil
}

func (pairingDispatcher) CacheStats() dispatch.CacheLayerStats { return dispatch.CacheLayerStats{} }

// opIDForRisk returns a catalog op whose default variant carries want, so the
// risk-gate in handleRiskTier admits the call instead of rejecting it before
// dispatch.
func opIDForRisk(t *testing.T, srv *Server, want catalog.RiskClass) string {
	t.Helper()
	if srv.snapshot == nil {
		t.Fatal("server has no catalog snapshot")
	}
	for i := range srv.snapshot.Ops {
		op := &srv.snapshot.Ops[i]
		v := defaultVariant(op)
		if v != nil && v.RiskClass == want {
			return op.OpID
		}
	}
	t.Fatalf("no catalog op with default-variant risk_class %q", want)
	return ""
}

// pairingCallArgs is the per-tool argument table. Every registered tool needs
// an entry; the test fails on a missing one so a newly registered tool cannot
// slip past the gate.
func pairingCallArgs(t *testing.T, srv *Server) map[string]map[string]any {
	t.Helper()

	skills := skillreg.DefaultRegistry().List()
	if len(skills) == 0 {
		t.Fatal("no embedded skills; skills_get has nothing to fetch")
	}

	return map[string]map[string]any{
		"gum.search_apis": {"query": "messages"},
		"gum.describe_op": {"op_id": srv.snapshot.Ops[0].OpID},
		"gum.read":        {"op_id": opIDForRisk(t, srv, catalog.RiskClassRead), "args": map[string]any{}},
		"gum.write":       {"op_id": opIDForRisk(t, srv, catalog.RiskClassWrite), "args": map[string]any{}},
		"gum.destructive": {"op_id": opIDForRisk(t, srv, catalog.RiskClassDestructive), "args": map[string]any{}},
		"gum.code":        {"language": "risor", "source": "1"},
		"gum.poll":        {"operation_name": "ops/pairing"},
		"gum.cache_stats": {},
		"gum.gain":        {},

		"skills_list": {},
		"skills_get":  {"name": skills[0].Name},

		// Convenience arguments are the op's own argument names (spec §4.1), with
		// the body arriving as one object under the advertised body argument.
		// The roster runs behind validatedHandler, so a flat legacy name here
		// would fail the call with INVALID_ARGS instead of exercising the tool.
		"gmail_search":      {"userId": "me", "q": "is:unread"},
		"gmail_get_message": {"userId": "me", "id": "m1"},
		"gmail_send": {"userId": "me", "message": map[string]any{
			"raw": "dG86IGFAZXhhbXBsZS5jb20=",
		}},
		"gmail_create_draft": {"userId": "me", "message": map[string]any{
			"raw": "dG86IGFAZXhhbXBsZS5jb20=",
		}},
		"drive_find":     {"q": "name contains 'x'"},
		"drive_get_file": {"fileId": "f1"},
		"drive_share": {"fileId": "f1", "permission": map[string]any{
			"role": "reader",
			"type": "user",
		}},
		"calendar_upcoming": {"calendarId": "primary", "maxResults": 5},
		"calendar_create_event": {"calendarId": "primary", "event": map[string]any{
			"summary": "s",
			"start":   map[string]any{"dateTime": "2026-01-01T00:00:00Z"},
			"end":     map[string]any{"dateTime": "2026-01-01T01:00:00Z"},
		}},
		"calendar_update_event": {"calendarId": "primary", "eventId": "e1", "event": map[string]any{
			"summary": "s",
			"start":   map[string]any{"dateTime": "2026-01-01T00:00:00Z"},
			"end":     map[string]any{"dateTime": "2026-01-01T01:00:00Z"},
		}},
		"docs_get":     {"documentId": "d1"},
		"docs_create":  {"document": map[string]any{"title": "t"}},
		"sheets_read":  {"spreadsheetId": "s1", "range": "A1:B2"},
		"sheets_write": {"spreadsheetId": "s1", "range": "A1:B2", "values": []any{[]any{"a"}}},
		"slides_get":   {"presentationId": "p1"},
		"tasks_list":   {"tasklist": "@default"},
		"tasks_create": {"tasklist": "@default", "task": map[string]any{"title": "t"}},
		"flights_search": {
			"origin":         "SFO",
			"destination":    "JFK",
			"departure_date": "2026-01-01",
		},
	}
}

// TestEveryRegisteredToolPairsOutputSchemaWithStructuredContent drives the live
// MCP server over an in-memory transport, calls every tool in tools/list, and
// requires each one that advertises an outputSchema to return structuredContent
// that validates against the schema it advertised.
//
// Every call must succeed. §13 exempts isError envelopes from validation, so
// a test that tolerated an error result would let a broken tool pass the gate
// by failing.
func TestEveryRegisteredToolPairsOutputSchemaWithStructuredContent(t *testing.T) {
	isolateAuditSentinel(t)
	t.Setenv("HOME", t.TempDir())

	srv := NewServer(pairingDispatcher{})
	srv.pollerFactory = func(func(time.Duration)) lroPoller {
		return &fakePoller{result: fakePollResult{
			result: map[string]any{"name": "ops/pairing", "done": true},
		}}
	}
	args := pairingCallArgs(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "pairing-test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) == 0 {
		t.Fatal("tools/list returned nothing")
	}

	for _, tool := range listed.Tools {
		tool := tool
		t.Run(tool.Name, func(t *testing.T) {
			callArgs, ok := args[tool.Name]
			if !ok {
				t.Fatalf("no call arguments for %q; add an entry to pairingCallArgs so a new tool cannot skip this gate", tool.Name)
			}

			res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool.Name, Arguments: callArgs})
			if err != nil {
				t.Fatalf("CallTool(%s): %v", tool.Name, err)
			}
			if res.IsError {
				t.Fatalf("CallTool(%s) returned an error result: %s", tool.Name, firstText(res))
			}

			// A tool with no outputSchema is the other half of the pairing:
			// it may return text only. It must not return structuredContent
			// nobody can validate.
			if tool.OutputSchema == nil {
				if res.StructuredContent != nil {
					got, _ := json.Marshal(res.StructuredContent)
					t.Fatalf("%s registers no outputSchema but returned structuredContent: %s", tool.Name, got)
				}
				return
			}

			if res.StructuredContent == nil {
				t.Fatalf("%s advertises an outputSchema but returned no structuredContent (spec §13)", tool.Name)
			}

			schemaJSON, err := json.Marshal(tool.OutputSchema)
			if err != nil {
				t.Fatalf("marshal registered outputSchema for %s: %v", tool.Name, err)
			}
			if err := compileSpecSchema(t, string(schemaJSON)).Validate(res.StructuredContent); err != nil {
				got, _ := json.MarshalIndent(res.StructuredContent, "", "  ")
				t.Fatalf("%s structuredContent fails its registered outputSchema: %v\nstructuredContent:\n%s\nschema:\n%s",
					tool.Name, err, got, schemaJSON)
			}
		})
	}
}

// TestDescribeOpResultValidatesForEveryCatalogOp validates the describe_op
// payload for every op in the embedded catalog, not just the one the pairing
// test calls. Schema-breaking omissions are per-op: a variant with no `scopes`
// and a variant with no `execution_support` each fail a required §13 field
// while their neighbours pass.
func TestDescribeOpResultValidatesForEveryCatalogOp(t *testing.T) {
	srv := NewServer(pairingDispatcher{})
	if srv.snapshot == nil || len(srv.snapshot.Ops) == 0 {
		t.Fatal("embedded catalog snapshot is empty")
	}
	rs := compileSpecSchema(t, string(metaToolOutputSchema("gum.describe_op")))

	for i := range srv.snapshot.Ops {
		op := &srv.snapshot.Ops[i]
		t.Run(op.OpID, func(t *testing.T) {
			body := asJSON(t, buildDescribeOpResult(op, defaultMaxVariants))
			if err := rs.Validate(body); err != nil {
				got, _ := json.MarshalIndent(body, "", "  ")
				t.Fatalf("describe_op payload for %s fails the registered outputSchema: %v\npayload:\n%s",
					op.OpID, err, got)
			}
		})
	}
}
