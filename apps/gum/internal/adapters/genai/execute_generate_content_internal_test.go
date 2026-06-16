package genai

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestExecuteGenerateContentRequiresArgs pins the two pre-flight
// guards that fire before any SDK call: missing model and missing
// prompt. Without these, the SDK would send a request that the
// upstream rejects with a 400 — burning quota on a known-bad payload.
func TestExecuteGenerateContentRequiresArgs(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing_both", map[string]any{}, "`model` is required"},
		{"missing_model", map[string]any{"prompt": "hi"}, "`model` is required"},
		{"missing_prompt", map[string]any{"model": "gemini-2.0-flash"}, "`prompt` is required"},
		{"empty_string_model", map[string]any{"model": "", "prompt": "hi"}, "`model` is required"},
		{"empty_string_prompt", map[string]any{"model": "g", "prompt": ""}, "`prompt` is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := executeGenerateContent(context.Background(), nil,
				&dispatch.Invocation{Args: tc.args})
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err=%q; want substring %q", err, tc.want)
			}
		})
	}
}

