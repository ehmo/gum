package main

import (
	"io"

	"github.com/ehmo/gum/internal/output/render"
)

// render.go holds the CLI's --output policy. The rendering itself lives in
// internal/output/render, shared with the MCP seam so both presentation layers
// produce identical bytes for a given format. What stays here is the part that
// is genuinely CLI-only: which format names the flag accepts, and which of them
// need the structured result instead of the dispatch encoder's wire bytes.

// cliFormatNeedsStructured reports whether format is rendered from the
// structured result (true) versus passed straight through to the dispatch
// encoder (false, for json|toon|raw).
func cliFormatNeedsStructured(format string) bool {
	if _, ok := render.ValuePath(format); ok {
		return true
	}
	switch format {
	case "table", "csv", "markdown":
		return true
	}
	return false
}

// validCLIFormat reports whether format is an accepted --output value.
func validCLIFormat(format string) bool {
	if _, ok := render.ValuePath(format); ok {
		return true
	}
	switch format {
	case "table", "json", "toon", "csv", "markdown", "raw":
		return true
	}
	return false
}

// renderStructured writes v (a parsed JSON result) to w in a human format.
func renderStructured(w io.Writer, format string, v any) error {
	return render.Structured(w, format, v)
}
