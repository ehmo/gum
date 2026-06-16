package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestCodeCmdResolvesAgainstEmbeddedCatalog is the shipped-surface regression
// for the gum.code P0 (gum-7ras): it dispatches `gum code` through the SAME
// newDefaultDispatcher() + embedded catalog the released binary uses, and
// asserts the script actually runs. Before the fix the embedded catalog had no
// gum.code op, so this returned OP_NOT_FOUND and the flagship feature was dead
// in every shipped build — yet the older branch-coverage test passed because it
// discarded the dispatch error. This test fails loudly if the op ever falls out
// of the embedded snapshot again.
func TestCodeCmdResolvesAgainstEmbeddedCatalog(t *testing.T) {
	const marker = "gum-code-embedded-ok"

	cmd := newCodeCmd()
	cmd.SetArgs([]string{`gum_print("` + marker + `")`})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("gum code against embedded catalog failed: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), marker) {
		t.Errorf("stdout = %q, want it to contain %q (sandbox did not run)", stdout.String(), marker)
	}
}

// TestGumCodeOpResolvesPastLookup pins the lower-level guarantee that the
// embedded catalog can resolve the gum.code op AND its none-auth strategy
// without an OP_NOT_FOUND / AUTH_STRATEGY_NOT_IMPLEMENTED error — the two
// halves of the P0. It dispatches directly so the assertion is about op/auth
// resolution rather than sandbox output.
func TestGumCodeOpResolvesPastLookup(t *testing.T) {
	disp := newDefaultDispatcher()
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{
			"language": "risor",
			"source":   `gum_print("ok")`,
		},
		Caller: dispatch.CallerCLI,
	}
	shaped, err := disp.Dispatch(context.Background(), inv)
	if err != nil {
		// A resolution failure surfaces as OP_NOT_FOUND or
		// AUTH_STRATEGY_NOT_IMPLEMENTED in the error string; either means the
		// shipped binary cannot run gum.code.
		msg := err.Error()
		if strings.Contains(msg, "OP_NOT_FOUND") || strings.Contains(msg, "AUTH_STRATEGY_NOT_IMPLEMENTED") {
			t.Fatalf("gum.code did not resolve in embedded catalog: %v", err)
		}
		t.Fatalf("unexpected dispatch error: %v", err)
	}
	if len(shaped.Body) == 0 {
		t.Error("empty response body; sandbox produced no output")
	}
}
