package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
	outprofile "github.com/ehmo/gum/internal/output/profile"
)

// TestParseMaxItems covers the --max-items grammar: absent, "all", a positive
// integer, and the values the flag must reject.
func TestParseMaxItems(t *testing.T) {
	cases := []struct {
		in      string
		want    outprofile.MaxItemsOverride
		wantErr bool
	}{
		{in: "", want: outprofile.MaxItemsOverride{}},
		{in: "   ", want: outprofile.MaxItemsOverride{}},
		{in: "all", want: outprofile.MaxItemsOverride{Mode: outprofile.MaxItemsUnlimited}},
		{in: "ALL", want: outprofile.MaxItemsOverride{Mode: outprofile.MaxItemsUnlimited}},
		{in: " all ", want: outprofile.MaxItemsOverride{Mode: outprofile.MaxItemsUnlimited}},
		{in: "1", want: outprofile.MaxItemsOverride{Mode: outprofile.MaxItemsLimit, Value: 1}},
		{in: "500", want: outprofile.MaxItemsOverride{Mode: outprofile.MaxItemsLimit, Value: 500}},
		{in: "0", wantErr: true},
		{in: "-3", wantErr: true},
		{in: "1.5", wantErr: true},
		{in: "lots", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseMaxItems(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseMaxItems(%q) err = nil; want an error", tc.in)
				}
				if !strings.Contains(err.Error(), "--max-items") {
					t.Errorf("err = %v; want it to name --max-items", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMaxItems(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseMaxItems(%q) = %+v; want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// TestMaxItemsFlagReachesInvocation checks every command that registers
// --max-items actually forwards it. A flag that parses but never reaches the
// kernel is the failure this pins.
func TestMaxItemsFlagReachesInvocation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want outprofile.MaxItemsOverride
	}{
		{"read all", []string{"read", "some.op", "--max-items", "all"}, outprofile.MaxItemsOverride{Mode: outprofile.MaxItemsUnlimited}},
		{"read number", []string{"read", "some.op", "--max-items", "250"}, outprofile.MaxItemsOverride{Mode: outprofile.MaxItemsLimit, Value: 250}},
		{"read default", []string{"read", "some.op"}, outprofile.MaxItemsOverride{}},
		{"write", []string{"write", "some.op", "--allow-write", "--max-items", "7"}, outprofile.MaxItemsOverride{Mode: outprofile.MaxItemsLimit, Value: 7}},
		{"destructive", []string{"destructive", "some.op", "--max-items", "all"}, outprofile.MaxItemsOverride{Mode: outprofile.MaxItemsUnlimited}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cap := &capturingDispatcher{}
			orig := newMetaToolDispatcher
			t.Cleanup(func() { newMetaToolDispatcher = orig })
			newMetaToolDispatcher = func(string) dispatch.Dispatcher { return cap }

			root := newRootCmd()
			var out, errOut bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&errOut)
			root.SetArgs(tc.args)
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if cap.inv == nil {
				t.Fatal("dispatcher was never called")
			}
			if cap.inv.MaxItems != tc.want {
				t.Errorf("Invocation.MaxItems = %+v; want %+v", cap.inv.MaxItems, tc.want)
			}
		})
	}
}

// TestCallMaxItemsFlagReachesInvocation covers the same seam on `gum call`.
func TestCallMaxItemsFlagReachesInvocation(t *testing.T) {
	cap := &capturingDispatcher{}
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher { return cap }

	rawArgs := []string{"call", "searchconsole.searchanalytics.query", "--risk=read",
		"--site-url=sc-domain:example.com",
		"--start-date=2026-05-01",
		"--end-date=2026-05-20",
		"--max-items=all",
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
	if cap.inv == nil {
		t.Fatal("dispatcher was never called")
	}
	if cap.inv.MaxItems.Mode != outprofile.MaxItemsUnlimited {
		t.Errorf("Invocation.MaxItems = %+v; want MaxItemsUnlimited", cap.inv.MaxItems)
	}
}

// TestMaxItemsFlagRejectsBadValueBeforeDispatch: an invalid --max-items must
// fail before any upstream request, not shape the response silently.
func TestMaxItemsFlagRejectsBadValueBeforeDispatch(t *testing.T) {
	cap := &capturingDispatcher{}
	orig := newMetaToolDispatcher
	t.Cleanup(func() { newMetaToolDispatcher = orig })
	newMetaToolDispatcher = func(string) dispatch.Dispatcher { return cap }

	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"read", "some.op", "--max-items", "none"})
	err := root.Execute()
	if err == nil {
		t.Fatal("Execute err = nil; want an error for --max-items none")
	}
	if cap.inv != nil {
		t.Error("dispatcher ran despite an invalid --max-items")
	}
}

// TestShapingNoticeLeadsWithOmittedResults closes the gum-pmbp notice criterion
// at the CLI seam: the stderr note has to state the omitted-result count and
// name results_omitted_count, not only the removed field.
func TestShapingNoticeLeadsWithOmittedResults(t *testing.T) {
	orig := newMetaToolDispatcher
	t.Cleanup(func() { newMetaToolDispatcher = orig })
	newMetaToolDispatcher = func(string) dispatch.Dispatcher {
		return collapseDispatcher{}
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
	for _, want := range []string{"143", "243", "results_omitted_count", "--max-items all", "results.closeVariants"} {
		if !strings.Contains(notice, want) {
			t.Errorf("stderr missing %q; got:\n%s", want, notice)
		}
	}
	if strings.Index(notice, "143") > strings.Index(notice, "closeVariants") {
		t.Errorf("notice leads with the removed field, not the omitted results:\n%s", notice)
	}
}

// collapseDispatcher reports both a truncated array and a removed field, the
// gum-pmbp repro shape.
type collapseDispatcher struct{}

func (collapseDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return &dispatch.ShapedResponse{
		Body:         []byte(`{"results":[],"results_omitted_count":143}`),
		Format:       "json",
		DroppedPaths: []string{"results.closeVariants"},
		CollapsedArrays: []outprofile.CollapsedArray{
			{Field: "results", CountKey: "results_omitted_count", Kept: 100, Omitted: 143},
		},
	}, nil
}
