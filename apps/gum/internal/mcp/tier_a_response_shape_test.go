package mcp

// Tier A response-shape conformance tests for spec §13 outputSchema $defs.
//
// Covers the three test-matrix Group A row 5 acceptance tests:
//   - TestCacheStatsOutputSchema       — validates handleCacheStats output
//   - TestGainOutputSchema             — validates handleGain output
//   - TestTierAResponseShapeConformance — validates representative fixtures
//     for ToonResult, SingleObjectResult, RawJsonResult, GainResult,
//     CacheStatsResult and confirms diff-only 304 responses (`{"unchanged":
//     true, "etag": "..."}`) are exempt (validation is skipped, not failed).
//
// Spec anchors:
//   - docs/spec.md §13 ToonResult/SingleObjectResult/RawJsonResult/GainResult/
//     CacheStatsResult (lines 2640-3046).
//   - docs/test-matrix.md row 5 ("Tier A representative structuredContent
//     validates against ... 304 responses are exempt").
//
// Schemas are inlined verbatim from spec.md §13 to make the conformance
// surface inspectable in this file. If the spec definitions change, regenerate
// these constants by re-extracting from docs/spec.md.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/ehmo/gum/internal/dispatch"
)

// expressionMetaDef is the §13 ExpressionMeta definition; embedded in every
// composite schema so $ref "#/$defs/ExpressionMeta" resolves locally.
const expressionMetaDef = `"ExpressionMeta": {
  "type": "object",
  "required": ["profile", "op_id", "variant_id", "lossy", "result_count"],
  "properties": {
    "profile":      {"type": "string"},
    "op_id":        {"type": "string"},
    "variant_id":   {"type": ["string", "null"]},
    "lossy":        {"type": "boolean"},
    "result_count": {"type": "integer", "minimum": 0},
    "omitted_count":{"type": "integer", "minimum": 0},
    "on_empty_message": {"type": ["string", "null"]},
    "full_result_path": {"type": "string"},
    "full_result_resource": {"type": "string"},
    "project_root_uri": {"type": ["string", "null"]},
    "artifact_expires_at": {"type": ["string", "null"]},
    "intentional_zero_max_items": {"type": ["boolean", "null"]},
    "_code_output_truncated": {"type": ["boolean", "null"]}
  },
  "additionalProperties": true
}`

// toonResultSpecSchema is the spec §13 ToonResult schema.
const toonResultSpecSchema = `{
  "$defs": {` + expressionMetaDef + `},
  "type": "object",
  "required": ["format", "toon", "_expression"],
  "properties": {
    "format": {"const": "toon"},
    "toon":   {"type": "string"},
    "op":     {"type": "string"},
    "variant":{"type": "string"},
    "next_page_token": {"type": "string"},
    "_expression": {"$ref": "#/$defs/ExpressionMeta"}
  },
  "additionalProperties": false
}`

// singleObjectResultSpecSchema is the spec §13 SingleObjectResult schema.
const singleObjectResultSpecSchema = `{
  "$defs": {` + expressionMetaDef + `},
  "type": "object",
  "required": ["format", "data", "_expression"],
  "properties": {
    "format": {"enum": ["json", "csv", "markdown"]},
    "data":   {},
    "_expression": {"$ref": "#/$defs/ExpressionMeta"}
  },
  "additionalProperties": false
}`

// rawJSONResultSpecSchema is the spec §13 RawJsonResult schema.
const rawJSONResultSpecSchema = `{
  "$defs": {` + expressionMetaDef + `},
  "type": "object",
  "required": ["format", "data", "_expression"],
  "properties": {
    "format": {"const": "json"},
    "data":   {},
    "_expression": {"$ref": "#/$defs/ExpressionMeta"}
  },
  "additionalProperties": false
}`

