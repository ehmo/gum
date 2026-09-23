package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/spf13/cobra"
)

// failWriter rejects every write, modeling a closed pipe on stdout or stderr.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

// staticDispatcher returns a fixed response so a test can drive the output
// tails of `gum call` without an upstream.
type staticDispatcher struct{ res *dispatch.ShapedResponse }

func (s staticDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return s.res, nil
}

// errDispatcher fails every call with a fixed error.
type errDispatcher struct{ err error }

func (e errDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return nil, e.err
}

// runCallWith executes `gum call` against d and returns stdout, stderr and the
// Execute error. out overrides the stdout writer when it is non-nil.
func runCallWith(t *testing.T, d dispatch.Dispatcher, out *bytes.Buffer, args ...string) (string, string, error) {
	t.Helper()
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher { return d }

	stdout := out
	if stdout == nil {
		stdout = &bytes.Buffer{}
	}
	var stderr bytes.Buffer
	cmd := newCallCmd()
	cmd.PersistentFlags().String("profile", "", "")
	// A non-file stdin keeps the interactive wizard out of the way: these arms
	// exercise the non-TTY path a script or agent takes.
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// TestCallRequiresAnOpID pins the MinimumNArgs guard: `gum call` with no
// positional names no operation, so it fails before the required-flag check.
func TestCallRequiresAnOpID(t *testing.T) {
	cmd := newCallCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(nil)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want an error for a call with no op_id")
	}
	if !strings.Contains(err.Error(), "arg") {
		t.Fatalf("got %v, want an argument-count error", err)
	}
}

// TestCallSkeletonArms covers the --skeleton introspection path: it must not
// require --risk, it refuses an unknown op_id, and it prints the template.
func TestCallSkeletonArms(t *testing.T) {
	t.Run("unknown op", func(t *testing.T) {
		_, _, err := runCallWith(t, staticDispatcher{}, nil, "no.such.op", "--skeleton")
		if err == nil || !strings.Contains(err.Error(), "unknown op_id") {
			t.Fatalf("got %v, want an unknown op_id refusal", err)
		}
	})

	t.Run("known op prints the template without --risk", func(t *testing.T) {
		stdout, _, err := runCallWith(t, staticDispatcher{}, nil,
			"gmail.users.messages.list", "--skeleton")
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !strings.Contains(stdout, "gmail.users.messages.list") {
			t.Fatalf("skeleton output = %q, want the op_id", stdout)
		}
	})
}

// TestCallPreDispatchRejections covers the validation arms that fail before any
// dispatcher runs: an ungrammatical positional, a mistyped field, and a bad
// --max-items.
func TestCallPreDispatchRejections(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "positional is not key=value",
			args: []string{"gmail.users.messages.list", "--risk=read", "userId"},
			want: "userId",
		},
		{
			name: "integer field carries a word",
			args: []string{"gmail.users.messages.list", "--risk=read", "maxResults=abc"},
			want: "maxResults",
		},
		{
			name: "max-items is not a count",
			args: []string{"gmail.users.messages.list", "--risk=read", "--max-items=abc"},
			want: "--max-items",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runCallWith(t, staticDispatcher{}, nil, tc.args...)
			if err == nil {
				t.Fatal("want a pre-dispatch rejection")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error naming %s", err, tc.want)
			}
		})
	}
}

// TestCallHostFlagsReachTheInvocation pins that --fields, --page-token and
// --page-size land in Args even when no positional arg was supplied, so the
// invocation the kernel sees carries them.
func TestCallHostFlagsReachTheInvocation(t *testing.T) {
	cases := []struct {
		name string
		flag string
		key  string
		want any
	}{
		{"fields", "--fields=id,snippet", "fields", "id,snippet"},
		{"page token", "--page-token=tok123", "pageToken", "tok123"},
		{"page size", "--page-size=7", "maxResults", 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cap := &capturingDispatcher{}
			if _, _, err := runCallWith(t, cap, nil,
				"gmail.users.messages.list", "--risk=read", tc.flag); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if cap.inv == nil {
				t.Fatal("dispatcher was not called")
			}
			if got := cap.inv.Args[tc.key]; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Args[%q] = %#v, want %#v", tc.key, got, tc.want)
			}
		})
	}
}

