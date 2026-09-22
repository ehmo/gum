package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestCodeCmdDestructiveBudgetAndScopeReachInvocation covers the spec §2492 CLI
// usage line for `gum code`: --destructive-budget=N and repeated
// --destructive-scope op_id[:resource_key].
//
// Without them --allow-destructive was unusable. The Risor adapter's
// validateDestructiveBudget rejects allow_destructive=true with a budget
// outside 1..20, and the CLI had no flag to set one, so every
// `gum code --allow-destructive` run failed INVALID_ARGS before executing a
// line of script.
func TestCodeCmdDestructiveBudgetAndScopeReachInvocation(t *testing.T) {
	cap := &capturingDispatcher{}
	orig := newCodeToolDispatcher
	t.Cleanup(func() { newCodeToolDispatcher = orig })
	newCodeToolDispatcher = func(string) dispatch.Dispatcher { return cap }

	cmd := newCodeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"gum_print(1)",
		"--allow-destructive", "--confirmed", "--token", "ct-1",
		"--destructive-budget", "3",
		"--destructive-scope", "drive.files.delete:file-a",
		"--destructive-scope", "gmail.users.messages.delete",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if cap.inv == nil {
		t.Fatal("dispatcher never received an Invocation")
	}

	if got := cap.inv.Args["destructive_budget"]; got != 3 {
		t.Errorf("args[destructive_budget]=%v (%T); want 3 (int)", got, got)
	}

	// The adapter reads the scope through a []any type assertion, so a
	// []map[string]any would be silently dropped rather than rejected.
	scope, ok := cap.inv.Args["destructive_scope"].([]any)
	if !ok {
		t.Fatalf("args[destructive_scope] is %T; want []any", cap.inv.Args["destructive_scope"])
	}
	if len(scope) != 2 {
		t.Fatalf("scope has %d entries; want 2", len(scope))
	}
	first, ok := scope[0].(map[string]any)
	if !ok {
		t.Fatalf("scope[0] is %T; want map[string]any", scope[0])
	}
	if first["op_id"] != "drive.files.delete" {
		t.Errorf("scope[0].op_id=%v; want drive.files.delete", first["op_id"])
	}
	if first["resource_key"] != "file-a" {
		t.Errorf("scope[0].resource_key=%v; want file-a", first["resource_key"])
	}
	second := scope[1].(map[string]any)
	if second["op_id"] != "gmail.users.messages.delete" {
		t.Errorf("scope[1].op_id=%v; want gmail.users.messages.delete", second["op_id"])
	}
	if second["resource_key"] != "" {
		t.Errorf("scope[1].resource_key=%v; want the empty string", second["resource_key"])
	}
}

// TestCodeCmdDestructiveArgsSatisfyAdapterGate closes the repro: the args the
// CLI now builds satisfy the 1..20 range the Risor adapter's
// validateDestructiveBudget enforces (covered in
// internal/adapters/code_risor_test.go). Before the flags existed, the CLI sent
// no destructive_budget at all and the gate read it as 0.
func TestCodeCmdDestructiveArgsSatisfyAdapterGate(t *testing.T) {
	cap := &capturingDispatcher{}
	orig := newCodeToolDispatcher
	t.Cleanup(func() { newCodeToolDispatcher = orig })
	newCodeToolDispatcher = func(string) dispatch.Dispatcher { return cap }

	cmd := newCodeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gum_print(1)", "--allow-destructive", "--yes", "--destructive-budget", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	budget, ok := cap.inv.Args["destructive_budget"].(int)
	if !ok {
		t.Fatalf("args[destructive_budget] is %T; want int", cap.inv.Args["destructive_budget"])
	}
	if budget < 1 || budget > 20 {
		t.Errorf("destructive_budget=%d; the adapter gate accepts 1..20 only", budget)
	}
}

// TestCodeCmdRejectsDestructiveFlagsWithoutAllow pins the usage error. Both
// flags are inert unless allow_destructive is true, and a flag that silently
// does nothing is worse than one that refuses.
func TestCodeCmdRejectsDestructiveFlagsWithoutAllow(t *testing.T) {
	orig := newCodeToolDispatcher
	t.Cleanup(func() { newCodeToolDispatcher = orig })
	newCodeToolDispatcher = func(string) dispatch.Dispatcher {
		t.Fatal("a destructive flag without --allow-destructive must fail before dispatch")
		return nil
	}

	cases := [][]string{
		{"gum_print(1)", "--destructive-budget", "3"},
		{"gum_print(1)", "--destructive-scope", "drive.files.delete:f"},
	}
	for _, args := range cases {
		cmd := newCodeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		err := cmd.Execute()
		if err == nil {
			t.Errorf("%v was accepted; want an error", args[1:])
			continue
		}
		if !strings.Contains(err.Error(), "--allow-destructive") {
			t.Errorf("%v: error %q does not name --allow-destructive", args[1:], err)
		}
	}
}

// TestCodeCmdRejectsMalformedScopeEntry covers the flag-syntax half: an entry
// with no op_id cannot be turned into a scope tuple.
func TestCodeCmdRejectsMalformedScopeEntry(t *testing.T) {
	orig := newCodeToolDispatcher
	t.Cleanup(func() { newCodeToolDispatcher = orig })
	newCodeToolDispatcher = func(string) dispatch.Dispatcher {
		t.Fatal("a malformed scope entry must fail before dispatch")
		return nil
	}

	cmd := newCodeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gum_print(1)", "--allow-destructive", "--destructive-budget", "1", "--destructive-scope", ":file-a"}) //nolint:lll
	if err := cmd.Execute(); err == nil {
		t.Error("an empty op_id was accepted; want an error")
	}
}
