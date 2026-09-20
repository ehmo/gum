package mcp

import (
	"github.com/ehmo/gum/internal/dispatch"
)

// tierAResult wraps a shaped dispatch result in the spec §13 structured result
// shape: ToonResult, SingleObjectResult, or RawJsonResult.
//
// The §13 selection rule (spec line 2701) keys on the resolved format, so the
// wrapper reads shaped.Format rather than guessing from the payload. Before
// this existed the MCP layer put the bare payload in structuredContent and no
// production path ever emitted `_expression`, so a client had the rows but no
// way to learn how many were missing.
func tierAResult(shaped *dispatch.ShapedResponse) any {
	if shaped == nil {
		return nil
	}
	meta := shaped.Expression
	if meta == nil {
		// No envelope means no §13 shape to build; hand back the payload
		// rather than emit an object that fails its own required list.
		return shaped.StructuredContent
	}

	env := map[string]any{"_expression": meta}

	switch shaped.Format {
	case "raw":
		// RawJsonResult: format is the constant "json" even for a raw pass,
		// and data is the unprocessed value (§2705). An executor that returned
		// opaque bytes has no JSON tree, so its printed output is the data.
		env["format"] = "json"
		if shaped.StructuredContent != nil {
			env["data"] = shaped.StructuredContent
		} else {
			env["data"] = string(shaped.Body)
		}

	case "json":
		// SingleObjectResult: the data field is open because a json result is
		// an object, array, or scalar.
		env["format"] = "json"
		env["data"] = shaped.StructuredContent

	case "csv", "markdown":
		// SingleObjectResult too, but the data is the encoded text the stage-8
		// encoder produced, not the shaped tree. Sending the tree instead
		// would answer a csv or markdown request with JSON under the caller's
		// own format label.
		env["format"] = shaped.Format
		env["data"] = string(shaped.Body)

	default:
		// ToonResult. The TOON text carries the count and fields headers, so
		// only op and variant repeat as top-level convenience keys. §13 closes
		// this object, so nothing else may be added here.
		//
		// This is the default rather than a `case "toon"` because profile.Apply
		// falls back to TOON for any format it does not implement. It renames
		// the format when it does so, so the constant here agrees with the
		// body in every case, including a format added to the enum before its
		// encoder ships.
		env["format"] = "toon"
		env["toon"] = string(shaped.Body)
		if meta.OpID != "" {
			env["op"] = meta.OpID
		}
		if meta.VariantID != nil {
			env["variant"] = *meta.VariantID
		}
	}

	return env
}