// gainResultSpecSchema is the spec §13 GainResult schema.
// `oneOf` enforces the mode-discriminator branches:
//   - summary  : sessions[] present, operations/history absent
//   - session  : operations[] present, sessions/history absent
//   - history  : history[]    present, sessions/operations absent
const gainResultSpecSchema = `{
  "type": "object",
  "required": ["mode", "window", "baseline_tokens", "actual_tokens",
    "savings_tokens", "savings_pct", "end_to_end_savings",
    "batch_envelope_overhead", "tokenizer"],
  "properties": {
    "mode":   {"enum": ["summary", "session", "history"]},
    "window": {"type": "string"},
    "baseline_tokens": {"type": "integer", "minimum": 0},
    "actual_tokens":   {"type": "integer", "minimum": 0},
    "savings_tokens":  {"type": "integer"},
    "savings_pct":     {"type": ["number", "null"]},
    "end_to_end_savings": {"type": ["number", "null"]},
    "batch_envelope_overhead": {"type": "integer", "minimum": 0},
    "per_op_shaping_savings":  {"type": ["number", "null"]},
    "by_tool": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["tool", "calls", "baseline_tokens", "actual_tokens"],
        "properties": {
          "tool":            {"type": "string"},
          "calls":           {"type": "integer", "minimum": 0},
          "baseline_tokens": {"type": "integer", "minimum": 0},
          "actual_tokens":   {"type": "integer", "minimum": 0}
        },
        "additionalProperties": false
      }
    },
    "sessions": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["session", "calls", "baseline_tokens", "actual_tokens",
          "savings_pct", "op_families"],
        "properties": {
          "session":         {"type": "string", "pattern": "^[0-9a-f]{8}$"},
          "calls":           {"type": "integer", "minimum": 0},
          "baseline_tokens": {"type": "integer", "minimum": 0},
          "actual_tokens":   {"type": "integer", "minimum": 0},
          "savings_pct":     {"type": ["number", "null"]},
          "op_families":     {"type": "array", "items": {"type": "string"}}
        },
        "additionalProperties": false
      }
    },
    "operations": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["op_id", "op_family", "calls", "baseline_tokens",
          "actual_tokens", "cache_status", "field_mask_status"],
        "properties": {
          "op_id":             {"type": "string"},
          "op_family":         {"type": "string"},
          "calls":             {"type": "integer", "minimum": 0},
          "baseline_tokens":   {"type": "integer", "minimum": 0},
          "actual_tokens":     {"type": "integer", "minimum": 0},
          "cache_status":      {"type": "string"},
          "field_mask_status": {"type": "string"}
        },
        "additionalProperties": false
      }
    },
    "history": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["session", "op_family", "baseline_tokens",
          "actual_tokens", "savings_pct"],
        "properties": {
          "session":         {"type": "string", "pattern": "^[0-9a-f]{8}$"},
          "op_family":       {"type": "string"},
          "baseline_tokens": {"type": "integer", "minimum": 0},
          "actual_tokens":   {"type": "integer", "minimum": 0},
          "savings_pct":     {"type": ["number", "null"]}
        },
        "additionalProperties": false
      }
    },
    "tokenizer": {"type": "string"}
  },
  "oneOf": [
    {
      "required": ["sessions"],
      "properties": {
        "mode":   {"const": "summary"},
        "window": {"pattern": "^(last-30-sessions|since:.+)$"}
      },
      "not": {"anyOf": [{"required": ["operations"]}, {"required": ["history"]}]}
    },
    {
      "required": ["operations"],
      "properties": {
        "mode":   {"const": "session"},
        "window": {"pattern": "^session:[0-9a-f]{8}$"}
      },
      "not": {"anyOf": [{"required": ["sessions"]}, {"required": ["history"]}]}
    },
    {
      "required": ["history"],
      "properties": {
        "mode":   {"const": "history"},
        "window": {"pattern": "^history(:since:.+)?$"}
      },
      "not": {"anyOf": [{"required": ["sessions"]}, {"required": ["operations"]}]}
    }
  ],
  "additionalProperties": false
}`

