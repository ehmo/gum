package initpkg

import (
	"testing"
)

// TestRenderGUMmdParseErrorPropagates pins the template.Parse error arm:
// a corrupt embedded template body (e.g. unbalanced action delimiter)
// MUST surface the parser's error verbatim rather than panicking inside
// text/template. Restores the embed via t.Cleanup so later tests stay
// green.
func TestRenderGUMmdParseErrorPropagates(t *testing.T) {
	saved := GUMmdTmpl
	t.Cleanup(func() { GUMmdTmpl = saved })
	GUMmdTmpl = "{{ unbalanced"

	_, err := RenderGUMmd("v0.1.0")
	if err == nil {
		t.Fatal("want parse error from unbalanced delimiter; got nil")
	}
}

// TestRenderGUMmdExecuteErrorPropagates pins the t.Execute error arm:
// a template that parses but references an undefined sub-template MUST
// surface the execution error rather than emitting a partial buffer.
func TestRenderGUMmdExecuteErrorPropagates(t *testing.T) {
	saved := GUMmdTmpl
	t.Cleanup(func() { GUMmdTmpl = saved })
	// "{{template ...}}" references a named sub-template that was never
	// defined; Parse succeeds, Execute fails.
	GUMmdTmpl = `{{template "missing-subtemplate" .}}`

	_, err := RenderGUMmd("v0.1.0")
	if err == nil {
		t.Fatal("want execute error from undefined sub-template; got nil")
	}
}
