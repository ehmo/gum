package mcp

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// gum.read/write/destructive advertise a closed format enum of
// toon|csv|json|markdown (spec §311). csv and markdown are §13
// SingleObjectResult shapes whose data is the encoded text, not the shaped
// tree: a client that asked for csv and got a JSON object under
// `"format":"toon"` had no way to tell the difference from a server bug.

func shapeOnce(t *testing.T, resp *dispatch.ShapedResponse) map[string]any {
	t.Helper()
	srv := NewServer(shapedDispatcher{resp: resp})
	res, err := srv.dispatchAndShape(context.Background(), &dispatch.Invocation{
		OpID: "gmail.users.messages.list",
	})
	if err != nil {
		t.Fatalf("dispatchAndShape: %v", err)
	}
	env, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structuredContent is %T, want the §13 envelope map", res.StructuredContent)
	}
	return env
}

func csvMarkdownMeta() *dispatch.ExpressionMeta {
	variant := "gmail.users.messages.list.v1"
	return &dispatch.ExpressionMeta{
		Profile:     "gmail.messages.list.v1",
		OpID:        "gmail.users.messages.list",
		VariantID:   &variant,
		ResultCount: 1,
	}
}

func TestCSVResultCarriesCSVText(t *testing.T) {
	env := shapeOnce(t, &dispatch.ShapedResponse{
		Body:              []byte("id,subject\nm1,hello\n"),
		Format:            "csv",
		StructuredContent: map[string]any{"messages": []any{map[string]any{"id": "m1", "subject": "hello"}}},
		Expression:        csvMarkdownMeta(),
	})
	if got := env["format"]; got != "csv" {
		t.Errorf("format = %v, want csv", got)
	}
	data, ok := env["data"].(string)
	if !ok {
		t.Fatalf("data is %T, want the CSV text as a string", env["data"])
	}
	if data != "id,subject\nm1,hello\n" {
		t.Errorf("data = %q, want the CSV body verbatim", data)
	}
	if _, present := env["toon"]; present {
		t.Error("a csv result must not carry a toon key")
	}
}

func TestMarkdownResultCarriesMarkdownText(t *testing.T) {
	body := "| id | subject |\n| --- | --- |\n| m1 | hello |\n"
	env := shapeOnce(t, &dispatch.ShapedResponse{
		Body:              []byte(body),
		Format:            "markdown",
		StructuredContent: map[string]any{"messages": []any{map[string]any{"id": "m1", "subject": "hello"}}},
		Expression:        csvMarkdownMeta(),
	})
	if got := env["format"]; got != "markdown" {
		t.Errorf("format = %v, want markdown", got)
	}
	data, ok := env["data"].(string)
	if !ok {
		t.Fatalf("data is %T, want the markdown text as a string (spec §2709)", env["data"])
	}
	if data != body {
		t.Errorf("data = %q, want the markdown body verbatim", data)
	}
}