// cacheStatsResultSpecSchema is the spec §13 CacheStatsResult schema
// (lines 3003-3046).
const cacheStatsResultSpecSchema = `{
  "type": "object",
  "required": ["semantic", "http", "prompt", "audit_broken"],
  "properties": {
    "semantic": {
      "type": "object",
      "required": ["hits", "misses", "evictions", "entries", "bytes"],
      "properties": {
        "hits":      {"type": "integer", "minimum": 0},
        "misses":    {"type": "integer", "minimum": 0},
        "evictions": {"type": "integer", "minimum": 0},
        "entries":   {"type": "integer", "minimum": 0},
        "bytes":     {"type": "integer", "minimum": 0}
      },
      "additionalProperties": false
    },
    "http": {
      "type": "object",
      "required": ["hits", "misses", "entries", "bytes"],
      "properties": {
        "hits":    {"type": "integer", "minimum": 0},
        "misses":  {"type": "integer", "minimum": 0},
        "entries": {"type": "integer", "minimum": 0},
        "bytes":   {"type": "integer", "minimum": 0}
      },
      "additionalProperties": false
    },
    "prompt": {
      "type": "object",
      "required": ["supported", "hits_estimate"],
      "properties": {
        "supported":     {"type": "boolean"},
        "hits_estimate": {"type": ["integer", "null"], "minimum": 0}
      },
      "additionalProperties": false
    },
    "audit_broken": {"type": "boolean"}
  },
  "additionalProperties": false
}`

// compileSpecSchema parses one of the schema constants above.
func compileSpecSchema(t *testing.T, src string) *jsonschema.Resolved {
	t.Helper()
	var s jsonschema.Schema
	if err := json.Unmarshal([]byte(src), &s); err != nil {
		t.Fatalf("schema parse: %v", err)
	}
	r, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("schema resolve: %v", err)
	}
	return r
}

// minimalExpressionMeta returns the smallest ExpressionMeta that satisfies
// the §13 required-field set for use in fixture composition.
func minimalExpressionMeta(opID, variantID, profile string) map[string]any {
	return map[string]any{
		"profile":      profile,
		"op_id":        opID,
		"variant_id":   variantID,
		"lossy":        false,
		"result_count": 0,
	}
}

// isDiffOnly304 returns true for the spec-defined 304 diff-only envelope
// `{"unchanged": true, "etag": "..."}`. test-matrix row 5 carves these out
// as schema-validation-exempt; the test reports them as skipped rather than
// failing them against any Tier A $def.
func isDiffOnly304(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	unchanged, _ := m["unchanged"].(bool)
	_, hasETag := m["etag"].(string)
	return unchanged && hasETag
}

// -----------------------------------------------------------------------------
// TestCacheStatsOutputSchema
// -----------------------------------------------------------------------------

// TestCacheStatsOutputSchema invokes handleCacheStats and validates the JSON
// body against the spec §13 CacheStatsResult schema. Catches drift between
// the runtime envelope and the registered outputSchema.
func TestCacheStatsOutputSchema(t *testing.T) {
	isolateAuditSentinel(t)

	srv := NewServer(noopDispatcher{})
	body := invokeCacheStats(t, srv)

	rs := compileSpecSchema(t, cacheStatsResultSpecSchema)
	if err := rs.Validate(body); err != nil {
		raw, _ := json.MarshalIndent(body, "", "  ")
		t.Fatalf("cache_stats body fails CacheStatsResult schema: %v\nbody:\n%s", err, raw)
	}
}

// -----------------------------------------------------------------------------
// TestGainOutputSchema
// -----------------------------------------------------------------------------

// TestGainOutputSchema invokes handleGain and validates the JSON body against
// the spec §13 GainResult schema. A default invocation lands in the
// `summary` branch with `sessions: []`; the `oneOf` enforces that the other
// mode-specific arrays (operations, history) are absent.
func TestGainOutputSchema(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	srv := NewServer(noopDispatcher{})
	body := invokeGainExpectSuccess(t, srv)

	rs := compileSpecSchema(t, gainResultSpecSchema)
	if err := rs.Validate(body); err != nil {
		raw, _ := json.MarshalIndent(body, "", "  ")
		t.Fatalf("gain body fails GainResult schema: %v\nbody:\n%s", err, raw)
	}
}

