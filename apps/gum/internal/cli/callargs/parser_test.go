package callargs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIArgGrammar exercises every clause of spec §12.0 in one fixture-based
// table so the contract is reviewable as a single matrix. Sub-tests are named
// by clause so a failure surfaces the exact grammar rule that broke.
func TestCLIArgGrammar(t *testing.T) {
	t.Run("key=value scalar string", func(t *testing.T) {
		r, err := ParseArgs([]string{"userId=me", "q=from:alice"}, Options{})
		if err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		if r.Args["userId"] != "me" || r.Args["q"] != "from:alice" {
			t.Errorf("args = %#v", r.Args)
		}
	})

	t.Run("key:=json typed values", func(t *testing.T) {
		r, err := ParseArgs([]string{
			`maxResults:=20`,
			`labels:=["INBOX","UNREAD"]`,
			`body:={"subject":"Hi"}`,
			`flag:=true`,
		}, Options{})
		if err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		// json.Number for numbers (UseNumber).
		mr, ok := r.Args["maxResults"].(json.Number)
		if !ok || mr.String() != "20" {
			t.Errorf("maxResults = %#v; want json.Number(20)", r.Args["maxResults"])
		}
		labels, _ := r.Args["labels"].([]any)
		if len(labels) != 2 || labels[0] != "INBOX" || labels[1] != "UNREAD" {
			t.Errorf("labels = %#v", labels)
		}
		body, _ := r.Args["body"].(map[string]any)
		if body["subject"] != "Hi" {
			t.Errorf("body = %#v", body)
		}
		if v, _ := r.Args["flag"].(bool); !v {
			t.Errorf("flag = %#v; want true", r.Args["flag"])
		}
	})

	t.Run("@file merges before inline; inline overrides", func(t *testing.T) {
		dir := t.TempDir()
		fp := filepath.Join(dir, "body.json")
		_ = os.WriteFile(fp, []byte(`{"userId":"me","q":"original","extra":"keep"}`), 0o644)
		r, err := ParseArgs([]string{"@" + fp, `q=overridden`, "newKey=newVal"}, Options{})
		if err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		if r.Args["userId"] != "me" || r.Args["extra"] != "keep" {
			t.Errorf("file values not preserved: %#v", r.Args)
		}
		if r.Args["q"] != "overridden" {
			t.Errorf("inline did not override file: q=%v", r.Args["q"])
		}
		if r.Args["newKey"] != "newVal" {
			t.Errorf("inline-only key missing: %#v", r.Args)
		}
	})

	t.Run("@- reads stdin", func(t *testing.T) {
		r, err := ParseArgs([]string{"@-"}, Options{Stdin: strings.NewReader(`{"hello":"stdin"}`)})
		if err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		if r.Args["hello"] != "stdin" {
			t.Errorf("@- did not load stdin: %#v", r.Args)
		}
	})

	t.Run("dotted key creates nested object", func(t *testing.T) {
		r, err := ParseArgs([]string{"message.subject=Hi", "message.body.text=hello"}, Options{})
		if err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		msg, _ := r.Args["message"].(map[string]any)
		if msg["subject"] != "Hi" {
			t.Errorf("subject not nested: %#v", r.Args)
		}
		body, _ := msg["body"].(map[string]any)
		if body["text"] != "hello" {
			t.Errorf("doubly-nested key missing: %#v", r.Args)
		}
	})

	t.Run("escaped dot keeps literal in key", func(t *testing.T) {
		r, err := ParseArgs([]string{`labels\.literal=KEEP`}, Options{})
		if err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		if r.Args["labels.literal"] != "KEEP" {
			t.Errorf("escaped dot did not preserve key: %#v", r.Args)
		}
	})

	t.Run("escaped backslash keeps literal", func(t *testing.T) {
		r, err := ParseArgs([]string{`weird\\key=val`}, Options{})
		if err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		if r.Args[`weird\key`] != "val" {
			t.Errorf("escaped backslash key wrong: %#v", r.Args)
		}
	})

	t.Run("duplicate scalar without array schema → CLI_ARG_DUPLICATE", func(t *testing.T) {
		_, err := ParseArgs([]string{"q=first", "q=second"}, Options{})
		if !IsDuplicate(err) {
			t.Fatalf("expected CLI_ARG_DUPLICATE, got %v", err)
		}
	})

	t.Run("duplicate when ArrayFields lists key → append", func(t *testing.T) {
		r, err := ParseArgs([]string{"labelIds=INBOX", "labelIds=UNREAD"}, Options{
			ArrayFields: []string{"labelIds"},
		})
		if err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		arr, _ := r.Args["labelIds"].([]any)
		if len(arr) != 2 || arr[0] != "INBOX" || arr[1] != "UNREAD" {
			t.Errorf("labelIds = %#v; want appended array", arr)
		}
	})

	t.Run("positional that isn't key=value/@file → CLI_ARG_INVALID", func(t *testing.T) {
		_, err := ParseArgs([]string{"no-equals-sign"}, Options{})
		if !IsInvalid(err) {
			t.Fatalf("expected CLI_ARG_INVALID, got %v", err)
		}
	})

	t.Run("--fields vs fields= host-control separation", func(t *testing.T) {
		// Spec §12.0 line 2422: the parser must NOT consume --fields; it is a
		// host-control flag handled by cobra. fields= however is an operation
		// arg literally named "fields".
		r, err := ParseArgs([]string{`fields=messages(id)`}, Options{})
		if err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		if r.Args["fields"] != "messages(id)" {
			t.Errorf("operation arg 'fields' not preserved: %#v", r.Args)
		}
		// And the parser rejects a leading -- arg (cobra would normally swallow
		// these, but a defensive parser must not silently treat them as keys).
		if _, err := ParseArgs([]string{"--fields=messages(id)"}, Options{}); !IsInvalid(err) {
			t.Errorf("expected CLI_ARG_INVALID for --fields=, got %v", err)
		}
	})

	t.Run("dangling backslash in key → CLI_ARG_INVALID", func(t *testing.T) {
		if _, err := ParseArgs([]string{`bad\=val`}, Options{}); !IsInvalid(err) {
			t.Errorf("expected CLI_ARG_INVALID, got %v", err)
		}
	})

	t.Run("trailing dot in key → CLI_ARG_INVALID", func(t *testing.T) {
		if _, err := ParseArgs([]string{`bad.=val`}, Options{}); !IsInvalid(err) {
			t.Errorf("expected CLI_ARG_INVALID, got %v", err)
		}
	})

	t.Run("typed JSON with trailing garbage → CLI_ARG_INVALID", func(t *testing.T) {
		if _, err := ParseArgs([]string{`x:=10garbage`}, Options{}); !IsInvalid(err) {
			t.Errorf("expected CLI_ARG_INVALID for trailing junk; got %v", err)
		}
	})

	t.Run("dotted key collides with non-object → CLI_ARG_INVALID", func(t *testing.T) {
		_, err := ParseArgs([]string{`message=string`, `message.body=Hi`}, Options{})
		if !IsInvalid(err) {
			t.Errorf("expected CLI_ARG_INVALID for object/string collision, got %v", err)
		}
	})
}

