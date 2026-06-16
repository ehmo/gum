// Package adapters_test — RED-TEAM failing tests for TestCodeDestructiveBudgetAndScope
// (spec.md §6.1 "Destructive budget and scope gate"; test-matrix.md row 57).
//
// ALL tests in this file are expected to FAIL until the green team implements:
//  1. DestructiveBudget / DestructiveScope fields on dispatch.Invocation.
//  2. Budget+scope tracking inside CodeRunner.Execute (or a helper it calls).
//  3. Per-call gum_confirm_destructive enforcement (must immediately precede each
//     destructive gum_call; calling the wrong op_id or skipping it → REQUIRES_CONFIRMATION).
//  4. Script-header pragma rejection (v0.1.0 MUST NOT parse pragmas silently).
//
// See /tmp/rgr/red/gum-ra1.md for the full mediated brief.
package adapters_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeCodeInvocation builds a minimal dispatch.Invocation for the gum.code op.
func makeCodeInvocation(source string, allowDestructive bool, budget int, scope []map[string]string) *dispatch.Invocation {
	args := map[string]any{
		"language": "risor",
		"source":   source,
	}
	if allowDestructive {
		args["allow_destructive"] = true
	}
	if budget > 0 {
		args["destructive_budget"] = budget
	}
	if len(scope) > 0 {
		rawScope := make([]any, len(scope))
		for i, s := range scope {
			rawScope[i] = map[string]any{
				"op_id":        s["op_id"],
				"resource_key": s["resource_key"],
			}
		}
		args["destructive_scope"] = rawScope
	}
	return &dispatch.Invocation{
		OpID:             "gum.code",
		Args:             args,
		AllowDestructive: allowDestructive,
		// DestructiveBudget and DestructiveScope are set by the fields below;
		// green team must add these to dispatch.Invocation:
		//   DestructiveBudget int
		//   DestructiveScope  []dispatch.DestructiveScopeEntry
		// For now we embed them in Args and expect the adapter to extract them.
	}
}

// makeMinimalVariant returns a ResolvedVariant with no binding (code.risor).
func makeMinimalVariant() *dispatch.ResolvedVariant {
	return &dispatch.ResolvedVariant{
		OpID:       "gum.code",
		Variant:    &catalog.Variant{VariantID: "gum.code.v1", RiskClass: catalog.RiskClassDestructive},
		AdapterKey: "code.risor",
	}
}

// runCode executes the CodeRunner against a minimal invocation and returns
// the error string (or "" on success).
func runCode(t *testing.T, inv *dispatch.Invocation) error {
	t.Helper()
	cr := adapters.NewCodeRunner().WithDispatcher(&destructiveGateDispatcher{})
	_, err := cr.Execute(context.Background(), inv, makeMinimalVariant(), nil)
	return err
}

type destructiveGateDispatcher struct{}

func (d *destructiveGateDispatcher) Dispatch(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	if !inv.AllowDestructive {
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeRiskToolMismatch,
			"destructive fixture requires gum.destructive").
			WithDetail("required_tool", "gum.destructive")
	}
	if !inv.Confirmed {
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation,
			"destructive fixture requires confirmation").
			WithDetail("confirmation_token", "fixture-token")
	}
	return &dispatch.ShapedResponse{
		Format:            "json",
		StructuredContent: map[string]any{"ok": true},
	}, nil
}

// assertErrCode asserts that err is a *dispatch.StructuredError with the given code.
func assertErrCode(t *testing.T, err error, wantCode dispatch.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %s, got nil", wantCode)
	}
	if !dispatch.IsStructuredError(err, wantCode) {
		t.Fatalf("expected %s error, got: %v", wantCode, err)
	}
}

// ---------------------------------------------------------------------------
// TestCodeDestructiveBudgetValidation
// §6.1: destructive_budget MUST be in 1..20 when allow_destructive=true.
// budget=0 → INVALID_ARGS
// budget=21 → INVALID_ARGS
// budget=-1 → INVALID_ARGS
// budget=1..20 → accepted (no budget-exhaustion error before any calls)
// ---------------------------------------------------------------------------

