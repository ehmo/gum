package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// codeConfirmRecorder stands in for the kernel and reproduces the §6.1.2
// handshake: an elevated invocation without Confirmed gets a
// REQUIRES_CONFIRMATION envelope carrying a fresh token, and only an echo of
// that exact token runs the script. It keeps every Invocation it saw so a test
// can assert how many round trips the CLI made.
type codeConfirmRecorder struct {
	invs  []dispatch.Invocation
	token string
}

func (r *codeConfirmRecorder) Dispatch(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	r.invs = append(r.invs, *inv)
	if (inv.AllowWrite || inv.AllowDestructive) && !inv.Confirmed {
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation,
			"op gum.code requires confirmed=true with a valid confirmation_token").
			WithDetail("confirmation_token", r.token)
	}
	if inv.Confirmed && inv.ConfirmationToken != r.token {
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeConfirmationTokenInvalid,
			"confirmation token invalid")
	}
	return &dispatch.ShapedResponse{Body: []byte("ran"), Format: "json"}, nil
}

func stubCodeDispatcher(t *testing.T) *codeConfirmRecorder {
	t.Helper()
	rec := &codeConfirmRecorder{token: "ct-issued-by-kernel"}
	orig := newCodeToolDispatcher
	t.Cleanup(func() { newCodeToolDispatcher = orig })
	newCodeToolDispatcher = func(string) dispatch.Dispatcher { return rec }
	return rec
}

// forceCodeTTY drives the interactive arm without a PTY.
func forceCodeTTY(t *testing.T, tty bool) {
	t.Helper()
	orig := codeStdinIsTTY
	t.Cleanup(func() { codeStdinIsTTY = orig })
	codeStdinIsTTY = func(io.Reader) bool { return tty }
}