// TestErrorMessage locks the four formatting branches of (*Error).Error().
// Each variant must render the spec'd CLI_ARG_* code prefix and surface
// whichever of (Key, Arg, Reason) are populated.
func TestErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		e    *Error
		want string
	}{
		{
			name: "duplicate_renders_key",
			e:    &Error{Code: "CLI_ARG_DUPLICATE", Key: "q"},
			want: "CLI_ARG_DUPLICATE: q",
		},
		{
			name: "invalid_with_arg_and_reason",
			e:    &Error{Code: "CLI_ARG_INVALID", Arg: "foo=", Reason: "missing value"},
			want: `CLI_ARG_INVALID: "foo=": missing value`,
		},
		{
			name: "invalid_with_arg_only",
			e:    &Error{Code: "CLI_ARG_INVALID", Arg: "foo="},
			want: `CLI_ARG_INVALID: "foo="`,
		},
		{
			name: "invalid_with_reason_only",
			e:    &Error{Code: "CLI_ARG_INVALID", Reason: "schema collision"},
			want: "CLI_ARG_INVALID: schema collision",
		},
		{
			name: "invalid_bare",
			e:    &Error{Code: "CLI_ARG_INVALID"},
			want: "CLI_ARG_INVALID",
		},
		{
			name: "unknown_code_falls_through",
			e:    &Error{Code: "CLI_ARG_WEIRD"},
			want: "CLI_ARG_ERROR: CLI_ARG_WEIRD",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.e.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}
