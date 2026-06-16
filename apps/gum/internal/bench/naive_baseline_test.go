package bench_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/bench"
	"github.com/ehmo/gum/internal/catalog"
)

// sampleCatalog returns a small synthetic Catalog covering the schema
// surface that NaiveToolsListPayload exercises: required params,
// optional params, multiple variants, and an op with no params at all.
func sampleCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-05-24T00:00:00Z",
		GeneratorVersion:     "test",
		Ops: []catalog.Op{
			{
				OpID:            "gmail.users.messages.list",
				OpSchemaVersion: 1,
				Title:           "List Gmail messages",
				Summary:         "List message IDs in a Gmail mailbox.",
				ParamsRequired:  [][]string{{"userId", "string"}},
				ParamsOptional:  [][]string{{"q", "string"}, {"maxResults", "integer"}},
				DefaultVariantID: "gmail.v1.list",
				Variants: []catalog.Variant{
					{
						VariantID:            "gmail.v1.list",
						VariantSchemaVersion: 1,
						Stability:            catalog.StabilityStable,
						InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
						BackendKind:          catalog.BackendKindTypedRestSDK,
						RiskClass:            catalog.RiskClassRead,
					},
					{
						VariantID:            "gmail.v1.list.raw",
						VariantSchemaVersion: 1,
						Stability:            catalog.StabilityBeta,
						InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
						BackendKind:          catalog.BackendKindRawHTTP,
						RiskClass:            catalog.RiskClassRead,
					},
				},
			},
			{
				OpID:             "calendar.events.list",
				OpSchemaVersion:  1,
				Title:            "List calendar events",
				Summary:          "List events in a calendar.",
				DefaultVariantID: "calendar.v3.list",
				Variants: []catalog.Variant{{
					VariantID:            "calendar.v3.list",
					VariantSchemaVersion: 1,
					Stability:            catalog.StabilityStable,
					InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
					BackendKind:          catalog.BackendKindTypedRestSDK,
					RiskClass:            catalog.RiskClassRead,
				}},
			},
		},
	}
}

// TestNaiveToolsListPayloadOnePerOp verifies the naive baseline registers
// exactly one tools/list entry per catalog op — no meta-tool aggregation,
// no superseding, no compaction (spec §2 contract).
func TestNaiveToolsListPayloadOnePerOp(t *testing.T) {
	c := sampleCatalog()
	tools, err := bench.NaiveToolsListPayload(c)
	if err != nil {
		t.Fatalf("NaiveToolsListPayload: %v", err)
	}
	if got, want := len(tools), len(c.Ops); got != want {
		t.Fatalf("tool count: got %d want %d", got, want)
	}
	// Sorted by op_id — calendar before gmail.
	if tools[0].Name != "calendar.events.list" || tools[1].Name != "gmail.users.messages.list" {
		t.Errorf("tools not sorted by op_id: %v", []string{tools[0].Name, tools[1].Name})
	}
}

// TestNaiveToolsListPayloadVerboseDescription verifies entries carry the
// op title + summary joined verbosely so the naive baseline pays the full
// human-readable description cost rather than the trimmed gum.read shape.
func TestNaiveToolsListPayloadVerboseDescription(t *testing.T) {
	c := sampleCatalog()
	tools, _ := bench.NaiveToolsListPayload(c)
	for _, tl := range tools {
		if !strings.Contains(tl.Description, " — ") {
			t.Errorf("tool %s description missing title/summary join: %q", tl.Name, tl.Description)
		}
	}
}

// TestNaiveInputSchemaIncludesEveryParam verifies every required and
// optional catalog param surfaces in the synthesised input schema — the
// naive baseline must NOT prune params the way gum.read does.
func TestNaiveInputSchemaIncludesEveryParam(t *testing.T) {
	op := sampleCatalog().Ops[0] // gmail.users.messages.list
	raw, err := bench.NaiveInputSchema(&op)
	if err != nil {
		t.Fatalf("NaiveInputSchema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing properties object: %v", schema)
	}
	for _, want := range []string{"userId", "q", "maxResults", "variants"} {
		if _, ok := props[want]; !ok {
			t.Errorf("schema missing property %q in %v", want, keys(props))
		}
	}
	req, ok := schema["required"].([]any)
	if !ok || len(req) != 1 || req[0].(string) != "userId" {
		t.Errorf("required: got %v want [userId]", req)
	}
}