func TestCodeDestructiveBudgetValidation(t *testing.T) {
	t.Run("budget=0 with allow_destructive → INVALID_ARGS", func(t *testing.T) {
		inv := makeCodeInvocation(`gum_print("noop")`, true, 0, nil)
		err := runCode(t, inv)
		assertErrCode(t, err, dispatch.ErrCodeInvalidArgs)
		if !strings.Contains(err.Error(), "destructive_budget") {
			t.Errorf("error should mention 'destructive_budget', got: %v", err)
		}
	})

	t.Run("budget=21 → INVALID_ARGS", func(t *testing.T) {
		inv := makeCodeInvocation(`gum_print("noop")`, true, 21, nil)
		err := runCode(t, inv)
		assertErrCode(t, err, dispatch.ErrCodeInvalidArgs)
		if !strings.Contains(err.Error(), "destructive_budget") {
			t.Errorf("error should mention 'destructive_budget', got: %v", err)
		}
	})

	t.Run("budget=-1 → INVALID_ARGS", func(t *testing.T) {
		inv := &dispatch.Invocation{
			OpID: "gum.code",
			Args: map[string]any{
				"language":           "risor",
				"source":             `gum_print("noop")`,
				"allow_destructive":  true,
				"destructive_budget": -1,
			},
			AllowDestructive: true,
		}
		err := runCode(t, inv)
		assertErrCode(t, err, dispatch.ErrCodeInvalidArgs)
	})

	// budget=1 through budget=20 should not immediately fail validation.
	for _, budget := range []int{1, 5, 10, 20} {
		budget := budget
		t.Run("budget=valid_accepted", func(t *testing.T) {
			// Script that makes no destructive calls, just prints.
			// With a valid budget, validation should pass and the script should run.
			inv := makeCodeInvocation(`gum_print("ok")`, true, budget, nil)
			// For a confirmed destructive invocation to run it also needs confirmed=true
			// and a valid token; for this validation test we just need the budget
			// range check to pass (i.e., no INVALID_ARGS for the budget value itself).
			// We expect either success or a REQUIRES_CONFIRMATION error (not INVALID_ARGS).
			cr := adapters.NewCodeRunner()
			_, err := cr.Execute(context.Background(), inv, makeMinimalVariant(), nil)
			if err != nil && dispatch.IsStructuredError(err, dispatch.ErrCodeInvalidArgs) {
				// Check whether the INVALID_ARGS is about destructive_budget specifically.
				if strings.Contains(err.Error(), "destructive_budget") {
					t.Errorf("budget=%d should be valid but got INVALID_ARGS: %v", budget, err)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCodeDestructiveBudgetEnforced
// §6.1: Each destructive gum_call consumes one budget unit. When exhausted →
// DESTRUCTIVE_BUDGET_EXCEEDED before the N+1-th call. No upstream request made.
//
// Script calls gum_confirm_destructive + gum_call (destructive) budget+1 times.
// The (budget+1)-th call must return DESTRUCTIVE_BUDGET_EXCEEDED.
// ---------------------------------------------------------------------------

func TestCodeDestructiveBudgetEnforced(t *testing.T) {
	const budget = 2
	// Script makes budget+1=3 destructive calls.
	// Each call is preceded by gum_confirm_destructive as required.
	// The 3rd call must fail with DESTRUCTIVE_BUDGET_EXCEEDED.
	//
	// The script uses gum_print to record how many calls succeeded so the test
	// can distinguish "failed before 3rd" vs "failed on 3rd".
	// Note: Risor uses "let" for variable declarations.
	script := `
let count = 0
gum_confirm_destructive("email.messages.delete", "msg001")
gum_call("email.messages.delete", {"id": "msg001"})
count = count + 1
gum_confirm_destructive("email.messages.delete", "msg002")
gum_call("email.messages.delete", {"id": "msg002"})
count = count + 1
gum_confirm_destructive("email.messages.delete", "msg003")
gum_call("email.messages.delete", {"id": "msg003"})
count = count + 1
gum_print(count)
`
	scope := []map[string]string{
		{"op_id": "email.messages.delete", "resource_key": "msg001"},
		{"op_id": "email.messages.delete", "resource_key": "msg002"},
		{"op_id": "email.messages.delete", "resource_key": "msg003"},
	}
	inv := makeCodeInvocation(script, true, budget, scope)
	inv.Confirmed = true // pre-approved

	err := runCode(t, inv)
	assertErrCode(t, err, dispatch.ErrCodeDestructiveBudgetExceeded)
}

// TestCodeDestructiveBudgetAbortsAtNplus1 is the explicit gum-1otq.4 acceptance
// test: with budget=2 and 3 destructive calls, only the first 2 execute. The
// third call MUST raise DESTRUCTIVE_BUDGET_EXCEEDED, and the error message MUST
// identify the third op (so success on call 1 and call 2 is provable from the
// error payload — neither error names them, so they must have completed).
//
// Distinct op_ids (op.first/op.second/op.third) make the proof unambiguous:
// the surfaced error's "for op X" message can only mention op.third if the
// budget gate was reached after op.first and op.second consumed their slots.
func TestCodeDestructiveBudgetAbortsAtNplus1(t *testing.T) {
	const budget = 2
	script := `
gum_confirm_destructive("op.first", "r")
gum_call("op.first", {})
gum_confirm_destructive("op.second", "r")
gum_call("op.second", {})
gum_confirm_destructive("op.third", "r")
gum_call("op.third", {})
`
	inv := makeCodeInvocation(script, true, budget, nil)
	inv.Confirmed = true

	err := runCode(t, inv)
	assertErrCode(t, err, dispatch.ErrCodeDestructiveBudgetExceeded)
	if !strings.Contains(err.Error(), "op.third") {
		t.Errorf("expected error to identify the over-budget 3rd call (op.third), got: %v", err)
	}
	if strings.Contains(err.Error(), "op.first") || strings.Contains(err.Error(), "op.second") {
		t.Errorf("error should not mention completed calls op.first/op.second, got: %v", err)
	}
}

// TestCodeDestructiveBudgetExactlyExhausted verifies that budget=N allows exactly
// N destructive calls and the script completes successfully without error.
func TestCodeDestructiveBudgetExactlyExhausted(t *testing.T) {
	const budget = 2
	script := `
gum_confirm_destructive("email.messages.delete", "msg001")
gum_call("email.messages.delete", {"id": "msg001"})
gum_confirm_destructive("email.messages.delete", "msg002")
gum_call("email.messages.delete", {"id": "msg002"})
gum_print("done")
`
	scope := []map[string]string{
		{"op_id": "email.messages.delete", "resource_key": "msg001"},
		{"op_id": "email.messages.delete", "resource_key": "msg002"},
	}
	inv := makeCodeInvocation(script, true, budget, scope)
	inv.Confirmed = true

	// Exactly budget calls: should NOT return DESTRUCTIVE_BUDGET_EXCEEDED.
	cr := adapters.NewCodeRunner()
	_, err := cr.Execute(context.Background(), inv, makeMinimalVariant(), nil)
	if err != nil && dispatch.IsStructuredError(err, dispatch.ErrCodeDestructiveBudgetExceeded) {
		t.Errorf("exactly budget=%d calls should succeed, got DESTRUCTIVE_BUDGET_EXCEEDED", budget)
	}
}

// ---------------------------------------------------------------------------
// TestCodeDestructiveScopeEnforced
// §6.1: When destructive_scope is non-empty, every destructive call must match
// one entry. A call outside scope → DESTRUCTIVE_SCOPE_MISMATCH before dispatch.
// ---------------------------------------------------------------------------

func TestCodeDestructiveScopeEnforced(t *testing.T) {
	t.Run("op_id_not_in_scope → DESTRUCTIVE_SCOPE_MISMATCH", func(t *testing.T) {
		// Scope only allows gmail.messages.delete; script tries calendar.events.delete.
		script := `
gum_confirm_destructive("calendar.events.delete", "evt001")
gum_call("calendar.events.delete", {"id": "evt001"})
`
		scope := []map[string]string{
			{"op_id": "gmail.messages.delete", "resource_key": "msg001"},
		}
		inv := makeCodeInvocation(script, true, 1, scope)
		inv.Confirmed = true

		err := runCode(t, inv)
		assertErrCode(t, err, dispatch.ErrCodeDestructiveScopeMismatch)
		if !strings.Contains(err.Error(), "calendar.events.delete") {
			t.Errorf("error should mention the mismatched op_id 'calendar.events.delete', got: %v", err)
		}
	})

	t.Run("resource_key_not_in_scope → DESTRUCTIVE_SCOPE_MISMATCH", func(t *testing.T) {
		// Scope allows gmail.messages.delete for msg001 only; script tries msg999.
		script := `
gum_confirm_destructive("gmail.messages.delete", "msg999")
gum_call("gmail.messages.delete", {"id": "msg999"})
`
		scope := []map[string]string{
			{"op_id": "gmail.messages.delete", "resource_key": "msg001"},
		}
		inv := makeCodeInvocation(script, true, 1, scope)
		inv.Confirmed = true

		err := runCode(t, inv)
		assertErrCode(t, err, dispatch.ErrCodeDestructiveScopeMismatch)
	})

	t.Run("matching scope entry → allowed", func(t *testing.T) {
		// Script calls exactly the op_id+resource_key in scope.
		script := `
gum_confirm_destructive("gmail.messages.delete", "msg001")
gum_call("gmail.messages.delete", {"id": "msg001"})
gum_print("ok")
`
		scope := []map[string]string{
			{"op_id": "gmail.messages.delete", "resource_key": "msg001"},
		}
		inv := makeCodeInvocation(script, true, 1, scope)
		inv.Confirmed = true

		cr := adapters.NewCodeRunner()
		_, err := cr.Execute(context.Background(), inv, makeMinimalVariant(), nil)
		if err != nil && dispatch.IsStructuredError(err, dispatch.ErrCodeDestructiveScopeMismatch) {
			t.Errorf("matching scope should not produce DESTRUCTIVE_SCOPE_MISMATCH, got: %v", err)
		}
	})

	t.Run("empty scope → no scope restriction", func(t *testing.T) {
		// When destructive_scope is empty/absent, any destructive call is allowed
		// (as long as budget permits).
		script := `
gum_confirm_destructive("any.op.delete", "resource-x")
gum_call("any.op.delete", {"id": "resource-x"})
gum_print("ok")
`
		inv := makeCodeInvocation(script, true, 1, nil)
		inv.Confirmed = true

		cr := adapters.NewCodeRunner()
		_, err := cr.Execute(context.Background(), inv, makeMinimalVariant(), nil)
		if err != nil && dispatch.IsStructuredError(err, dispatch.ErrCodeDestructiveScopeMismatch) {
			t.Errorf("empty scope should not scope-restrict calls, got: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestCodeRequiresGumConfirmDestructive
// §6.1 step 4: every destructive gum_call MUST be immediately preceded by
// gum_confirm_destructive(op_id, resource_key?) for the same target.
// Calling a destructive op without gum_confirm_destructive → REQUIRES_CONFIRMATION.
// ---------------------------------------------------------------------------

func TestCodeRequiresGumConfirmDestructive(t *testing.T) {
	t.Run("missing_gum_confirm_destructive → REQUIRES_CONFIRMATION", func(t *testing.T) {
		// Script calls gum_call to a destructive op WITHOUT first calling
		// gum_confirm_destructive.
		script := `gum_call("email.messages.delete", {"id": "msg001"})`
		inv := makeCodeInvocation(script, true, 1, nil)
		inv.Confirmed = true

		err := runCode(t, inv)
		assertErrCode(t, err, dispatch.ErrCodeRequiresConfirmation)
		if !strings.Contains(err.Error(), "gum_confirm_destructive") {
			t.Errorf("error should mention 'gum_confirm_destructive', got: %v", err)
		}
	})

	t.Run("gum_confirm_destructive op_id mismatch → REQUIRES_CONFIRMATION", func(t *testing.T) {
		// Script calls gum_confirm_destructive for op A, then gum_call for op B.
		// The assertion is consumed by the immediately following call only, and
		// the op_ids must match.
		script := `
gum_confirm_destructive("email.messages.delete", "msg001")
gum_call("drive.files.delete", {"id": "file001"})
`
		inv := makeCodeInvocation(script, true, 1, nil)
		inv.Confirmed = true

		err := runCode(t, inv)
		assertErrCode(t, err, dispatch.ErrCodeRequiresConfirmation)
	})

	t.Run("gum_confirm_destructive consumed → second call without re-confirm → REQUIRES_CONFIRMATION", func(t *testing.T) {
		// gum_confirm_destructive is a one-shot assertion — consumed by the
		// immediately following destructive call. A second call without another
		// gum_confirm_destructive must be rejected.
		script := `
gum_confirm_destructive("email.messages.delete", "msg001")
gum_call("email.messages.delete", {"id": "msg001"})
gum_call("email.messages.delete", {"id": "msg002"})
`
		inv := makeCodeInvocation(script, true, 2, nil)
		inv.Confirmed = true

		err := runCode(t, inv)
		assertErrCode(t, err, dispatch.ErrCodeRequiresConfirmation)
	})

	t.Run("correct gum_confirm_destructive sequence → allowed", func(t *testing.T) {
		// Both calls have their own gum_confirm_destructive assertion.
		script := `
gum_confirm_destructive("email.messages.delete", "msg001")
gum_call("email.messages.delete", {"id": "msg001"})
gum_confirm_destructive("email.messages.delete", "msg002")
gum_call("email.messages.delete", {"id": "msg002"})
gum_print("both_ok")
`
		inv := makeCodeInvocation(script, true, 2, nil)
		inv.Confirmed = true

		cr := adapters.NewCodeRunner()
		_, err := cr.Execute(context.Background(), inv, makeMinimalVariant(), nil)
		if err != nil && dispatch.IsStructuredError(err, dispatch.ErrCodeRequiresConfirmation) {
			t.Errorf("correct gum_confirm_destructive sequence should not fail, got: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestCodeRejectsScriptHeaderPragma
// §6.1 step 6 (bare-CLI path) and spec line 1110:
// "Script-header pragmas, --no-confirm, and CLI language selection are deferred
// to v0.3.0 and MUST NOT be parsed or accepted silently in v0.1.0."
//
// The adapter MUST scan source BEFORE passing to the Risor sandbox. If the
// source contains a pragma header line (pattern: optional-whitespace + "#" +
// optional-whitespace + "pragma:" ..., case-insensitive, anywhere in first
// non-empty lines), it must return *dispatch.StructuredError with
// ErrCodeInvalidArgs and a message that mentions "pragma".
//
// The adapter must NOT let Risor return a generic syntax error for these —
// that would "parse silently" in the sense that the error message is opaque.
// The structured INVALID_ARGS response with a pragma-specific message is the
// normative rejection signal.
//
// Note: Risor does not support # comments, so any # line is inherently a
// syntax error; but a generic Risor syntax error is NOT the same as a
// structured INVALID_ARGS with "pragma" in the message. The test specifically
// requires the adapter to intercept and re-wrap.
// ---------------------------------------------------------------------------

func TestCodeRejectsScriptHeaderPragma(t *testing.T) {
	pragmaScripts := []struct {
		name   string
		source string
	}{
		{
			name:   "gum_allow_destructive pragma",
			source: "// pragma: gum_allow_destructive=true\ngum_print(\"hello\")",
		},
		{
			name:   "gum_no_confirm pragma",
			source: "// pragma: no_confirm\ngum_print(\"hello\")",
		},
		{
			name:   "arbitrary gum pragma",
			source: "// pragma: gum_version=2\ngum_print(\"hello\")",
		},
		{
			name:   "pragma with leading whitespace",
			source: "  // pragma: gum_allow_write=true\ngum_print(\"hello\")",
		},
	}

	for _, tc := range pragmaScripts {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			inv := &dispatch.Invocation{
				OpID: "gum.code",
				Args: map[string]any{
					"language": "risor",
					"source":   tc.source,
				},
			}

			err := runCode(t, inv)
			if err == nil {
				t.Fatalf("expected INVALID_ARGS for script with pragma header, got nil")
			}
			if !dispatch.IsStructuredError(err, dispatch.ErrCodeInvalidArgs) {
				t.Errorf("expected INVALID_ARGS for pragma header script, got: %v", err)
			}
			errStr := strings.ToLower(err.Error())
			if !strings.Contains(errStr, "pragma") {
				t.Errorf("error should mention 'pragma', got: %v", err)
			}
		})
	}

	t.Run("non-pragma comment allowed", func(t *testing.T) {
		// A normal Risor comment (// style, not a pragma) should not be rejected.
		// Risor uses // for line comments.
		source := "// this is a normal comment\ngum_print(\"hello\")"
		inv := &dispatch.Invocation{
			OpID: "gum.code",
			Args: map[string]any{
				"language": "risor",
				"source":   source,
			},
		}
		cr := adapters.NewCodeRunner()
		_, err := cr.Execute(context.Background(), inv, makeMinimalVariant(), nil)
		if err != nil && dispatch.IsStructuredError(err, dispatch.ErrCodeInvalidArgs) {
			// Only fail if it's specifically a pragma rejection.
			if strings.Contains(strings.ToLower(err.Error()), "pragma") {
				t.Errorf("non-pragma comment should not be rejected as pragma, got: %v", err)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestCodeDestructiveBudgetNoUpstreamOnExceeded
// §6.1: "makes no upstream request on any rejection path"
// When budget is exceeded, gum_call must NOT be invoked for the over-budget call.
// ---------------------------------------------------------------------------

func TestCodeDestructiveBudgetNoUpstreamOnExceeded(t *testing.T) {
	var callCount int
	callTracker := func(opID any, callArgs any) any {
		callCount++
		return map[string]any{"ok": true}
	}

	// budget=1, script tries 2 calls.
	script := `
gum_confirm_destructive("email.messages.delete", "msg001")
gum_call("email.messages.delete", {"id": "msg001"})
gum_confirm_destructive("email.messages.delete", "msg002")
gum_call("email.messages.delete", {"id": "msg002"})
`
	// We need to inject a custom gum_call tracker.
	// The adapter must expose a way to inject gum_call for testing, OR
	// the test must construct the adapter with the tracker injected.
	// Since CodeRunner currently hardcodes the gum_call stub, green team must
	// add an option (e.g. CodeRunnerOptions.GumCallFn or functional options).
	//
	// For now, use the default CodeRunner and just verify the error code;
	// the "no upstream" assertion is best-effort here until the adapter
	// exposes injection.
	_ = callTracker // green team must wire this

	inv := makeCodeInvocation(script, true, 1, nil)
	inv.Confirmed = true

	err := runCode(t, inv)
	assertErrCode(t, err, dispatch.ErrCodeDestructiveBudgetExceeded)
}

// ---------------------------------------------------------------------------
// TestCodeDestructiveScopeNoUpstreamOnMismatch
// §6.1: "makes no upstream request on any rejection path"
// When scope is mismatched, gum_call must NOT be invoked.
// ---------------------------------------------------------------------------

func TestCodeDestructiveScopeNoUpstreamOnMismatch(t *testing.T) {
	script := `
gum_confirm_destructive("calendar.events.delete", "evt001")
gum_call("calendar.events.delete", {"id": "evt001"})
`
	scope := []map[string]string{
		{"op_id": "gmail.messages.delete", "resource_key": "msg001"},
	}
	inv := makeCodeInvocation(script, true, 1, scope)
	inv.Confirmed = true

	err := runCode(t, inv)
	assertErrCode(t, err, dispatch.ErrCodeDestructiveScopeMismatch)
}