// TestCallRenderFailures covers the two CLI-render error arms: an upstream body
// that is not JSON, and a render that cannot write its output.
func TestCallRenderFailures(t *testing.T) {
	t.Run("upstream body is not JSON", func(t *testing.T) {
		d := staticDispatcher{res: &dispatch.ShapedResponse{Body: []byte("<html>"), Format: "json"}}
		_, _, err := runCallWith(t, d, nil,
			"gmail.users.messages.list", "--risk=read", "--output", "table")
		if err == nil || !strings.Contains(err.Error(), "not JSON") {
			t.Fatalf("got %v, want a non-JSON render refusal", err)
		}
	})

	t.Run("render cannot write", func(t *testing.T) {
		d := staticDispatcher{res: &dispatch.ShapedResponse{Body: []byte(`{"id":"1"}`), Format: "json"}}
		orig := newCallDispatcher
		t.Cleanup(func() { newCallDispatcher = orig })
		newCallDispatcher = func(string) dispatch.Dispatcher { return d }

		cmd := newCallCmd()
		cmd.PersistentFlags().String("profile", "", "")
		cmd.SetIn(strings.NewReader(""))
		cmd.SetOut(failWriter{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"gmail.users.messages.list", "--risk=read", "--output", "table"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("want the writer failure to surface")
		}
	})
}

// TestCallStructuredOnlyResponse covers the output tail taken when the kernel
// returns structured content and no body bytes.
func TestCallStructuredOnlyResponse(t *testing.T) {
	d := staticDispatcher{res: &dispatch.ShapedResponse{
		StructuredContent: map[string]any{"id": "abc"},
		Format:            "json",
	}}
	stdout, _, err := runCallWith(t, d, nil,
		"gmail.users.messages.list", "--risk=read", "--output", "json")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, `"id":"abc"`) {
		t.Fatalf("stdout = %q, want the structured content as JSON", stdout)
	}
}

// TestCallMarkdownFlagSelectsMarkdown covers the --markdown selector arm of
// resolveCallFormat through the real command.
func TestCallMarkdownFlagSelectsMarkdown(t *testing.T) {
	d := staticDispatcher{res: &dispatch.ShapedResponse{Body: []byte(`{"id":"1"}`), Format: "json"}}
	stdout, _, err := runCallWith(t, d, nil,
		"gmail.users.messages.list", "--risk=read", "--markdown")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "|") {
		t.Fatalf("stdout = %q, want a markdown table", stdout)
	}
}

// TestCallRiskCompletion pins the closed --risk enum offered to the shell.
func TestCallRiskCompletion(t *testing.T) {
	cmd := newCallCmd()
	fn, ok := cmd.GetFlagCompletionFunc("risk")
	if !ok {
		t.Fatal("--risk has no completion function")
	}
	got, directive := fn(cmd, nil, "")
	want := []string{"read", "write", "destructive"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("risk completions = %v, want %v", got, want)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
}

// TestCompleteFieldsForOpArms covers the guards of the --fields completion.
// Each case names an op outside the curated §9.1 default_fields set, so every
// catalog path here returns an empty candidate list; fieldMaskCandidates
// carries the expansion logic.
func TestCompleteFieldsForOpArms(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no op_id yet", nil},
		{"op without default_fields", []string{"gmail.users.messages.list"}},
		{"unknown op", []string{"no.such.op"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, directive := completeFieldsForOp(nil, tc.args, "")
			if got != nil {
				t.Fatalf("completions = %v, want none", got)
			}
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf("directive = %v, want NoFileComp", directive)
			}
		})
	}
}

