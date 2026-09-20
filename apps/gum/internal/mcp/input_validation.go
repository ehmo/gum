package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// input_validation.go enforces the inputSchema every Tier A tool advertises.
//
// The MCP SDK validates tool input only inside its generic AddTool[In, Out]
// helper. gum registers raw json.RawMessage schemas with untyped handlers, so
// the SDK's validation never ran: a closed enum, a required field, a declared
// type and additionalProperties:false were all advisory. A bad value reached
// the handler as a silent zero value, and one of them (a negative k) panicked
// the stdio server on a slice bound. Validating here puts the declared
// contract back in force at the seam, before any handler sees the arguments.

// resolveInputSchema compiles a registered inputSchema so arguments can be
// validated against it.
func resolveInputSchema(raw json.RawMessage) (*jsonschema.Resolved, error) {
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	resolved, err := s.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	return resolved, nil
}

// validateToolArgs checks one call's raw arguments against a resolved schema.
// Absent, null, and empty arguments all validate as the empty object, so a
// schema with no required properties still accepts a bare call.
func validateToolArgs(resolved *jsonschema.Resolved, raw json.RawMessage) error {
	var args any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return fmt.Errorf("arguments are not valid JSON: %w", err)
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	return resolved.Validate(args)
}

// validatedHandler wraps h with the argument check declared by schema.
// toolName names the tool in the rejection so the caller knows which call
// failed when several are in flight.
func validatedHandler(toolName string, schema json.RawMessage, h sdkmcp.ToolHandler) sdkmcp.ToolHandler {
	resolved, err := resolveInputSchema(schema)
	if err != nil {
		// Fail closed, and only for this tool. Running the handler unvalidated
		// is the fault being fixed, and a panic here would end the stdio
		// session for every other tool too (spec §3.1).
		msg := fmt.Sprintf("%s: unusable inputSchema: %v", toolName, err)
		return func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return jsonErrorResult(map[string]any{
				"error_code": string(dispatch.ErrCodeServiceDown),
				"message":    msg,
				"retryable":  false,
			}), nil
		}
	}

	return func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var raw json.RawMessage
		if req != nil && req.Params != nil {
			raw = req.Params.Arguments
		}

		if verr := validateToolArgs(resolved, raw); verr != nil {
			return jsonErrorResult(map[string]any{
				"error_code": string(dispatch.ErrCodeInvalidArgs),
				"message":    fmt.Sprintf("%s: %v", toolName, verr),
				"retryable":  false,
			}), nil
		}

		return h(ctx, req)
	}
}
