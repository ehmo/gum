package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestPromptConfirm covers the y/Y confirmation gate used by `gum init` for
// destructive host-config mutations. Any answer other than y/yes (case-
// insensitive, surrounding whitespace tolerated) is a refusal — EOF too.
func TestPromptConfirm(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"yes_short", "y\n", true},
		{"yes_upper", "Y\n", true},
		{"yes_full", "yes\n", true},
		{"yes_with_whitespace", " yes \n", true},
		{"no_short", "n\n", false},
		{"empty_line_is_no", "\n", false},
		{"eof_is_no", "", false},
		{"garbage_is_no", "definitely\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w bytes.Buffer
			r := strings.NewReader(tc.in)
			got := promptConfirm(r, &w, "proceed? ")
			if got != tc.want {
				t.Errorf("promptConfirm(%q) = %v, want %v", tc.in, got, tc.want)
			}
			if !strings.Contains(w.String(), "proceed?") {
				t.Errorf("prompt %q not written to w; got %q", "proceed?", w.String())
			}
		})
	}
}

// TestReaderOnly verifies the bufio adapter passes bytes through unchanged.
// This is the small shim that lets cobra's bare io.Reader feed bufio.NewReader.
func TestReaderOnly(t *testing.T) {
	src := strings.NewReader("hello")
	adapter := readerOnly{r: src}
	buf := make([]byte, 16)
	n, err := adapter.Read(buf)
	if err != nil {
		t.Fatalf("Read err = %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("Read = %q, want hello", string(buf[:n]))
	}
}
