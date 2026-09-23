package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ehmo/gum/internal/mcp"
)

// TestSearchJSONEnvelopeMatchesSpec pins the `gum search --format=json` root to
// spec §12: `{"query", "results", "on_empty_message"?}`. The command emitted
// only `results`, so a consumer reading a saved file could not tell which query
// produced the hits, and an empty set arrived with no explanation.
func TestSearchJSONEnvelopeMatchesSpec(t *testing.T) {
	run := func(t *testing.T, args ...string) map[string]any {
		t.Helper()
		root := newRootCmd()
		var out, errOut bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute %v: %v (stderr: %s)", args, err, errOut.String())
		}
		var env map[string]any
		if err := json.Unmarshal(out.Bytes(), &env); err != nil {
			t.Fatalf("search output is not JSON: %v\noutput: %s", err, out.String())
		}
		return env
	}

	t.Run("hit", func(t *testing.T) {
		env := run(t, "search", "gmail", "--format=json")
		if got := env["query"]; got != "gmail" {
			t.Errorf("query = %v; want %q", got, "gmail")
		}
		results, ok := env["results"].([]any)
		if !ok {
			t.Fatalf("results is not an array: %T", env["results"])
		}
		if len(results) == 0 {
			t.Fatal("search gmail returned no results; embedded catalog may be empty")
		}
		if _, present := env["on_empty_message"]; present {
			t.Error("on_empty_message must be absent when results is non-empty")
		}
	})

	t.Run("miss", func(t *testing.T) {
		const query = "zzzzzzzznosuchoperationanywhere"
		env := run(t, "search", query, "--format=json")
		if got := env["query"]; got != query {
			t.Errorf("query = %v; want %q", got, query)
		}
		results, ok := env["results"].([]any)
		if !ok {
			t.Fatalf("results is not an array: %T (must be [] not null)", env["results"])
		}
		if len(results) != 0 {
			t.Fatalf("results = %v; want empty", results)
		}
		if got := env["on_empty_message"]; got != mcp.SearchNoResultsMessage {
			t.Errorf("on_empty_message = %v; want %q", got, mcp.SearchNoResultsMessage)
		}
	})
}
