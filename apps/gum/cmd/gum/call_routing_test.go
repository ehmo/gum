package main

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// bodyDispatcher returns a fixed JSON body so a test can exercise the CLI render
// path (table/csv/...) end-to-end through Execute.
type bodyDispatcher struct{ body string }

func (b bodyDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return &dispatch.ShapedResponse{Body: []byte(b.body), Format: "json"}, nil
}

// capturingDispatcher records the Invocation it receives so a test can assert
// how the CLI routed flat args into path/query/body.
type capturingDispatcher struct{ inv *dispatch.Invocation }

func (c *capturingDispatcher) Dispatch(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	c.inv = inv
	return &dispatch.ShapedResponse{Body: []byte(`{}`), Format: "json"}, nil
}

// TestCallFlatFieldsRouteToInvocation is the end-to-end routing test: a flat
// `gum call` with positional key=value fields must produce a dispatch.Invocation
// whose path field stays top-level, whose body fields are assembled into
// args["body"] with the right types, and whose array field is a slice — proving
// the RequestField-driven assembly is wired through the real command path, not
// just unit-tested in isolation.
func TestCallFlatFieldsRouteToInvocation(t *testing.T) {
	cap := &capturingDispatcher{}
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher { return cap }

	cmd := newCallCmd()
	cmd.PersistentFlags().String("profile", "", "")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"searchconsole.searchanalytics.query", "--risk=read",
		"siteUrl=sc-domain:turek.co",
		"startDate=2026-05-01", "endDate=2026-05-20",
		"dimensions=query", "dimensions=page",
		"rowLimit=5",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	inv := cap.inv
	if inv == nil {
		t.Fatal("dispatcher was not called")
	}
	// Path param stays top-level (the adapter substitutes it into the URL).
	if inv.Args["siteUrl"] != "sc-domain:turek.co" {
		t.Errorf("siteUrl top-level arg = %v, want sc-domain:turek.co", inv.Args["siteUrl"])
	}
	// Body fields assembled into args["body"] with correct types.
	body, ok := inv.Args["body"].(map[string]any)
	if !ok {
		t.Fatalf("body not assembled into a map: %#v", inv.Args["body"])
	}
	if body["startDate"] != "2026-05-01" || body["endDate"] != "2026-05-20" {
		t.Errorf("body dates wrong: %#v", body)
	}
	if body["rowLimit"] != int64(5) {
		t.Errorf("rowLimit = %#v, want int64(5)", body["rowLimit"])
	}
	if !reflect.DeepEqual(body["dimensions"], []any{"query", "page"}) {
		t.Errorf("dimensions = %#v, want [query page]", body["dimensions"])
	}
	// Body fields must NOT linger at the top level.
	for _, k := range []string{"startDate", "endDate", "dimensions", "rowLimit"} {
		if _, present := inv.Args[k]; present {
			t.Errorf("body field %q leaked to top-level args", k)
		}
	}
}

// TestCallSingleArrayValueWrappedAndNormalized pins two fixes at once: a single
// occurrence of an array field is wrapped into a slice (not left a bare scalar),
// and an enum value is normalized to the API's canonical case. So
// `dimensions=searchAPPEARANCE` (single, wrong case) must reach the body as
// ["searchAppearance"], not "searchAPPEARANCE".
func TestCallSingleArrayValueWrappedAndNormalized(t *testing.T) {
	cap := &capturingDispatcher{}
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher { return cap }

	cmd := newCallCmd()
	cmd.PersistentFlags().String("profile", "", "")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"searchconsole.searchanalytics.query", "--risk=read",
		"siteUrl=x", "startDate=2026-05-01", "endDate=2026-05-02",
		"dimensions=searchAPPEARANCE",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body, ok := cap.inv.Args["body"].(map[string]any)
	if !ok {
		t.Fatalf("body not assembled: %#v", cap.inv.Args["body"])
	}
	if !reflect.DeepEqual(body["dimensions"], []any{"searchAppearance"}) {
		t.Errorf("dimensions = %#v, want [searchAppearance] (wrapped + canonical case)", body["dimensions"])
	}
}

// TestCallHostControlFlagsRemap pins finding #2: --fields and --page-token map
// to the canonical Google query parameters (fields, pageToken), not the dead
// __-prefixed args that the upstream API silently ignored.
func TestCallHostControlFlagsRemap(t *testing.T) {
	cap := &capturingDispatcher{}
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher { return cap }

	cmd := newCallCmd()
	cmd.PersistentFlags().String("profile", "", "")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"searchconsole.searchanalytics.query", "--risk=read",
		"siteUrl=x", "startDate=2026-05-01", "endDate=2026-05-02",
		"--fields=rows.keys", "--page-token=tok123",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if cap.inv.Args["fields"] != "rows.keys" {
		t.Errorf("fields = %v, want rows.keys", cap.inv.Args["fields"])
	}
	if cap.inv.Args["pageToken"] != "tok123" {
		t.Errorf("pageToken = %v, want tok123", cap.inv.Args["pageToken"])
	}
	for _, dead := range []string{"__fields", "__page_token", "__page_size"} {
		if _, present := cap.inv.Args[dead]; present {
			t.Errorf("dead host-control arg %q still present", dead)
		}
	}
}

// TestCallTableOutputEndToEnd covers the cliRender branch of gum call's RunE
// (uncovered before): --output=table dispatches under the hood and renders the
// structured result as a table through the real command path.
func TestCallTableOutputEndToEnd(t *testing.T) {
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher {
		return bodyDispatcher{body: `{"rows":[{"clicks":5,"page":"/x"}]}`}
	}

	cmd := newCallCmd()
	cmd.PersistentFlags().String("profile", "", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"searchconsole.searchanalytics.query", "--risk=read",
		"siteUrl=x", "startDate=2026-05-01", "endDate=2026-05-02",
		"--output=table",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "clicks") || !strings.Contains(got, "page") {
		t.Errorf("table output missing expected headers, got:\n%s", got)
	}
}

// TestCallCSVOutputEndToEnd covers the --output=csv render path through Execute.
func TestCallCSVOutputEndToEnd(t *testing.T) {
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher {
		return bodyDispatcher{body: `{"rows":[{"clicks":5,"page":"/x"}]}`}
	}

	cmd := newCallCmd()
	cmd.PersistentFlags().String("profile", "", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"searchconsole.searchanalytics.query", "--risk=read",
		"siteUrl=x", "startDate=2026-05-01", "endDate=2026-05-02",
		"--output=csv",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "clicks") || !strings.Contains(got, ",") {
		t.Errorf("csv output missing expected header/row, got:\n%s", got)
	}
}

// TestCallEnumRejectionEndToEnd pins that a bad enum value is rejected by the
// real command before any dispatch.
func TestCallEnumRejectionEndToEnd(t *testing.T) {
	cap := &capturingDispatcher{}
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher { return cap }

	cmd := newCallCmd()
	cmd.PersistentFlags().String("profile", "", "")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"searchconsole.searchanalytics.query", "--risk=read",
		"siteUrl=x", "startDate=2026-05-01", "endDate=2026-05-02",
		"dimensions=bogus",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected enum rejection error")
	}
	if cap.inv != nil {
		t.Error("dispatcher should not be called when enum validation fails")
	}
}
