// Package mcp — Red Team failing tests for gum-9vuq.11.
//
// These tests assert the acceptance criteria for issue gum-9vuq.11:
// 4 write convenience tools (gmail_send, gmail_create_draft,
// calendar_create_event, calendar_update_event) must have:
//  1. Typed schemas with additionalProperties:false and >= 1 required typed field.
//  2. Optional `confirmed` (bool) and `confirmation_token` (string) fields
//     per spec.md §4.1 (confirmation_passthrough=yes row contract).
//  3. MCP annotations: readOnlyHint=false, destructiveHint=false.
//  4. Calling the tool handler without confirmed=true returns a
//     REQUIRES_CONFIRMATION envelope (spec.md §6 / §4.1).
//
// ALL FOUR TESTS ARE EXPECTED TO FAIL until the Green Team ships:
//   - confirmation fields in schemas for the 4 write tools
//   - Annotations on those 4 tool defs (readOnlyHint=false, destructiveHint=false)
//   - Convenience handler confirmation gate (confirmed=false → REQUIRES_CONFIRMATION)
//
// Spec anchors:
//
//	spec.md §4.1 table rows: gmail_send, gmail_create_draft,
//	                          calendar_create_event, calendar_update_event
//	spec.md §4.1: "Their inputSchema is generated from the listed required/optional
//	               args plus confirmed? / confirmation_token? when
//	               confirmation_passthrough=yes."
//	spec.md §6 / §1421: REQUIRES_CONFIRMATION stable error code.
//
// Done criterion:
//
//	go test ./internal/mcp/... -run TestTierAWrite -count=1
//	All 4 subtests FAIL (red state).
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// writeToolNames are the 4 write convenience tools that must implement the
// confirmation_passthrough=yes contract per spec.md §4.1.
var writeToolNames = []string{
	"gmail_send",
	"gmail_create_draft",
	"calendar_create_event",
	"calendar_update_event",
}

// writeToolRequiredFields specifies at least one required field per tool
// that must appear in the schema's "required" array.
var writeToolRequiredFields = map[string]string{
	"gmail_send":            "to",
	"gmail_create_draft":    "to",
	"calendar_create_event": "calendarId",
	"calendar_update_event": "calendarId",
}

// --- helpers ---

// schemaRequired returns the list of required property names from a schema.
func schemaRequired(raw json.RawMessage) []string {
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	reqSlice, ok := s["required"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(reqSlice))
	for _, v := range reqSlice {
		if sv, ok := v.(string); ok {
			out = append(out, sv)
		}
	}
	return out
}

// schemaPropertyType returns the "type" of a named property, or "" if absent.
func schemaPropertyType(raw json.RawMessage, propName string) string {
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	props, ok := s["properties"].(map[string]any)
	if !ok {
		return ""
	}
	propDef, ok := props[propName].(map[string]any)
	if !ok {
		return ""
	}
	t, _ := propDef["type"].(string)
	return t
}

// schemaHasProperty returns true when the schema declares the named property.
func schemaHasProperty(raw json.RawMessage, propName string) bool {
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	props, ok := s["properties"].(map[string]any)
	if !ok {
		return false
	}
	_, exists := props[propName]
	return exists
}

// isRequired returns true when name appears in the schema's required array.
func isRequired(raw json.RawMessage, name string) bool {
	for _, r := range schemaRequired(raw) {
		if r == name {
			return true
		}
	}
	return false
}

// writeToolSchemas extracts schemas for the 4 write tools from TierAConvenienceToolDefs.
func writeToolSchemas(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	defs := TierAConvenienceToolDefs()
	schemas := make(map[string]json.RawMessage, len(writeToolNames))
	for _, d := range defs {
		for _, wn := range writeToolNames {
			if d.Name == wn {
				schemas[wn] = d.Schema
			}
		}
	}
	for _, wn := range writeToolNames {
		if schemas[wn] == nil {
			t.Errorf("write tool %q not found in TierAConvenienceToolDefs()", wn)
		}
	}
	return schemas
}

// --- Test 1: typed schemas with additionalProperties:false and required fields ---