func runCodeCmd(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := newCodeCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func elevatedArgs(flag string) []string {
	args := []string{"gum_print(1)", flag}
	if flag == "--allow-destructive" {
		args = append(args, "--destructive-budget", "1")
	}
	return args
}

// TestCLICodeConfirmationAndScopeGrammar is the docs/test-matrix.md row-100
// proof for spec §6.1 step 6: `--allow-write` / `--allow-destructive` require
// an interactive y or a non-interactive `--yes`, read-only scripts require
// neither, repeated `--destructive-scope op_id[:resource_key]` is the only
// scope grammar, and the v0.3.0 flags fail parsing instead of being accepted.
func TestCLICodeConfirmationAndScopeGrammar(t *testing.T) {
	t.Run("read_only_script_needs_no_consent", func(t *testing.T) {
		rec := stubCodeDispatcher(t)
		forceCodeTTY(t, true)
		_, stderr, err := runCodeCmd(t, "", "gum_print(1)")
		if err != nil {
			t.Fatalf("read-only gum code failed: %v\nstderr: %s", err, stderr)
		}
		if len(rec.invs) != 1 {
			t.Fatalf("dispatched %d times; want 1", len(rec.invs))
		}
		if strings.Contains(stderr, "[y/N]") {
			t.Errorf("read-only run prompted for confirmation: %q", stderr)
		}
	})

	for _, flag := range []string{"--allow-write", "--allow-destructive"} {
		name := strings.TrimPrefix(flag, "--")

		t.Run(name+"_without_yes_is_requires_confirmation", func(t *testing.T) {
			rec := stubCodeDispatcher(t)
			forceCodeTTY(t, false)
			_, stderr, err := runCodeCmd(t, "", elevatedArgs(flag)...)
			var se *dispatch.StructuredError
			if !errors.As(err, &se) || se.ErrCode != dispatch.ErrCodeRequiresConfirmation {
				t.Fatalf("err = %v; want REQUIRES_CONFIRMATION", err)
			}
			if len(rec.invs) != 0 {
				t.Fatalf("dispatched %d times; want 0, confirmation gates before dispatch", len(rec.invs))
			}
			if !strings.Contains(stderr, "--yes") {
				t.Errorf("stderr does not name --yes: %q", stderr)
			}
		})

		t.Run(name+"_with_yes_runs", func(t *testing.T) {
			rec := stubCodeDispatcher(t)
			forceCodeTTY(t, false)
			stdout, stderr, err := runCodeCmd(t, "", append(elevatedArgs(flag), "--yes")...)
			if err != nil {
				t.Fatalf("--yes did not authorise the run: %v\nstderr: %s", err, stderr)
			}
			if len(rec.invs) != 2 {
				t.Fatalf("dispatched %d times; want 2 (first contact, then the token echo)", len(rec.invs))
			}
			if rec.invs[1].ConfirmationToken != rec.token || !rec.invs[1].Confirmed {
				t.Errorf("retry carried confirmed=%v token=%q; want true / %q",
					rec.invs[1].Confirmed, rec.invs[1].ConfirmationToken, rec.token)
			}
			// §6.1 keeps confirmation_token an MCP construct: the operator
			// consents with --yes and never sees or retypes a token.
			if strings.Contains(stdout+stderr, rec.token) {
				t.Errorf("confirmation token leaked onto the CLI surface: %q %q", stdout, stderr)
			}
			if strings.Contains(stderr, "[y/N]") {
				t.Errorf("--yes still prompted: %q", stderr)
			}
		})

		t.Run(name+"_interactive_y_runs", func(t *testing.T) {
			rec := stubCodeDispatcher(t)
			forceCodeTTY(t, true)
			_, stderr, err := runCodeCmd(t, "y\n", elevatedArgs(flag)...)
			if err != nil {
				t.Fatalf("interactive y did not authorise the run: %v\nstderr: %s", err, stderr)
			}
			if !strings.Contains(stderr, "[y/N]") {
				t.Errorf("no confirmation prompt on stderr: %q", stderr)
			}
			if len(rec.invs) != 2 {
				t.Fatalf("dispatched %d times; want 2", len(rec.invs))
			}
		})
	}

	refusals := []struct{ name, answer string }{
		{"explicit_n", "n\n"},
		{"bare_newline", "\n"},
		{"closed_stdin", ""},
		{"y_with_trailing_words", "Y es\n"},
	}
	for _, tc := range refusals {
		answer := tc.answer
		t.Run("interactive_refusal_"+tc.name, func(t *testing.T) {
			rec := stubCodeDispatcher(t)
			forceCodeTTY(t, true)
			_, _, err := runCodeCmd(t, answer, "gum_print(1)", "--allow-write")
			var se *dispatch.StructuredError
			if !errors.As(err, &se) || se.ErrCode != dispatch.ErrCodeRequiresConfirmation {
				t.Fatalf("answer %q: err = %v; want REQUIRES_CONFIRMATION", answer, err)
			}
			if len(rec.invs) != 0 {
				t.Fatalf("answer %q dispatched %d times; want 0", answer, len(rec.invs))
			}
		})
	}

	t.Run("deferred_v030_flags_fail_parsing", func(t *testing.T) {
		for _, extra := range [][]string{
			{"--no-confirm"},
			{"--lang", "risor"},
			{"--lang=risor"},
		} {
			rec := stubCodeDispatcher(t)
			forceCodeTTY(t, false)
			args := append([]string{"gum_print(1)", "--yes"}, extra...)
			_, _, err := runCodeCmd(t, "", args...)
			if err == nil {
				t.Errorf("%v was accepted; spec §6.1 defers it to v0.3.0", extra)
			}
			if len(rec.invs) != 0 {
				t.Errorf("%v reached dispatch; want a parse failure", extra)
			}
		}
	})

	t.Run("destructive_scope_grammar", func(t *testing.T) {
		rec := stubCodeDispatcher(t)
		forceCodeTTY(t, false)
		_, stderr, err := runCodeCmd(t, "", "gum_print(1)", "--allow-destructive", "--yes",
			"--destructive-budget", "2",
			"--destructive-scope", "drive.files.delete:file-a",
			"--destructive-scope", "gmail.users.messages.delete")
		if err != nil {
			t.Fatalf("scope grammar rejected: %v\nstderr: %s", err, stderr)
		}
		scope, ok := rec.invs[0].Args["destructive_scope"].([]any)
		if !ok || len(scope) != 2 {
			t.Fatalf("args[destructive_scope] = %#v; want 2 entries as []any", rec.invs[0].Args["destructive_scope"])
		}
		first, _ := scope[0].(map[string]any)
		if first["op_id"] != "drive.files.delete" || first["resource_key"] != "file-a" {
			t.Errorf("scope[0] = %#v; want op_id/resource_key from op_id:resource_key", scope[0])
		}
		second, _ := scope[1].(map[string]any)
		if second["op_id"] != "gmail.users.messages.delete" || second["resource_key"] != "" {
			t.Errorf("scope[1] = %#v; want a bare op_id with an empty resource_key", scope[1])
		}

		_, _, err = runCodeCmd(t, "", "gum_print(1)", "--allow-destructive", "--yes",
			"--destructive-budget", "1", "--destructive-scope", ":file-a")
		if err == nil {
			t.Error("a scope entry with no op_id was accepted")
		}
	})
}

// TestCodeCmdConfirmedFlagSatisfiesConsent pins the escape hatch: an operator
// who passes --confirmed has already stated the intent the prompt asks for, so
// the CLI gate stands aside and lets the kernel judge the token instead.
func TestCodeCmdConfirmedFlagSatisfiesConsent(t *testing.T) {
	rec := stubCodeDispatcher(t)
	forceCodeTTY(t, false)

	_, _, err := runCodeCmd(t, "", "gum_print(1)", "--allow-write", "--confirmed", "--token", "ct-issued-by-kernel")
	if err != nil {
		t.Fatalf("--confirmed was refused by the CLI gate: %v", err)
	}
	if len(rec.invs) != 1 {
		t.Fatalf("dispatched %d times; want 1, --confirmed skips the CLI handshake", len(rec.invs))
	}
	if !rec.invs[0].Confirmed || rec.invs[0].ConfirmationToken != "ct-issued-by-kernel" {
		t.Errorf("invocation = confirmed:%v token:%q; want the operator's own pair",
			rec.invs[0].Confirmed, rec.invs[0].ConfirmationToken)
	}
}

// TestCodeCmdElevatedYesRunsAgainstEmbeddedCatalog is the shipped-surface half
// of the row-100 contract. It drives the real newDefaultCodeDispatcher and the
// embedded catalog, so it proves the whole confirmation handshake, CLI consent
// plus kernel token issue and verify, actually executes an elevated script and
// never makes the operator read a token off their terminal.
func TestCodeCmdElevatedYesRunsAgainstEmbeddedCatalog(t *testing.T) {
	const marker = "gum-code-elevated-consent-ok"

	cmd := newCodeCmd()
	cmd.SetArgs([]string{`gum_print("` + marker + `")`, "--allow-write", "--yes"})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(""))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("elevated gum code --yes failed: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), marker) {
		t.Errorf("stdout = %q, want %q (sandbox did not run)", stdout.String(), marker)
	}
	if both := stdout.String() + stderr.String(); strings.Contains(both, "confirmation_token") {
		t.Errorf("output leaked a confirmation token to the operator:\n%s", both)
	}
}
