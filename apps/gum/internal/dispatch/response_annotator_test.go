package dispatch

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// annotatingAdapter implements both Adapter and ResponseAnnotator.
type annotatingAdapter struct{ calls int }

func (a *annotatingAdapter) Execute(context.Context, *Invocation, *ResolvedVariant, *Credentials) (*Response, error) {
	return nil, nil
}

func (a *annotatingAdapter) AnnotateResponse(_ *Invocation, _ *ResolvedVariant, body []byte) []byte {
	a.calls++
	return []byte(`{"annotated":true}`)
}

func annotatorDispatcher(a Adapter) (*dispatcher, *ResolvedVariant) {
	d := &dispatcher{adapters: map[string]Adapter{"test.annotating": a}}
	rv := &ResolvedVariant{Variant: &catalog.Variant{}, AdapterKey: "test.annotating"}
	return d, rv
}

// gum-o84f: the annotator runs inside step 8, so a shaped format sees the
// annotation.
func TestAnnotateResponseRunsOnShapedFormats(t *testing.T) {
	for _, format := range []string{"toon", "json", ""} {
		t.Run("format="+format, func(t *testing.T) {
			adapter := &annotatingAdapter{}
			d, rv := annotatorDispatcher(adapter)

			out, err := d.shapeResponse(t.Context(), &Invocation{OpID: "x", Format: format}, rv, &Response{Body: []byte(`{"annotated":false}`)})
			if err != nil {
				t.Fatalf("shapeResponse: %v", err)
			}
			if adapter.calls != 1 {
				t.Fatalf("annotator calls = %d; want 1", adapter.calls)
			}
			if !strings.Contains(string(out.Body), "true") {
				t.Errorf("body = %q; want the annotated body", out.Body)
			}
		})
	}
}

// TestAnnotateResponseSkippedForRaw is acceptance criterion (c): `--format raw`
// returns the upstream bytes, so it must not reach the annotator at all.
func TestAnnotateResponseSkippedForRaw(t *testing.T) {
	adapter := &annotatingAdapter{}
	d, rv := annotatorDispatcher(adapter)
	body := []byte(`{"annotated":false}`)

	out, err := d.shapeResponse(t.Context(), &Invocation{OpID: "x", Format: "raw"}, rv, &Response{Body: body})
	if err != nil {
		t.Fatalf("shapeResponse: %v", err)
	}
	if adapter.calls != 0 {
		t.Errorf("annotator ran for --format raw (%d calls); raw must be the upstream bytes", adapter.calls)
	}
	if string(out.Body) != string(body) {
		t.Errorf("body = %q; want the upstream bytes %q", out.Body, body)
	}
}

// TestAnnotateResponseStructuredContent: the MCP structuredContent block is
// built from the annotated body, not the upstream one, or an MCP caller reads a
// different result from the text block.
func TestAnnotateResponseStructuredContent(t *testing.T) {
	d, rv := annotatorDispatcher(&annotatingAdapter{})

	out, err := d.shapeResponse(t.Context(), &Invocation{OpID: "x", Format: "json"}, rv, &Response{Body: []byte(`{"annotated":false}`)})
	if err != nil {
		t.Fatalf("shapeResponse: %v", err)
	}
	m, ok := out.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent = %#v; want a map", out.StructuredContent)
	}
	if m["annotated"] != true {
		t.Errorf("StructuredContent = %#v; want the annotated body", m)
	}
}

// plainAdapter implements Adapter only.
type plainAdapter struct{}

func (plainAdapter) Execute(context.Context, *Invocation, *ResolvedVariant, *Credentials) (*Response, error) {
	return nil, nil
}

// TestAnnotateResponseOptional: an adapter that does not implement the
// interface, and an unregistered adapter key, both leave the body alone.
func TestAnnotateResponseOptional(t *testing.T) {
	body := []byte(`{"a":1}`)
	cases := map[string]*dispatcher{
		"no annotator": {adapters: map[string]Adapter{"test.plain": plainAdapter{}}},
		"unregistered": {adapters: map[string]Adapter{}},
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			rv := &ResolvedVariant{Variant: &catalog.Variant{}, AdapterKey: "test.plain"}
			got := d.annotateResponse(&Invocation{OpID: "x"}, rv, body)
			if string(got) != string(body) {
				t.Errorf("body = %q; want it unchanged", got)
			}
		})
	}
}

// nilAdapter returns nil from AnnotateResponse.
type nilAdapter struct{ plainAdapter }

func (nilAdapter) AnnotateResponse(*Invocation, *ResolvedVariant, []byte) []byte { return nil }

func TestAnnotateResponseNilKeepsBody(t *testing.T) {
	d, rv := annotatorDispatcher(nilAdapter{})
	body := []byte(`{"a":1}`)

	if got := d.annotateResponse(&Invocation{OpID: "x"}, rv, body); string(got) != string(body) {
		t.Errorf("body = %q; want it unchanged when the annotator returns nil", got)
	}
}
