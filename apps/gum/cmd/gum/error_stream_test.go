package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestDispatchErrorGoesToStderrNotStdout pins the pipeline-safety contract
// (review gum-nxaw): a dispatch error must leave stdout empty so that
// `gum read ... | jq` never parses an error envelope as data. The structured
// envelope is written to stderr and remains valid JSON for tooling that wants
// to inspect it.
func TestDispatchErrorGoesToStderrNotStdout(t *testing.T) {
	for _, cmdArgs := range [][]string{
		{"call", "does.not.exist", "--risk=read"},
		{"read", "does.not.exist"},
	} {
		t.Run(cmdArgs[0], func(t *testing.T) {
			root := newRootCmd()
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs(cmdArgs)

			err := root.ExecuteContext(context.Background())
			if err == nil {
				t.Fatalf("expected error for unknown op; stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout must be empty on dispatch error; got %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), "OP_NOT_FOUND") {
				t.Errorf("stderr missing error envelope; got %q", stderr.String())
			}
			var env map[string]any
			if jerr := json.Unmarshal(stderr.Bytes(), &env); jerr != nil {
				t.Errorf("stderr envelope is not valid JSON: %v; got %q", jerr, stderr.String())
			}
		})
	}
}
