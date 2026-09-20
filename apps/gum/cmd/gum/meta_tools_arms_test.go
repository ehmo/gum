package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ehmo/gum/internal/dispatch"
)

// nanDispatcher returns structured content json.Marshal cannot encode, so the
// structured-only branch fails on the marshal rather than on the write.
type nanDispatcher struct{}

func (nanDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return &dispatch.ShapedResponse{StructuredContent: make(chan int)}, nil
}

func TestMetaToolFormatArms(t *testing.T) {
	t.Run("unknown --format is rejected locally", func(t *testing.T) {
		_, err := metaToolFormat("", "yaml")
		if err == nil || !strings.Contains(err.Error(), `unknown --format "yaml"`) {
			t.Fatalf("want the format usage error, got %v", err)
		}
	})

	t.Run("GUM_DEFAULT_OUTPUT supplies the default", func(t *testing.T) {
		t.Setenv("GUM_DEFAULT_OUTPUT", "csv")
		got, err := metaToolFormat("", "")
		if err != nil || got != "csv" {
			t.Fatalf("got (%q,%v), want (\"csv\",nil)", got, err)
		}
	})

	t.Run("an invalid GUM_DEFAULT_OUTPUT falls through", func(t *testing.T) {
		t.Setenv("GUM_DEFAULT_OUTPUT", "yaml")
		got, err := metaToolFormat("", "")
		if err != nil || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",nil): a bad personal default is ignored, never fatal", got, err)
		}
	})
}

func TestOnEmptyMessageOfArms(t *testing.T) {
	msg := "no messages matched"
	shaped := &dispatch.ShapedResponse{Expression: &dispatch.ExpressionMeta{OnEmptyMessage: &msg}}
	if got := onEmptyMessageOf(shaped); got != msg {
		t.Fatalf("onEmptyMessageOf = %q, want %q", got, msg)
	}
	if got := onEmptyMessageOf(&dispatch.ShapedResponse{}); got != "" {
		t.Fatalf("onEmptyMessageOf without an envelope = %q, want empty", got)
	}
}

// The notice is written for an operator, so a command with no stderr and a
// dispatch that produced nothing both leave it silent rather than panicking.
func TestPrintShapingNoticeIsANoOpWithoutInput(t *testing.T) {
	printShapingNotice(nil, &dispatch.ShapedResponse{})
	printShapingNotice(&bytes.Buffer{}, nil)
}

func TestDispatchToWriterStructuredOnlyArms(t *testing.T) {
	inv := &dispatch.Invocation{OpID: "gmail.users.labels.list"}

	t.Run("structured content is emitted when the body is empty", func(t *testing.T) {
		d := staticDispatcher{res: &dispatch.ShapedResponse{
			StructuredContent: map[string]any{"labels": []any{"INBOX"}},
		}}
		var out, errOut bytes.Buffer
		err := dispatchToWriterWithFactory(context.Background(), "default", &out, &errOut, inv, "read",
			func(string) dispatch.Dispatcher { return d })
		if err != nil {
			t.Fatalf("dispatchToWriter: %v", err)
		}
		if !strings.Contains(out.String(), `"labels"`) {
			t.Fatalf("stdout missing the structured payload: %q", out.String())
		}
	})

	t.Run("unencodable structured content surfaces the marshal error", func(t *testing.T) {
		var out, errOut bytes.Buffer
		err := dispatchToWriterWithFactory(context.Background(), "default", &out, &errOut, inv, "read",
			func(string) dispatch.Dispatcher { return nanDispatcher{} })
		if err == nil {
			t.Fatal("want a marshal error for an unencodable value")
		}
	})

	t.Run("a closed stdout surfaces the write error", func(t *testing.T) {
		d := staticDispatcher{res: &dispatch.ShapedResponse{
			StructuredContent: map[string]any{"labels": []any{"INBOX"}},
		}}
		var errOut bytes.Buffer
		err := dispatchToWriterWithFactory(context.Background(), "default", failWriter{}, &errOut, inv, "read",
			func(string) dispatch.Dispatcher { return d })
		if err == nil {
			t.Fatal("want the pipe error")
		}
	})

	t.Run("a closed stdout surfaces the body write error", func(t *testing.T) {
		d := staticDispatcher{res: &dispatch.ShapedResponse{Body: []byte(`{"ok":true}`)}}
		var errOut bytes.Buffer
		err := dispatchToWriterWithFactory(context.Background(), "default", failWriter{}, &errOut, inv, "read",
			func(string) dispatch.Dispatcher { return d })
		if err == nil {
			t.Fatal("want the pipe error")
		}
	})
}

