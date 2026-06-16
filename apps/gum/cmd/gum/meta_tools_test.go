package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embed"
)

// TestParseArgsJSON covers the --args=JSON unmarshaller. Empty → empty map;
// valid object → typed map; anything else → wrapped error so the CLI surfaces
// the cause without panic.
func TestParseArgsJSON(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantLen int
		wantErr bool
		errSub  string
	}{
		{name: "empty", in: "", wantLen: 0},
		{name: "object", in: `{"q":"hello","n":1}`, wantLen: 2},
		{name: "array_rejected", in: `[1,2]`, wantErr: true, errSub: "--args"},
		{name: "garbage", in: `not json`, wantErr: true, errSub: "--args"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseArgsJSON(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseArgsJSON(%q) err = nil, want error", tc.in)
				}
				if tc.errSub != "" && !strings.Contains(err.Error(), tc.errSub) {
					t.Errorf("err = %v, want substring %q", err, tc.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgsJSON(%q) err = %v", tc.in, err)
			}
			if len(got) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(got), tc.wantLen)
			}
		})
	}
}

// TestRenderSearchTable verifies the human-readable BM25 result rendering:
// header columns are present, summaries longer than 80 chars are truncated
// with an ellipsis, and the empty-results message is rendered when results
// are empty.
func TestRenderSearchTable(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var buf bytes.Buffer
		renderSearchTable(&buf, nil)
		if !strings.Contains(buf.String(), "no results") {
			t.Errorf("empty render = %q, want 'no results' line", buf.String())
		}
	})
	t.Run("rows", func(t *testing.T) {
		var buf bytes.Buffer
		long := strings.Repeat("x", 200)
		renderSearchTable(&buf, []embed.SearchResult{
			{OpID: "a.short", RiskClass: "read", AuthStrategy: "byo_oauth", Summary: "ok"},
			{OpID: "b.much.longer.op.id", RiskClass: "destructive", AuthStrategy: "api_key", Summary: long},
		})
		out := buf.String()
		for _, want := range []string{"OP_ID", "RISK", "AUTH", "SUMMARY", "a.short", "b.much.longer.op.id", "..."} {
			if !strings.Contains(out, want) {
				t.Errorf("rendered output missing %q\nfull:\n%s", want, out)
			}
		}
		// Summary truncated to 80 chars (77 + "..."). Verify no untruncated
		// line of 200 chars leaked out.
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, strings.Repeat("x", 100)) {
				t.Errorf("found untruncated summary in line: %q", line)
			}
		}
	})
}

// TestReadScriptArg covers the @file vs inline-source disambiguation. Inline
// strings are passed through verbatim; @path reads the file; @missing returns
// a wrapped error.
func TestReadScriptArg(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "hello.risor")
	if err := os.WriteFile(scriptPath, []byte("print('hi')"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "inline", in: "print('inline')", want: "print('inline')"},
		{name: "empty_inline", in: "", want: ""},
		{name: "file", in: "@" + scriptPath, want: "print('hi')"},
		{name: "missing_file", in: "@" + filepath.Join(dir, "does-not-exist.risor"), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readScriptArg(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("readScriptArg(%q) err = nil, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("readScriptArg(%q) err = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("readScriptArg(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSynthesizeExampleArgs locks the describe example_args synthesis path:
// required params + URL-template placeholders are folded into one map; values
// follow the conservative name heuristic (page_size→10, user_id→"me", else
// <sigil>). We construct a synthetic op so the test is decoupled from catalog
// drift.
func TestSynthesizeExampleArgs(t *testing.T) {
	op := &catalog.Op{
		OpID:             "test.messages.list",
		DefaultVariantID: "v1",
		ParamsRequired:   [][]string{{"q", "page_size"}},
		Variants: []catalog.Variant{
			{
				VariantID: "v1",
				Binding: &catalog.Binding{
					HTTP: &catalog.HTTPBinding{
						Path: "/users/{userId}/messages",
					},
				},
			},
		},
	}
	got := synthesizeExampleArgs(op)
	if got["q"] != "<q>" {
		t.Errorf("q = %v, want <q>", got["q"])
	}
	if got["page_size"] != 10 {
		t.Errorf("page_size = %v, want 10", got["page_size"])
	}
	if got["userId"] != "me" {
		t.Errorf("userId = %v, want 'me' (from path template)", got["userId"])
	}
	// No params at all → empty map (not nil).
	empty := synthesizeExampleArgs(&catalog.Op{
		OpID: "empty", DefaultVariantID: "v1",
		Variants: []catalog.Variant{{VariantID: "v1"}},
	})
	if empty == nil {
		t.Errorf("expected non-nil empty map, got nil")
	}
	if len(empty) != 0 {
		t.Errorf("expected empty map, got %v", empty)
	}
}

// TestPathTemplateParams locks the URL-template placeholder extractor.
func TestPathTemplateParams(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{in: "/no/placeholders", want: nil},
		{in: "/users/{userId}/messages", want: []string{"userId"}},
		{in: "/a/{x}/b/{y}", want: []string{"x", "y"}},
		{in: "/dangling/{unterminated", want: nil}, // graceful with unclosed
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := pathTemplateParams(tc.in)
			if len(got) != len(tc.want) {
				t.Errorf("pathTemplateParams(%q) = %v, want %v", tc.in, got, tc.want)
				return
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("element %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestExampleValueFor covers the name-heuristic placeholder mapper.
func TestExampleValueFor(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{"page_size", 10},
		{"pageSize", 10},
		{"item_count", 10},
		{"user_id", "me"},
		{"userId", "me"},
		{"verbose_enabled", false},
		{"include_archived", false},
		{"q", "<q>"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := exampleValueFor(tc.in)
			if got != tc.want {
				t.Errorf("exampleValueFor(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestDispatchToWriter exercises the wrapper that turns a dispatcher's
// shaped result into a CLI-friendly stream (always newline-terminated). We
// use OP_NOT_FOUND to drive the error branch without needing live creds.
func TestDispatchToWriter(t *testing.T) {
	t.Run("op_not_found_returns_error", func(t *testing.T) {
		var buf bytes.Buffer
		inv := &dispatch.Invocation{
			OpID:   "does.not.exist",
			Caller: dispatch.CallerCLI,
		}
		err := dispatchToWriter(context.Background(), "default", &buf, &buf, inv)
		if err == nil {
			t.Errorf("expected error for unknown op")
		}
	})
}

// TestJoinArgs verifies the space-joining helper used by `gum search`.
func TestJoinArgs(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b", "c"}, "a b c"},
	}
	for _, tc := range cases {
		got := joinArgs(tc.in)
		if got != tc.want {
			t.Errorf("joinArgs(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