// TestTierAWriteToolsDeclareTypedSchemas asserts that each of the 4 write tools:
//   - Has additionalProperties:false
//   - Has at least one required property (per spec §4.1 table row)
//   - That required property has an explicit type (not just {})
//
// Expected failure: current schemas for gmail_send / gmail_create_draft lack
// `to` as a typed required field; calendar tools lack calendarId in required list.
// (Check the actual current schema in schemas.go — if the required field happens
// to already be present, the subtests for confirmation fields will still fail.)
func TestTierAWriteToolsDeclareTypedSchemas(t *testing.T) {
	schemas := writeToolSchemas(t)

	for _, name := range writeToolNames {
		name := name
		raw, ok := schemas[name]
		if !ok {
			continue
		}
		t.Run(name, func(t *testing.T) {
			// 1. additionalProperties:false
			if !schemaHasAdditionalPropertiesFalse(raw) {
				t.Errorf("%s: schema must set additionalProperties:false (spec §4.1 criterion 4)", name)
			}

			// 2. at least one declared property
			if schemaPropertyCount(raw) < 1 {
				t.Errorf("%s: schema must declare at least one property", name)
			}

			// 3. the spec-mandated required field is present and typed
			requiredField := writeToolRequiredFields[name]
			if !schemaHasProperty(raw, requiredField) {
				t.Errorf("%s: schema missing required property %q (spec §4.1 table)", name, requiredField)
			}
			if !isRequired(raw, requiredField) {
				t.Errorf("%s: property %q must be in schema required[] (spec §4.1 table)", name, requiredField)
			}
			typ := schemaPropertyType(raw, requiredField)
			if typ == "" {
				t.Errorf("%s: required property %q must declare a type (spec §4.1 acceptance criterion 3)", name, requiredField)
			}
		})
	}
}

// --- Test 2: confirmed + confirmation_token optional fields ---

// TestTierAWriteToolsExposeConfirmationFields asserts that each write tool's schema
// includes optional `confirmed` (type:boolean) and `confirmation_token` (type:string)
// properties, and that neither is listed as required.
//
// Spec anchor: spec.md §4.1: "confirmed? / confirmation_token? when
// confirmation_passthrough=yes".
//
// Expected failure: current schemas for all 4 tools omit both fields entirely.
func TestTierAWriteToolsExposeConfirmationFields(t *testing.T) {
	schemas := writeToolSchemas(t)

	for _, name := range writeToolNames {
		name := name
		raw, ok := schemas[name]
		if !ok {
			continue
		}
		t.Run(name, func(t *testing.T) {
			// confirmed: optional bool
			if !schemaHasProperty(raw, "confirmed") {
				t.Errorf("%s: schema missing optional property \"confirmed\" (spec §4.1 confirmation_passthrough=yes)", name)
			} else {
				typ := schemaPropertyType(raw, "confirmed")
				if typ != "boolean" {
					t.Errorf("%s: confirmed.type = %q; want \"boolean\"", name, typ)
				}
				if isRequired(raw, "confirmed") {
					t.Errorf("%s: \"confirmed\" must be optional (not in required[])", name)
				}
			}

			// confirmation_token: optional string
			if !schemaHasProperty(raw, "confirmation_token") {
				t.Errorf("%s: schema missing optional property \"confirmation_token\" (spec §4.1 confirmation_passthrough=yes)", name)
			} else {
				typ := schemaPropertyType(raw, "confirmation_token")
				if typ != "string" {
					t.Errorf("%s: confirmation_token.type = %q; want \"string\"", name, typ)
				}
				if isRequired(raw, "confirmation_token") {
					t.Errorf("%s: \"confirmation_token\" must be optional (not in required[])", name)
				}
			}
		})
	}
}

// --- Test 3: MCP tool annotations ---