// Every meta-tool command validates --format and --max-items before it builds
// an invocation, so a typo is a usage error rather than a dispatch blob.
func TestMetaToolCmdsRejectBadFlagsBeforeDispatch(t *testing.T) {
	cases := []struct {
		name string
		new  func() *cobra.Command
		args []string
	}{
		{"read bad format", newReadCmd, []string{"gmail.users.labels.list", "--format", "yaml"}},
		{"read bad max-items", newReadCmd, []string{"gmail.users.labels.list", "--max-items", "lots"}},
		{"write bad format", newWriteCmd, []string{"gmail.users.messages.send", "--format", "yaml"}},
		{"write bad max-items", newWriteCmd, []string{"gmail.users.messages.send", "--max-items", "lots"}},
		{"destructive bad format", newDestructiveCmd, []string{"gmail.users.messages.delete", "--format", "yaml"}},
		{"destructive bad max-items", newDestructiveCmd, []string{"gmail.users.messages.delete", "--max-items", "lots"}},
		{"code bad format", newCodeCmd, []string{"gum_print(1)", "--format", "yaml"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.new()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err == nil {
				t.Fatal("want a usage error before dispatch")
			}
		})
	}
}

// dispatchAndRender renders table/csv/markdown/value from the structured tree,
// so a dispatch failure, a non-JSON body and a render failure each have their
// own exit.
func TestDispatchAndRenderArms(t *testing.T) {
	orig := newMetaToolDispatcher
	t.Cleanup(func() { newMetaToolDispatcher = orig })

	t.Run("dispatch failure is rendered to stderr", func(t *testing.T) {
		newMetaToolDispatcher = func(string) dispatch.Dispatcher {
			return errDispatcher{err: errors.New("upstream down")}
		}
		cmd := &cobra.Command{Use: "read"}
		cmd.SetContext(context.Background())
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		err := dispatchAndRender(cmd, &dispatch.Invocation{OpID: "x"}, "read", "table")
		if err == nil {
			t.Fatal("want the dispatch error")
		}
		if out.Len() != 0 {
			t.Fatalf("stdout must stay clean for a pipeline, got %q", out.String())
		}
		_ = errOut
	})

	t.Run("a non-JSON body cannot be tabulated", func(t *testing.T) {
		newMetaToolDispatcher = func(string) dispatch.Dispatcher {
			return staticDispatcher{res: &dispatch.ShapedResponse{Body: []byte("not json at all")}}
		}
		cmd := &cobra.Command{Use: "read"}
		cmd.SetContext(context.Background())
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		err := dispatchAndRender(cmd, &dispatch.Invocation{OpID: "x"}, "read", "table")
		if err == nil || !strings.Contains(err.Error(), "not JSON") {
			t.Fatalf("want the not-JSON render error, got %v", err)
		}
	})

	t.Run("a closed stdout surfaces the render error", func(t *testing.T) {
		newMetaToolDispatcher = func(string) dispatch.Dispatcher {
			return staticDispatcher{res: &dispatch.ShapedResponse{
				StructuredContent: map[string]any{"a": 1},
			}}
		}
		cmd := &cobra.Command{Use: "read"}
		cmd.SetContext(context.Background())
		cmd.SetOut(failWriter{})
		cmd.SetErr(&bytes.Buffer{})
		if err := dispatchAndRender(cmd, &dispatch.Invocation{OpID: "x"}, "read", "table"); err == nil {
			t.Fatal("want the pipe error")
		}
	})
}