// TestFieldMaskCandidates covers the mask expansion: distinct fields first, the
// whole mask last, blank and duplicate segments dropped, prefix respected.
func TestFieldMaskCandidates(t *testing.T) {
	cases := []struct {
		name       string
		mask       string
		toComplete string
		want       []string
	}{
		{
			name: "every field plus the whole mask",
			mask: "id,snippet",
			want: []string{"id", "snippet", "id,snippet"},
		},
		{
			name: "blank and duplicate segments drop out",
			mask: "id, ,id,snippet",
			want: []string{"id", "snippet", "id, ,id,snippet"},
		},
		{
			name:       "prefix filters the candidates",
			mask:       "id,snippet",
			toComplete: "sn",
			want:       []string{"snippet"},
		},
		{
			name:       "prefix matching the whole mask keeps it",
			mask:       "id,snippet",
			toComplete: "id,",
			want:       []string{"id,snippet"},
		},
		{
			// A sub-selection is one candidate, not one per inner field. The
			// commas inside the parentheses are not separators, so splitting on
			// them would offer "labels(id" -- a mask no API accepts.
			name: "sub-selection stays whole",
			mask: "labels(id,name),nextPageToken",
			want: []string{
				"labels(id,name)",
				"nextPageToken",
				"labels(id,name),nextPageToken",
			},
		},
		{
			name: "a single-selector mask is offered once",
			mask: "labels(id,name)",
			want: []string{"labels(id,name)"},
		},
		{
			name: "nested sub-selections stay whole",
			mask: "items(id,snippet(title,thumbnails(default))),pageInfo",
			want: []string{
				"items(id,snippet(title,thumbnails(default)))",
				"pageInfo",
				"items(id,snippet(title,thumbnails(default))),pageInfo",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fieldMaskCandidates(tc.mask, tc.toComplete)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("fieldMaskCandidates(%q, %q) = %v, want %v",
					tc.mask, tc.toComplete, got, tc.want)
			}
		})
	}
}

// TestPrintDispatchErrorArms covers the two non-envelope arms: a plain error
// passes through untouched, and a stderr that cannot be written surfaces the
// write failure instead of the envelope.
func TestPrintDispatchErrorArms(t *testing.T) {
	t.Run("plain error passes through", func(t *testing.T) {
		want := errors.New("boom")
		if got := printDispatchError(&bytes.Buffer{}, "read", want); !errors.Is(got, want) {
			t.Fatalf("got %v, want the original error", got)
		}
	})

	t.Run("envelope write failure surfaces", func(t *testing.T) {
		se := &dispatch.StructuredError{
			ErrCode: dispatch.ErrCodeRiskToolMismatch,
			Message: "risk mismatch",
			Detail:  map[string]any{"variant_risk_class": "write"},
		}
		err := printDispatchError(failWriter{}, "read", se)
		if err == nil || !strings.Contains(err.Error(), "pipe closed") {
			t.Fatalf("got %v, want the writer failure", err)
		}
	})
}

// TestCallDispatchErrorIsRendered pins that a structured dispatch failure is
// rendered as the §7 envelope on stderr and still exits non-zero.
func TestCallDispatchErrorIsRendered(t *testing.T) {
	se := &dispatch.StructuredError{
		ErrCode: dispatch.ErrCodeRiskToolMismatch,
		Message: "risk mismatch",
		Detail:  map[string]any{"variant_risk_class": "write"},
	}
	_, stderr, err := runCallWith(t, errDispatcher{err: se}, nil,
		"gmail.users.messages.list", "--risk=read")
	if err == nil {
		t.Fatal("want a non-zero exit")
	}
	if !strings.Contains(stderr, "RISK_TOOL_MISMATCH") {
		t.Fatalf("stderr = %q, want the error envelope", stderr)
	}
	if !strings.Contains(stderr, "--risk=write") {
		t.Fatalf("stderr = %q, want the required risk flag", stderr)
	}
}