// -----------------------------------------------------------------------------
// TestTierAResponseShapeConformance
// -----------------------------------------------------------------------------

// shapeFixture describes one representative structuredContent fixture and the
// $def it is expected to validate against. The diff-only 304 fixture is
// marked exemptDiff304 = true; the test must SKIP (not fail) that case per
// test-matrix row 5.
type shapeFixture struct {
	name          string
	def           string // schema constant (one of the *SpecSchema above)
	body          any
	exemptDiff304 bool
}

// TestTierAResponseShapeConformance feeds representative structuredContent
// fixtures through their declared $defs. Mirrors the structural assertion in
// spec §13 ("structuredContent MUST validate against the registered
// outputSchema") for the five Tier A response shapes plus the §13-exempt
// 304 diff-only envelope.
func TestTierAResponseShapeConformance(t *testing.T) {
	isolateAuditSentinel(t)
	t.Setenv("HOME", t.TempDir())

	// Live envelopes from the handlers (caught by integration with $defs).
	srv := NewServer(noopDispatcher{})
	cacheBody := invokeCacheStats(t, srv)
	gainBody := invokeGainExpectSuccess(t, srv)

	fixtures := []shapeFixture{
		{
			name: "ToonResult/representative",
			def:  toonResultSpecSchema,
			body: map[string]any{
				"format":      "toon",
				"toon":        "count: 0\nfields: id,name\n",
				"op":          "gmail.search",
				"variant":     "gmail.search.v1",
				"_expression": minimalExpressionMeta("gmail.search", "gmail.search.v1", "gmail.search"),
			},
		},
		{
			name: "SingleObjectResult/json",
			def:  singleObjectResultSpecSchema,
			body: map[string]any{
				"format":      "json",
				"data":        map[string]any{"id": "abc", "title": "hello"},
				"_expression": minimalExpressionMeta("docs.get", "docs.get.v1", "docs.get"),
			},
		},
		{
			name: "SingleObjectResult/markdown",
			def:  singleObjectResultSpecSchema,
			body: map[string]any{
				"format":      "markdown",
				"data":        "# Title\n\nbody",
				"_expression": minimalExpressionMeta("docs.get", "docs.get.v1", "docs.get"),
			},
		},
		{
			name: "RawJsonResult/raw_result_allowed",
			def:  rawJSONResultSpecSchema,
			body: map[string]any{
				"format":      "json",
				"data":        map[string]any{"opaque": []any{1, 2, 3}},
				"_expression": minimalExpressionMeta("genai.generate", "genai.generate.v1", "_raw"),
			},
		},
		{
			name: "GainResult/handler-summary",
			def:  gainResultSpecSchema,
			body: gainBody,
		},
		{
			name: "CacheStatsResult/handler",
			def:  cacheStatsResultSpecSchema,
			body: cacheBody,
		},
		{
			name:          "DiffOnly304/exempt",
			body:          map[string]any{"unchanged": true, "etag": "W/\"abc123\""},
			exemptDiff304: true,
		},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			if f.exemptDiff304 {
				if !isDiffOnly304(f.body) {
					t.Fatalf("fixture marked exemptDiff304 does not look like a 304 envelope: %+v", f.body)
				}
				t.Skip("304 diff-only envelopes are exempt from $def validation (test-matrix row 5)")
				return
			}
			rs := compileSpecSchema(t, f.def)
			if err := rs.Validate(f.body); err != nil {
				raw, _ := json.MarshalIndent(f.body, "", "  ")
				t.Fatalf("fixture %s fails its declared schema: %v\nbody:\n%s", f.name, err, raw)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// TestDispatchAndShapeStructuredContentValidates
// -----------------------------------------------------------------------------

// shapedDispatcher returns one fixed ShapedResponse so the live MCP seam can
// be driven through each §13 branch.
type shapedDispatcher struct{ resp *dispatch.ShapedResponse }

func (d shapedDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return d.resp, nil
}

// asJSON round-trips v through encoding/json so the validator sees the same
// value a client would: Go structs become objects and the json tags apply.
func asJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal structuredContent: %v", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	return out
}

// TestDispatchAndShapeStructuredContentValidates validates the
// structuredContent that dispatchAndShape actually emits, against both the
// spec §13 $def for the branch and the schema the tool registers.
//
// TestTierAResponseShapeConformance above validates hand-written literals, so
// it passed for months while no production path emitted `_expression` at all
// and structuredContent carried the pre-shaping payload. This test fails if
// the runtime and the registered schema drift apart again.
func TestDispatchAndShapeStructuredContentValidates(t *testing.T) {
	variant := "gmail.users.messages.list.v1"
	meta := func(profile, format string) *dispatch.ExpressionMeta {
		m := &dispatch.ExpressionMeta{
			Profile:      profile,
			OpID:         "gmail.users.messages.list",
			VariantID:    &variant,
			Lossy:        true,
			ResultCount:  1,
			OmittedCount: 2,
		}
		if format == "raw" {
			m.Profile = "_raw"
			m.Lossy = false
		}
		return m
	}

	for _, tc := range []struct {
		name string
		resp *dispatch.ShapedResponse
		def  string
	}{
		{
			name: "ToonResult",
			resp: &dispatch.ShapedResponse{
				Body:              []byte("messages[1]{id}:\n  m1\n"),
				Format:            "toon",
				StructuredContent: map[string]any{"messages": []any{map[string]any{"id": "m1"}}},
				Expression:        meta("gmail.messages.list.v1", "toon"),
			},
			def: toonResultSpecSchema,
		},
		{
			name: "SingleObjectResult/json",
			resp: &dispatch.ShapedResponse{
				Body:              []byte(`{"id":"m1"}`),
				Format:            "json",
				StructuredContent: map[string]any{"id": "m1"},
				Expression:        meta("gmail.messages.get.v1", "json"),
			},
			def: singleObjectResultSpecSchema,
		},
		{
			name: "SingleObjectResult/json-array-payload",
			resp: &dispatch.ShapedResponse{
				Body:              []byte(`[{"id":"m1"}]`),
				Format:            "json",
				StructuredContent: []any{map[string]any{"id": "m1"}},
				Expression:        meta("gmail.messages.list.v1", "json"),
			},
			def: singleObjectResultSpecSchema,
		},
		{
			name: "RawJsonResult",
			resp: &dispatch.ShapedResponse{
				Body:              []byte(`{"opaque":true}`),
				Format:            "raw",
				StructuredContent: map[string]any{"opaque": true},
				Expression:        meta("", "raw"),
			},
			def: rawJSONResultSpecSchema,
		},
		{
			name: "SingleObjectResult/csv",
			resp: &dispatch.ShapedResponse{
				Body:              []byte("id\nm1\n"),
				Format:            "csv",
				StructuredContent: map[string]any{"messages": []any{map[string]any{"id": "m1"}}},
				Expression:        meta("gmail.messages.list.v1", "csv"),
			},
			def: singleObjectResultSpecSchema,
		},
		{
			name: "SingleObjectResult/markdown",
			resp: &dispatch.ShapedResponse{
				Body:              []byte("| id |\n| --- |\n| m1 |\n"),
				Format:            "markdown",
				StructuredContent: map[string]any{"messages": []any{map[string]any{"id": "m1"}}},
				Expression:        meta("gmail.messages.list.v1", "markdown"),
			},
			def: singleObjectResultSpecSchema,
		},
		{
			// profile.Apply renames a format it cannot encode, so the wrapper
			// never has to guess: a name it does not know is TOON bytes.
			name: "UnimplementedFormatFallsBackToToon",
			resp: &dispatch.ShapedResponse{
				Body:              []byte("messages[1]{id}:\n  m1\n"),
				Format:            "toon",
				StructuredContent: map[string]any{"messages": []any{map[string]any{"id": "m1"}}},
				Expression:        meta("gmail.messages.list.v1", "toon"),
			},
			def: toonResultSpecSchema,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer(shapedDispatcher{resp: tc.resp})
			res, err := srv.dispatchAndShape(context.Background(), &dispatch.Invocation{
				OpID: "gmail.users.messages.list",
			})
			if err != nil {
				t.Fatalf("dispatchAndShape: %v", err)
			}
			if res.StructuredContent == nil {
				t.Fatal("structuredContent is nil; §13 requires the result envelope")
			}
			body := asJSON(t, res.StructuredContent)

			if err := compileSpecSchema(t, tc.def).Validate(body); err != nil {
				raw, _ := json.MarshalIndent(body, "", "  ")
				t.Fatalf("live structuredContent fails its §13 $def: %v\nbody:\n%s", err, raw)
			}

			// The registered schema must accept what the handler emits, or a
			// client that validates the response rejects a correct call.
			for tool, schema := range map[string]json.RawMessage{
				"gum.read":     metaToolOutputSchema("gum.read"),
				"gmail_search": convenienceToolOutputSchema("gmail_search"),
			} {
				if err := compileSpecSchema(t, string(schema)).Validate(body); err != nil {
					raw, _ := json.MarshalIndent(body, "", "  ")
					t.Fatalf("live structuredContent fails the registered %s outputSchema: %v\nbody:\n%s",
						tool, err, raw)
				}
			}
		})
	}
}

// TestRegisteredResultSchemasMatchSpecDefs pins the registered `$defs` to the
// spec §13 definitions inlined at the top of this file. The registered schema
// is what clients validate against, so a silent divergence from §13 is a
// wire-contract break that no other test would catch.
func TestRegisteredResultSchemasMatchSpecDefs(t *testing.T) {
	defsOf := func(raw json.RawMessage) map[string]any {
		t.Helper()
		var s struct {
			Defs map[string]any `json:"$defs"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("registered schema is not valid JSON: %v", err)
		}
		return s.Defs
	}
	specDefOf := func(src string) any {
		t.Helper()
		var s struct {
			Defs map[string]any `json:"$defs"`
			Type string         `json:"type"`
			Req  []string       `json:"required"`
			Prop map[string]any `json:"properties"`
			Add  bool           `json:"additionalProperties"`
		}
		if err := json.Unmarshal([]byte(src), &s); err != nil {
			t.Fatalf("spec schema is not valid JSON: %v", err)
		}
		return map[string]any{
			"type":                 s.Type,
			"required":             s.Req,
			"properties":           s.Prop,
			"additionalProperties": s.Add,
		}
	}

	registered := defsOf(shapedResultSchema())
	for name, specSrc := range map[string]string{
		"ToonResult":         toonResultSpecSchema,
		"SingleObjectResult": singleObjectResultSpecSchema,
		"RawJsonResult":      rawJSONResultSpecSchema,
	} {
		got, ok := registered[name]
		if !ok {
			t.Errorf("registered $defs is missing %q", name)
			continue
		}
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(specDefOf(specSrc))
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("%s drifted from spec §13:\n registered: %s\n spec:       %s", name, gotJSON, wantJSON)
		}
	}

	// ExpressionMeta must stay open per §13: a client that pins to the
	// registered schema must not reject a future _-prefixed field.
	em, ok := registered["ExpressionMeta"].(map[string]any)
	if !ok {
		t.Fatal("registered $defs is missing ExpressionMeta")
	}
	if open, _ := em["additionalProperties"].(bool); !open {
		t.Error("ExpressionMeta.additionalProperties must be true (spec §13 $comment)")
	}
	var specEM struct {
		Props map[string]any `json:"properties"`
	}
	if err := json.Unmarshal([]byte(expressionMetaDef[len(`"ExpressionMeta": `):]), &specEM); err != nil {
		t.Fatalf("spec ExpressionMeta is not valid JSON: %v", err)
	}
	gotProps, _ := em["properties"].(map[string]any)
	for field := range specEM.Props {
		if _, ok := gotProps[field]; !ok {
			t.Errorf("registered ExpressionMeta is missing §13 field %q", field)
		}
	}
}
