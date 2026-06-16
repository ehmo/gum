package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestInitTwiceReportsNoOp pins the `plan.NoOp → "No changes — ..."`
// arm of newInitCmd: running `gum init --yes` a SECOND time against an
// already-patched settings.json MUST print the "No changes" line rather
// than re-applying the patch (idempotency). This protects users from
// duplicate mcpServers.gum entries on repeated init invocations.
func TestInitTwiceReportsNoOp(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// First run: patches settings.json.
	root1 := newRootCmd()
	var out1 bytes.Buffer
	root1.SetOut(&out1)
	root1.SetErr(&out1)
	root1.SetArgs([]string{"init", "--yes"})
	if err := root1.Execute(); err != nil {
		t.Fatalf("first init --yes: %v\noutput:\n%s", err, out1.String())
	}

	// Second run: should see entry already present → NoOp arm.
	root2 := newRootCmd()
	var out2 bytes.Buffer
	root2.SetOut(&out2)
	root2.SetErr(&out2)
	root2.SetArgs([]string{"init", "--yes"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("second init --yes: %v\noutput:\n%s", err, out2.String())
	}

	if !strings.Contains(out2.String(), "No changes") {
		t.Errorf("second init output missing 'No changes' line; got:\n%s", out2.String())
	}
}
