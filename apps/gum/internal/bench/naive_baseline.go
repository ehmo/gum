// Naive baseline synthetic MCP server (spec.md §2, bead gum-0ux).
//
// This file is the *denominator* used by `gum gain --fixture-replay` and by
// the in-tree release-gate token-budget tests when computing GUM's
// MCP-layer savings claim. Spec §2 fixes the naive baseline as a synthetic
// MCP server that:
//
//  1. Registers every catalog op as its own `tools/list` entry (no
//     meta-tool aggregation, no superseding, no compaction).
//  2. Uses the full uncompressed JSON Schema as each entry's `inputSchema`
//     (verbose title/description, every parameter spelled out, no
//     abbreviation, no field-mask hints).
//  3. Returns the raw upstream JSON response body unchanged on every
//     `tools/call` (no field masking, no expression profile, no string
//     truncation, no array collapse, no deduplication; `format=json`).
//  4. Sends no `fields` parameter upstream.
//
// Lowering any of these (e.g. collapsing duplicate variants, dropping
// optional params from the schema, truncating long strings in the
// response) makes the naive baseline cheaper and inflates the published
// savings number. Don't do it.
package bench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ehmo/gum/internal/catalog"
)

// NaiveTool is one tools/list entry produced by the naive baseline. The
// shape mirrors the MCP wire payload (`name`, `description`, `inputSchema`)
// but is materialised as a plain Go struct so callers can token-count
// individual entries without dragging in the MCP SDK.
type NaiveTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// NaiveToolsListPayload renders the naive `tools/list` reply for the
// supplied catalog: one NaiveTool per Op, sorted by OpID for byte
// determinism. The schema for each entry is the full uncompressed
// JSON Schema synthesised by NaiveInputSchema; the description is the
// op's title + summary joined by " — " so the verbose human-readable
// text contributes its full token cost (this is what a human-authored
// naive server would ship).
//
// Variants are intentionally NOT collapsed: an op with N variants still
// appears as a single tools/list entry because the catalog op_id is the
// stable user-visible name, but the synthetic schema enumerates every
// variant under a "variants" property so the naive baseline pays the
// full per-variant cost (mirroring a naive server that exposes each
// (op × variant) combination's surface area).
func NaiveToolsListPayload(c *catalog.Catalog) ([]NaiveTool, error) {
	if c == nil {
		return nil, fmt.Errorf("bench: NaiveToolsListPayload: nil catalog")
	}
	tools := make([]NaiveTool, 0, len(c.Ops))
	for i := range c.Ops {
		op := &c.Ops[i]
		schema, err := NaiveInputSchema(op)
		if err != nil {
			return nil, fmt.Errorf("bench: NaiveToolsListPayload: op %s: %w", op.OpID, err)
		}
		desc := op.Title
		if op.Summary != "" {
			if desc != "" {
				desc += " — "
			}
			desc += op.Summary
		}
		tools = append(tools, NaiveTool{
			Name:        op.OpID,
			Description: desc,
			InputSchema: schema,
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

// NaiveInputSchema renders the full uncompressed JSON Schema for one op.
// The schema is verbose by design: every required/optional parameter
// pair from the catalog is emitted as its own property carrying the
// declared JSON type, every variant is enumerated under a "variants"
// property so backend choice contributes its full schema cost, and
// additionalProperties is *not* set to false (the naive server permits
// extra params, mirroring a hand-written wrapper that hasn't been
// tightened yet).
//
// Marshaling uses MarshalIndent with two-space indent so the baseline
// pays the whitespace token cost a naive author's pretty-printed
// schema would carry; spec §2 explicitly rules out compaction.
func NaiveInputSchema(op *catalog.Op) (json.RawMessage, error) {
	if op == nil {
		return nil, fmt.Errorf("bench: NaiveInputSchema: nil op")
	}

	properties := map[string]any{}
	for _, p := range op.ParamsRequired {
		name, typ := paramNameType(p)
		if name == "" {
			continue
		}
		properties[name] = naivePropertySchema(typ, "Required parameter "+name+".")
	}
	for _, p := range op.ParamsOptional {
		name, typ := paramNameType(p)
		if name == "" {
			continue
		}
		if _, dup := properties[name]; dup {
			continue
		}
		properties[name] = naivePropertySchema(typ, "Optional parameter "+name+".")
	}

	properties["variants"] = naiveVariantsProperty(op)

	required := make([]string, 0, len(op.ParamsRequired))
	for _, p := range op.ParamsRequired {
		name, _ := paramNameType(p)
		if name != "" {
			required = append(required, name)
		}
	}
	sort.Strings(required)

	schema := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"type":        "object",
		"title":       op.Title,
		"description": op.Summary,
		"properties":  properties,
		"required":    required,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(schema); err != nil {
		return nil, fmt.Errorf("bench: NaiveInputSchema: marshal op %s: %w", op.OpID, err)
	}
	// json.Encoder appends a trailing newline; preserve it so naive output
	// matches what a hand-rolled server's `json.MarshalIndent` would emit
	// before being placed into the MCP envelope.
	return json.RawMessage(buf.Bytes()), nil
}

// paramNameType splits a [name, type] catalog param pair. Missing or
// malformed entries return ("", "").
func paramNameType(p []string) (name, typ string) {
	if len(p) >= 1 {
		name = p[0]
	}
	if len(p) >= 2 {
		typ = p[1]
	}
	if typ == "" {
		typ = "string"
	}
	return name, typ
}

// naivePropertySchema returns the per-property fragment a naive author
// would ship: just `type` + `description`, no enum tightening, no
// min/max bounds, no examples.
func naivePropertySchema(typ, desc string) map[string]any {
	return map[string]any{
		"type":        typ,
		"description": desc,
	}
}

// naiveVariantsProperty returns the "variants" property block: a string
// enum over every variant_id with verbose per-variant metadata included
// in the description. A naive author who didn't know about variant
// negotiation would hand-list every backend here.
func naiveVariantsProperty(op *catalog.Op) map[string]any {
	enum := make([]string, 0, len(op.Variants))
	for _, v := range op.Variants {
		enum = append(enum, v.VariantID)
	}
	sort.Strings(enum)

	desc := fmt.Sprintf("Backend variant selector for %s. One of: ", op.OpID)
	for i, v := range op.Variants {
		if i > 0 {
			desc += ", "
		}
		desc += fmt.Sprintf("%s (%s, %s, %s)", v.VariantID, v.Stability, v.InterfaceKind, v.BackendKind)
	}
	desc += "."

	return map[string]any{
		"type":        "string",
		"enum":        enum,
		"description": desc,
		"default":     op.DefaultVariantID,
	}
}

// NaiveResponseProcessor is the response-side passthrough required by
// spec §2: the naive server returns the raw upstream JSON body
// unchanged. No field masking, no profile, no truncation, no array
// collapse, no deduplication. The function returns a copy so the
// caller can't accidentally mutate fixture bytes through the alias.
//
// It is intentionally an identity function. The point is to document
// the contract in code so a future "let's just normalise whitespace"
// optimisation can't slip in and silently inflate the savings number.
func NaiveResponseProcessor(raw []byte) []byte {
	if raw == nil {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}

// NaiveToolsListJSON marshals the tools/list reply produced by
// NaiveToolsListPayload as a single JSON document — the exact bytes a
// naive MCP server would write to the wire in response to a
// `tools/list` request. Use this when measuring tool-definition tokens
// (spec §2 savings goal #1 component).
func NaiveToolsListJSON(c *catalog.Catalog) ([]byte, error) {
	tools, err := NaiveToolsListPayload(c)
	if err != nil {
		return nil, err
	}
	envelope := map[string]any{"tools": tools}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(envelope); err != nil {
		return nil, fmt.Errorf("bench: NaiveToolsListJSON: %w", err)
	}
	return buf.Bytes(), nil
}