// writeToolAnnotationDef carries the tool def with annotation for inspection.
// The Green Team must add WriteConvenienceToolDefs() or expose the annotations
// via the existing TierAConvenienceToolDefs ToolDef struct. Until then this test
// accesses annotations through a new ToolDefWithAnnotations export.
//
// To avoid a compile error before the Green Team ships, we call the existing
// TierAConvenienceToolDefs and check a new Annotations field on ToolDef.
// If that field does not exist, the test fails structurally.
//
// TestTierAWriteToolsAnnotations asserts readOnlyHint=false and destructiveHint=false
// for each of the 4 write tools (spec §4.1: write ops are not read-only and not
// destructive in the MCP annotation model).
//
// Expected failure: server.go never sets Annotations on sdkmcp.Tool for convenience
// tools, so annotations are absent entirely. A missing annotation != readOnlyHint=false
// per the go-sdk (ReadOnlyHint defaults to false, DestructiveHint defaults to true
// when omitted and ReadOnlyHint=false). The test asserts explicit declarations.
func TestTierAWriteToolsAnnotations(t *testing.T) {
	// Access tool annotations via a new accessor that the Green Team must add.
	// If TierAConvenienceToolAnnotations() is not yet defined, the test fails
	// at compile time, which is the desired red state.
	annots := TierAConvenienceToolAnnotations()

	for _, name := range writeToolNames {
		name := name
		t.Run(name, func(t *testing.T) {
			ann, ok := annots[name]
			if !ok || ann == nil {
				t.Fatalf("%s: no annotation entry returned by TierAConvenienceToolAnnotations(); "+
					"Green Team must add explicit sdkmcp.ToolAnnotations for write tools "+
					"(spec §4.1: readOnlyHint=false, destructiveHint=false)", name)
			}

			// readOnlyHint must be false (it is a write tool, not read-only).
			if ann.ReadOnlyHint {
				t.Errorf("%s: readOnlyHint = true; write tools must set readOnlyHint=false "+
					"(spec §4.1 MCP annotations: write, not destructive)", name)
			}

			// destructiveHint must be explicitly false (write ≠ destructive).
			if ann.DestructiveHint == nil {
				t.Errorf("%s: destructiveHint must be explicitly false (not nil/default=true); "+
					"write tools are non-destructive per spec §4.1 annotation contract", name)
			} else if *ann.DestructiveHint {
				t.Errorf("%s: destructiveHint = true; write tools must set destructiveHint=false "+
					"(spec §4.1: write ops are not destructive in MCP annotation model)", name)
			}
		})
	}
}

// --- Test 4: calling without confirmed=true returns REQUIRES_CONFIRMATION ---

type writeConfirmationAdapter struct{}

func (writeConfirmationAdapter) Execute(_ context.Context, inv *dispatch.Invocation, _ *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	return &dispatch.Response{
		Body:       json.RawMessage(`{"result":"ok","op_id":"` + inv.OpID + `"}`),
		Format:     "json",
		StatusCode: 200,
	}, nil
}

func writeConfirmationCatalog() *catalog.Catalog {
	ops := make([]catalog.Op, 0, len(writeToolNames))
	for _, toolName := range writeToolNames {
		opID := convenienceOpRouting[toolName]
		variantID := opID + ".v1.test"
		ops = append(ops, catalog.Op{
			OpID:             opID,
			OpSchemaVersion:  1,
			Title:            opID,
			Summary:          "test write op for " + toolName,
			DefaultVariantID: variantID,
			Variants: []catalog.Variant{
				{
					VariantID:            variantID,
					VariantSchemaVersion: 1,
					Stability:            catalog.StabilityStable,
					InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
					BackendKind:          catalog.BackendKindDiscoveryREST,
					RiskClass:            catalog.RiskClassWrite,
					Binding: &catalog.Binding{
						BindingSchemaVersion: 1,
						AdapterKey:           "write.confirmation.test",
						OperationKey:         variantID + ".exec",
					},
				},
			},
		})
	}
	return minimalCatalog(ops...)
}

