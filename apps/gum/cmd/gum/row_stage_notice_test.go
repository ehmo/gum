package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestShapingNoticeNamesDedupedAndLimitedRows closes the gum-as84 CLI seam:
// dedupe and limit remove whole records and write no count into the body, so a
// silent notice reports a short result as a complete one.
func TestShapingNoticeNamesDedupedAndLimitedRows(t *testing.T) {
	orig := newMetaToolDispatcher
	t.Cleanup(func() { newMetaToolDispatcher = orig })
	newMetaToolDispatcher = func(string) dispatch.Dispatcher {
		return rowStageDispatcher{}
	}

	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"read", "some.op"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	notice := errOut.String()
	for _, want := range []string{"4", "duplicate", "7", "limit"} {
		if !strings.Contains(notice, want) {
			t.Errorf("stderr missing %q; got:\n%s", want, notice)
		}
	}
}

// rowStageDispatcher reports rows removed by stage 7 and by the profile limit,
// with nothing in the body to show for them.
type rowStageDispatcher struct{}

func (rowStageDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return &dispatch.ShapedResponse{
		Body:        []byte(`{"messages":[{"id":"a","occurrence_count":5}]}`),
		Format:      "json",
		DedupedRows: 4,
		LimitedRows: 7,
	}, nil
}