// TestNaiveInputSchemaIsVerboseJSON verifies the schema is pretty-printed
// (multi-line, indented). The naive baseline pays whitespace token cost;
// compacting it to one line is what tightens the savings denominator.
func TestNaiveInputSchemaIsVerboseJSON(t *testing.T) {
	op := sampleCatalog().Ops[0]
	raw, _ := bench.NaiveInputSchema(&op)
	if bytes.Count(raw, []byte("\n")) < 5 {
		t.Errorf("schema appears compact; expected indented multi-line JSON, got:\n%s", raw)
	}
}

// TestNaiveInputSchemaEnumeratesVariants verifies the variants property
// includes every variant_id in its enum and references them in the
// description so backend choice contributes its full cost.
func TestNaiveInputSchemaEnumeratesVariants(t *testing.T) {
	op := sampleCatalog().Ops[0] // two variants
	raw, _ := bench.NaiveInputSchema(&op)
	var schema map[string]any
	_ = json.Unmarshal(raw, &schema)
	variants := schema["properties"].(map[string]any)["variants"].(map[string]any)
	enum := variants["enum"].([]any)
	if len(enum) != 2 {
		t.Errorf("variants enum len: got %d want 2", len(enum))
	}
	desc := variants["description"].(string)
	for _, id := range []string{"gmail.v1.list", "gmail.v1.list.raw"} {
		if !strings.Contains(desc, id) {
			t.Errorf("variants description missing %q in %q", id, desc)
		}
	}
}

// TestNaiveResponseProcessorIsByteIdentity verifies the response-side
// passthrough returns byte-for-byte equal output (a copy, not the
// original slice). This pins spec §2's "no field mask / no profile / no
// truncation / no collapse / no dedup" contract in code.
func TestNaiveResponseProcessorIsByteIdentity(t *testing.T) {
	in := []byte(`{"messages":[{"id":"abc","threadId":"xyz","snippet":"hello world"}]}`)
	out := bench.NaiveResponseProcessor(in)
	if !bytes.Equal(in, out) {
		t.Errorf("response processor mutated bytes:\n in: %s\nout: %s", in, out)
	}
	if len(in) > 0 && &in[0] == &out[0] {
		t.Error("response processor returned aliased slice; expected a copy")
	}
}

// TestNaiveResponseProcessorNilSafe verifies the passthrough handles nil
// (no panic, returns nil) — defensive against empty-fixture replays.
func TestNaiveResponseProcessorNilSafe(t *testing.T) {
	if out := bench.NaiveResponseProcessor(nil); out != nil {
		t.Errorf("nil in: got %v want nil", out)
	}
}

// TestNaiveToolsListJSONIsValid verifies the full tools/list envelope
// marshals to valid JSON and contains every op's name. This is the
// byte-stream a naive MCP server would write to the wire at session
// start — the denominator for tool-definition compression savings.
func TestNaiveToolsListJSONIsValid(t *testing.T) {
	c := sampleCatalog()
	body, err := bench.NaiveToolsListJSON(c)
	if err != nil {
		t.Fatalf("NaiveToolsListJSON: %v", err)
	}
	var env struct {
		Tools []bench.NaiveTool `json:"tools"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if len(env.Tools) != len(c.Ops) {
		t.Errorf("envelope tools: got %d want %d", len(env.Tools), len(c.Ops))
	}
	for _, op := range c.Ops {
		if !bytes.Contains(body, []byte(op.OpID)) {
			t.Errorf("envelope missing op_id %q", op.OpID)
		}
	}
}

// TestNaiveToolsListPayloadNilCatalog guards against nil-deref.
func TestNaiveToolsListPayloadNilCatalog(t *testing.T) {
	if _, err := bench.NaiveToolsListPayload(nil); err == nil {
		t.Error("expected error for nil catalog, got nil")
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
