package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// noticeDispatcher returns a shaped body plus the dropped-path report the kernel
// attaches when an expression profile removed fields (gum-bpx0).
type noticeDispatcher struct {
	body    string
	dropped []string
	full    string
}

func (d noticeDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return &dispatch.ShapedResponse{
		Body:           []byte(d.body),
		Format:         "json",
		DroppedPaths:   d.dropped,
		FullResultPath: d.full,
	}, nil
}

// TestMetaToolNamesDroppedFieldsOnStderr closes the gum-bpx0 requirement at the
// CLI seam: a profile whitelist that removes a field the op advertises must not
// be silent. The notice names the paths and goes to stderr, so stdout stays a
// clean machine-readable body for a pipeline.
func TestMetaToolNamesDroppedFieldsOnStderr(t *testing.T) {
	const body = `{"results":[{"text":"a"}]}`
	orig := newMetaToolDispatcher
	t.Cleanup(func() { newMetaToolDispatcher = orig })
	newMetaToolDispatcher = func(string) dispatch.Dispatcher {
		return noticeDispatcher{
			body:    body,
			dropped: []string{"results.keywordMetrics.monthlySearchVolumes"},
			full:    "/tmp/gum/tee/op/abc.json.gz",
		}
	}

	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"read", "some.op"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.TrimSpace(out.String()) != body {
		t.Errorf("stdout = %q; want the shaped body alone", out.String())
	}
	notice := errOut.String()
	for _, want := range []string{
		"results.keywordMetrics.monthlySearchVolumes",
		"--format raw",
		"/tmp/gum/tee/op/abc.json.gz",
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("stderr missing %q; got:\n%s", want, notice)
		}
	}
}

// TestMetaToolStaysSilentWhenNothingDropped: a lossless response must not print
// a notice, or every clean call trains the reader to ignore the line.
func TestMetaToolStaysSilentWhenNothingDropped(t *testing.T) {
	orig := newMetaToolDispatcher
	t.Cleanup(func() { newMetaToolDispatcher = orig })
	newMetaToolDispatcher = func(string) dispatch.Dispatcher {
		return noticeDispatcher{body: `{"results":[]}`}
	}

	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"read", "some.op"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q; want empty when the profile dropped nothing", errOut.String())
	}
}

// TestStructuredRenderSkipsDroppedFieldNotice: --output=table renders from
// StructuredContent, which carries the pre-shaping body, so the fields the
// profile removed from the text body are still on screen. Printing the notice
// there would tell the caller data is missing that they can see.
func TestStructuredRenderSkipsDroppedFieldNotice(t *testing.T) {
	orig := newMetaToolDispatcher
	t.Cleanup(func() { newMetaToolDispatcher = orig })
	newMetaToolDispatcher = func(string) dispatch.Dispatcher {
		return noticeDispatcher{
			body:    `{"rows":[{"clicks":7}]}`,
			dropped: []string{"rows.page"},
		}
	}

	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"read", "some.op", "--output=table"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(errOut.String(), "rows.page") {
		t.Errorf("structured render printed a dropped-field notice; got:\n%s", errOut.String())
	}
}

// TestCallNamesDroppedFieldsOnStderr covers the same seam on `gum call`, the
// path an agent uses directly.
func TestCallNamesDroppedFieldsOnStderr(t *testing.T) {
	const body = `{"rows":[{"clicks":7}]}`
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher {
		return noticeDispatcher{body: body, dropped: []string{"rows.page", "responseAggregationType"}}
	}

	rawArgs := []string{"call", "searchconsole.searchanalytics.query", "--risk=read",
		"--site-url=sc-domain:example.com",
		"--start-date=2026-05-01",
		"--end-date=2026-05-20",
	}
	root := newRootCmd()
	registerDynamicCallFlags(root, rawArgs)
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(rawArgs)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.TrimSpace(out.String()) != body {
		t.Errorf("stdout = %q; want the shaped body alone", out.String())
	}
	got := errOut.String()
	if !strings.Contains(got, "rows.page") || !strings.Contains(got, "responseAggregationType") {
		t.Errorf("stderr missing the dropped paths; got:\n%s", got)
	}
}
