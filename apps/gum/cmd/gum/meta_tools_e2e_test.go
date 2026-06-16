package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestReadCmdTableOutputEndToEnd is the end-to-end coverage for the
// dispatchAndRender cliFormatNeedsStructured branch (gum-rq2u #28). It injects
// bodyDispatcher via the newMetaToolDispatcher seam, runs
// `gum read some.op --output=table` through root.Execute(), and asserts the
// rendered table contains the expected column headers.
func TestReadCmdTableOutputEndToEnd(t *testing.T) {
	orig := newMetaToolDispatcher
	t.Cleanup(func() { newMetaToolDispatcher = orig })
	newMetaToolDispatcher = func(string) dispatch.Dispatcher {
		return bodyDispatcher{body: `{"rows":[{"clicks":7,"page":"/home"}]}`}
	}

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"read", "some.op", "--output=table"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "clicks") || !strings.Contains(got, "page") {
		t.Errorf("table output missing expected headers; got:\n%s", got)
	}
}

// TestReadCmdCSVOutputEndToEnd covers the --output=csv render path for the
// meta-tool (dispatchAndRender cliFormatNeedsStructured branch).
func TestReadCmdCSVOutputEndToEnd(t *testing.T) {
	orig := newMetaToolDispatcher
	t.Cleanup(func() { newMetaToolDispatcher = orig })
	newMetaToolDispatcher = func(string) dispatch.Dispatcher {
		return bodyDispatcher{body: `{"rows":[{"clicks":7,"page":"/home"}]}`}
	}

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"read", "some.op", "--output=csv"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "clicks") || !strings.Contains(got, ",") {
		t.Errorf("csv output missing expected header/comma; got:\n%s", got)
	}
}

// TestKebabFlagsRouteViaRootExecute is the end-to-end composition test for
// registerDynamicCallFlags + applyKebabFlags (gum-rq2u #30). It calls
// registerDynamicCallFlags on a real root, then runs root.Execute() with kebab
// flags, and asserts via a captured Invocation that the flags routed to the
// correct camelCase keys — proving the two-pass wiring through Execute(), not
// just the isolated applyKebabFlags unit test.
func TestKebabFlagsRouteViaRootExecute(t *testing.T) {
	cap := &capturingDispatcher{}
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher { return cap }

	const opID = "searchconsole.searchanalytics.query"
	rawArgs := []string{"call", opID, "--risk=read",
		"--site-url=sc-domain:turek.co",
		"--start-date=2026-05-01",
		"--end-date=2026-05-20",
		"--dimensions=query",
		"--row-limit=5",
	}

	root := newRootCmd()
	registerDynamicCallFlags(root, rawArgs)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(rawArgs)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	inv := cap.inv
	if inv == nil {
		t.Fatal("dispatcher was not called")
	}
	if inv.Args["siteUrl"] != "sc-domain:turek.co" {
		t.Errorf("siteUrl = %v, want sc-domain:turek.co", inv.Args["siteUrl"])
	}
	body, ok := inv.Args["body"].(map[string]any)
	if !ok {
		t.Fatalf("body not assembled into a map: %#v", inv.Args["body"])
	}
	if body["startDate"] != "2026-05-01" {
		t.Errorf("startDate = %v, want 2026-05-01", body["startDate"])
	}
	if body["endDate"] != "2026-05-20" {
		t.Errorf("endDate = %v, want 2026-05-20", body["endDate"])
	}
	if dims, _ := body["dimensions"].([]any); len(dims) == 0 || dims[0] != "query" {
		t.Errorf("dimensions = %#v, want [query]", body["dimensions"])
	}
	if body["rowLimit"] != int64(5) {
		t.Errorf("rowLimit = %#v, want int64(5)", body["rowLimit"])
	}
}
