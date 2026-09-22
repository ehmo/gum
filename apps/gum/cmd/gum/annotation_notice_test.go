package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// gum-9l5c: `gum read` trimmed a merged keyword batch and told the caller to
// use --format raw for "the complete body". Raw bypasses the adapter
// annotator, so it returns closeVariants and no matchedInputs: the caller who
// takes the advice loses the one field that says which submitted keyword each
// result answers for. The CLI seam has to pass the surviving annotation paths
// into the notice.
func TestCLINoticeNamesTheAnnotationFieldsRawDrops(t *testing.T) {
	notice := runAnnotatedRead(t, []string{"results.matchedInputs", "unmatchedInputs"})

	want := "Use --format raw for the complete upstream body;" +
		" raw omits the gum-added fields results.matchedInputs, unmatchedInputs."
	if !strings.Contains(notice, want) {
		t.Errorf("stderr missing %q; got:\n%s", want, notice)
	}
}

// A response with no adapter annotation keeps the wording it always had.
func TestCLINoticeKeepsThePlainRawHint(t *testing.T) {
	notice := runAnnotatedRead(t, nil)

	if want := "Use --format raw for the complete body."; !strings.Contains(notice, want) {
		t.Errorf("stderr missing %q; got:\n%s", want, notice)
	}
}

func runAnnotatedRead(t *testing.T, annotations []string) string {
	t.Helper()

	orig := newMetaToolDispatcher
	t.Cleanup(func() { newMetaToolDispatcher = orig })
	newMetaToolDispatcher = func(string) dispatch.Dispatcher {
		return annotatedDispatcher{annotations: annotations}
	}

	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"read", "some.op"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return errOut.String()
}

// annotatedDispatcher returns a trimmed keyword-history body carrying
// matchedInputs, with closeVariants reported as dropped by the profile.
type annotatedDispatcher struct{ annotations []string }

func (d annotatedDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return &dispatch.ShapedResponse{
		Body: []byte(`{"results":[{"text":"akhal teke horse","matchedInputs":["akhal teke horse","akhal-teke horse"]}],` +
			`"unmatchedInputs":["sailing lessons"]}`),
		Format:          "json",
		DroppedPaths:    []string{"results.closeVariants"},
		AnnotationPaths: d.annotations,
	}, nil
}
