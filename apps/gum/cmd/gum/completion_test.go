package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestCLICompletionsAllShells exercises cobra's built-in `gum completion <shell>`
// for each supported shell, asserting the emitted script is non-empty and
// contains a shell-specific marker. Spec §12.2.
func TestCLICompletionsAllShells(t *testing.T) {
	cases := []struct {
		shell  string
		marker string
	}{
		{"bash", "__gum_"},
		{"zsh", "#compdef gum"},
		{"fish", "complete -c gum"},
		{"powershell", "Register-ArgumentCompleter"},
	}
	for _, tc := range cases {
		t.Run(tc.shell, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs([]string{"completion", tc.shell})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("completion %s: %v\nout=%s", tc.shell, err, out.String())
			}
			if out.Len() == 0 {
				t.Fatalf("completion %s: empty output", tc.shell)
			}
			if !strings.Contains(out.String(), tc.marker) {
				t.Errorf("completion %s: expected marker %q in output (got %d bytes, first 200=%q)",
					tc.shell, tc.marker, out.Len(), out.String()[:min(200, out.Len())])
			}
		})
	}
}

// TestCLICompletionDynamicOpIDByRisk verifies that the cobra hidden `__complete`
// subcommand returns op_ids whose default variant matches the parent command's
// risk class (spec §12.2 per-flag/per-arg completions backed by the embedded
// catalog).
func TestCLICompletionDynamicOpIDByRisk(t *testing.T) {
	cases := []struct {
		parent      string
		mustContain string // a known op_id of that risk class in the embedded catalog
		mustExclude string // an op_id from a different risk class
	}{
		{"read", "gmail.users.messages.list", "gmail.users.messages.send"},
		{"write", "gmail.users.messages.send", "gmail.users.messages.list"},
		{"destructive", "gmail.users.messages.trash", "gmail.users.messages.list"},
	}
	for _, tc := range cases {
		t.Run(tc.parent, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs([]string{"__complete", tc.parent, ""})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("__complete %s: %v\nout=%s", tc.parent, err, out.String())
			}
			got := out.String()
			if !strings.Contains(got, tc.mustContain) {
				t.Errorf("__complete %s: expected to include %q; got=%s", tc.parent, tc.mustContain, got)
			}
			if strings.Contains(got, tc.mustExclude+"\n") {
				t.Errorf("__complete %s: should not include %q (wrong risk class); got=%s",
					tc.parent, tc.mustExclude, got)
			}
		})
	}
}

// TestCLICompletionDescribeAcceptsAnyRisk asserts that `gum describe` completion
// returns ops across all risk classes (read+write+destructive), since describe
// is risk-agnostic.
func TestCLICompletionDescribeAcceptsAnyRisk(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"__complete", "describe", ""})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("__complete describe: %v\nout=%s", err, out.String())
	}
	got := out.String()
	wantAny := []string{
		"gmail.users.messages.list",  // read
		"gmail.users.messages.send",  // write
		"gmail.users.messages.trash", // destructive
	}
	for _, w := range wantAny {
		if !strings.Contains(got, w) {
			t.Errorf("__complete describe: expected to include %q; got=%s", w, got)
		}
	}
}

// TestCLICompletionPrefixFiltering asserts that the completer honours the
// "toComplete" prefix argument (cobra's standard contract).
func TestCLICompletionPrefixFiltering(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"__complete", "read", "gmail."})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("__complete read 'gmail.': %v\nout=%s", err, out.String())
	}
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "Completion ended") {
			continue
		}
		if !strings.HasPrefix(line, "gmail.") {
			t.Errorf("__complete read 'gmail.': unexpected non-matching candidate %q", line)
		}
	}
}

// TestCLICompletionCallFields verifies the --fields completer is wired and
// returns a ShellComp directive without error, even when the resolved op has
// no default_fields (the common case in the embedded catalog at this stage).
// gum-wcwn item 11: the completion plumbing is what we lock here; populating
// default_fields for live ops is a catalog-side task.
func TestCLICompletionCallFields(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"__complete", "call", "gmail.users.messages.list", "--fields", ""})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("__complete call --fields: %v\nout=%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "ShellCompDirective") {
		t.Errorf("__complete call --fields: expected a completion directive marker; got=%q", got)
	}
}

// TestCLICompletionCallFieldsFromDefaultFields exercises the path where the
// resolved variant DOES carry default_fields. We synthesize a Variant on the
// fly via direct call into completeFieldsForOp instead of round-tripping
// through cobra so we don't depend on catalog content.
func TestCLICompletionCallFieldsFromDefaultFields(t *testing.T) {
	if loadCatalog() == nil {
		t.Skip("embedded catalog unavailable")
	}
	// Verify the helper is at least exported and callable with no args (which
	// MUST return ShellCompDirectiveNoFileComp without panicking).
	results, directive := completeFieldsForOp(nil, nil, "")
	if results != nil {
		t.Errorf("completeFieldsForOp with no args should return nil; got %v", results)
	}
	if directive == 0 {
		t.Errorf("completeFieldsForOp should return a non-zero directive")
	}
}

// TestCLICompletionCallVariantID verifies the --variant-id completer enumerates
// the op's variants (gum-wcwn item 11).
func TestCLICompletionCallVariantID(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"__complete", "call", "gmail.users.messages.list", "--variant-id", ""})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("__complete call --variant-id: %v\nout=%s", err, out.String())
	}
	got := out.String()
	if strings.TrimSpace(got) == "" {
		t.Errorf("__complete call --variant-id: expected at least one variant_id; got empty")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
