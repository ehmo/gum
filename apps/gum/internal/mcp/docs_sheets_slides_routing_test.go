// gum-45d acceptance: docs/sheets/slides convenience tool routing.
//
// Spec §4.1 lines 359-363. The convenience handlers (one per row in
// convenienceABITable) must:
//
//  1. Resolve to the right catalog op_id so dispatch.resolveVariant finds
//     a variant — the missing-op smoke test fails with OP_NOT_FOUND before
//     gum-45d ships these catalog entries.
//  2. Forward the spec's required args verbatim so the typed-rest-sdk
//     adapter can stamp the URL path placeholders.
//
// We don't exercise the live REST executor (that needs BYO OAuth + the
// network — that part of the spec acceptance lives in gum-45d's smoke-test
// gate). What we DO pin here is the wire contract between the convenience
// handler and the kernel: a captured dispatcher records the OpID + Args the
// kernel saw, and we assert against the spec's op_id row.

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/dispatch"
)

// workspaceCapturingDispatcher records each (op_id, args) it receives so the
// routing test can assert that the convenience handler resolved the catalog
// op the spec mandates.
type workspaceCapturingDispatcher struct {
	gotOpID string
	gotArgs map[string]any
}

func (d *workspaceCapturingDispatcher) Dispatch(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	d.gotOpID = inv.OpID
	d.gotArgs = inv.Args
	return &dispatch.ShapedResponse{Body: []byte(`{}`)}, nil
}

// TestDocsSheetsSlidesConvenienceRouting drives every gum-45d convenience
// handler with the spec's required-arg payload and pins the captured op_id
// to the spec §4.1 row. Forwards-only assertion on args — the convenience
// layer does not transform field names; the typed-rest-sdk adapter does.
func TestDocsSheetsSlidesConvenienceRouting(t *testing.T) {
	cases := []struct {
		tool        string
		wantOpID    string
		args        string
		requiredArg string
	}{
		{
			tool:        "docs_get",
			wantOpID:    "docs.documents.get",
			args:        `{"documentId":"DOC123"}`,
			requiredArg: "documentId",
		},
		{
			// Write tools must include confirmed:true to clear the REQUIRES_
			// CONFIRMATION gate (spec §4.1 / §6.1); without it the convenience
			// handler short-circuits before Dispatch is reached.
			tool:        "docs_create",
			wantOpID:    "docs.documents.create",
			args:        `{"document":{"title":"Draft"},"confirmed":true}`,
			requiredArg: "document",
		},
		{
			tool:        "sheets_read",
			wantOpID:    "sheets.spreadsheets.values.get",
			args:        `{"spreadsheetId":"SS1","range":"Sheet1!A1:C5"}`,
			requiredArg: "spreadsheetId",
		},
		{
			tool:        "sheets_write",
			wantOpID:    "sheets.spreadsheets.values.update",
			args:        `{"spreadsheetId":"SS1","range":"Sheet1!A1","values":[["x"]],"confirmed":true}`,
			requiredArg: "values",
		},
		{
			tool:        "slides_get",
			wantOpID:    "slides.presentations.get",
			args:        `{"presentationId":"PR1"}`,
			requiredArg: "presentationId",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.tool, func(t *testing.T) {
			disp := &workspaceCapturingDispatcher{}
			srv := NewServer(disp)
			req := &sdkmcp.CallToolRequest{
				Params: &sdkmcp.CallToolParamsRaw{
					Name:      tc.tool,
					Arguments: json.RawMessage(tc.args),
				},
			}
			handler := srv.makeConvenienceHandler(tc.tool)
			if _, err := handler(context.Background(), req); err != nil {
				t.Fatalf("handler returned err=%v; want nil", err)
			}
			if disp.gotOpID != tc.wantOpID {
				t.Errorf("dispatcher saw op_id=%q; want %q (spec §4.1)", disp.gotOpID, tc.wantOpID)
			}
			if _, ok := disp.gotArgs[tc.requiredArg]; !ok {
				t.Errorf("Invocation.Args missing required arg %q for tool %q (forward-verbatim contract)",
					tc.requiredArg, tc.tool)
			}
		})
	}
}

// TestDocsSheetsSlidesCatalogEntriesResolve uses the embedded catalog (the
// dispatcher reaches it via defaultCatalog) and asserts each of the five new
// op_ids resolves to a discovery-rest variant pointing at the typed-rest-sdk
// adapter. This is the gate against the "missing catalog entry" failure
// mode — without it the convenience handler would surface OP_NOT_FOUND when
// invoked at runtime.
func TestDocsSheetsSlidesCatalogEntriesResolve(t *testing.T) {
	cat := defaultCatalog()
	if cat == nil {
		t.Fatal("defaultCatalog() returned nil; embedded catalog must load")
	}
	expected := map[string]string{
		"docs.documents.get":                "docs.v1.rest.documents.get",
		"docs.documents.create":             "docs.v1.rest.documents.create",
		"sheets.spreadsheets.values.get":    "sheets.v4.rest.spreadsheets.values.get",
		"sheets.spreadsheets.values.update": "sheets.v4.rest.spreadsheets.values.update",
		"slides.presentations.get":          "slides.v1.rest.presentations.get",
	}
	byID := map[string]bool{}
	for _, op := range cat.Ops {
		if want, ok := expected[op.OpID]; ok {
			byID[op.OpID] = true
			if op.DefaultVariantID != want {
				t.Errorf("op %s default_variant_id=%q; want %q", op.OpID, op.DefaultVariantID, want)
			}
			if len(op.Variants) == 0 {
				t.Errorf("op %s: no variants", op.OpID)
				continue
			}
			v := op.Variants[0]
			if string(v.BackendKind) != "typed-rest-sdk" {
				t.Errorf("op %s: backend_kind=%q; want typed-rest-sdk", op.OpID, v.BackendKind)
			}
			if v.Binding == nil || v.Binding.AdapterKey != "rest.typed-rest-sdk" {
				t.Errorf("op %s: adapter_key wrong/missing", op.OpID)
			}
		}
	}
	for id := range expected {
		if !byID[id] {
			t.Errorf("op %q missing from embedded catalog; gum-45d wiring incomplete", id)
		}
	}
}