// buildWriteCallRequest constructs a minimal *sdkmcp.CallToolRequest for
// a write tool, omitting the confirmed field entirely (simulating a caller
// that has not yet confirmed the action).
func buildWriteCallRequest(toolName string, extraArgs map[string]any) *sdkmcp.CallToolRequest {
	args := map[string]any{}
	// Provide minimal required args so the request is not rejected for missing args.
	switch toolName {
	case "gmail_send":
		args["to"] = "test@example.com"
		args["subject"] = "Test"
		args["body"] = "Hello"
	case "gmail_create_draft":
		args["to"] = "test@example.com"
		args["subject"] = "Draft"
		args["body"] = "Body"
	case "calendar_create_event":
		args["calendarId"] = "primary"
		args["summary"] = "Meeting"
		args["start"] = "2026-06-01T10:00:00Z"
		args["end"] = "2026-06-01T11:00:00Z"
	case "calendar_update_event":
		args["calendarId"] = "primary"
		args["eventId"] = "evt123"
		args["summary"] = "Updated Meeting"
	}
	for k, v := range extraArgs {
		args[k] = v
	}
	raw, _ := json.Marshal(args)
	return &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name:      toolName,
			Arguments: raw,
		},
	}
}

// TestTierAWriteToolsRequireConfirmation calls each write tool's convenience
// handler without confirmed=true and expects a REQUIRES_CONFIRMATION envelope.
//
// Spec anchor: spec.md §4.1 confirmation_passthrough=yes rows; §6 confirmation
// gate; §1421 stable error code REQUIRES_CONFIRMATION.
//
// Expected failure: makeConvenienceHandler currently calls applyRiskFlagsFromCatalog
// (sets AllowWrite=true) then dispatches directly without checking confirmed. The
// dispatch kernel's confirmation gate only fires for destructive ops (RiskClassDestructive),
// not write ops. So calling gmail_send without confirmed=true currently succeeds
// (or returns OP_NOT_FOUND from catalog), never REQUIRES_CONFIRMATION.
func TestTierAWriteToolsRequireConfirmation(t *testing.T) {
	snap := writeConfirmationCatalog()
	disp := dispatch.NewDispatcher(snap, map[string]dispatch.Adapter{
		"write.confirmation.test": writeConfirmationAdapter{},
	})
	srv := NewServerWithCatalog(disp, snap)

	for _, name := range writeToolNames {
		name := name
		t.Run(name, func(t *testing.T) {
			handler := srv.makeConvenienceHandler(name)
			req := buildWriteCallRequest(name, nil)

			result, err := handler(context.Background(), req)
			if err != nil {
				t.Fatalf("%s: handler returned unexpected error: %v", name, err)
			}
			if result == nil {
				t.Fatalf("%s: handler returned nil result", name)
			}

			// Extract text content from result.
			var body string
			for _, c := range result.Content {
				if tc, ok := c.(*sdkmcp.TextContent); ok {
					body = tc.Text
					break
				}
			}

			if !strings.Contains(body, "REQUIRES_CONFIRMATION") {
				t.Errorf("%s: calling without confirmed=true should return REQUIRES_CONFIRMATION envelope; "+
					"got: %s\n"+
					"(spec §4.1 confirmation_passthrough=yes; §1421 stable error code)", name, body)
			}
			var env map[string]any
			if err := json.Unmarshal([]byte(body), &env); err != nil {
				t.Fatalf("%s: confirmation body is not JSON: %v\n%s", name, err, body)
			}
			token, _ := env["confirmation_token"].(string)
			if token == "" {
				t.Fatalf("%s: REQUIRES_CONFIRMATION missing confirmation_token: %s", name, body)
			}

			// isError MUST be true when returning a confirmation envelope.
			if !result.IsError {
				t.Errorf("%s: result.IsError should be true for REQUIRES_CONFIRMATION response "+
					"(confirmation envelope is an error result per spec §6)", name)
			}

			retry := buildWriteCallRequest(name, map[string]any{
				"confirmed":          true,
				"confirmation_token": token,
			})
			result, err = handler(context.Background(), retry)
			if err != nil {
				t.Fatalf("%s: confirmed retry returned unexpected error: %v", name, err)
			}
			if result.IsError {
				t.Fatalf("%s: confirmed retry returned error: %#v", name, result.Content)
			}
		})
	}
}
