// gum-6ne acceptance: drive convenience tool routing.
//
// Spec §4.1 — three Drive Tier A convenience tools:
//
//	drive_find      → drive.files.list           (read)
//	drive_get_file  → drive.files.get            (read)
//	drive_share     → drive.permissions.create   (write, requires confirmation)
//
// Mirrors docs_sheets_slides_routing_test.go: a capturing dispatcher records
// (op_id, args) for each convenience handler so we can assert the
// convenienceABITable → convenienceOpRouting wiring resolves the spec's
// op_ids and forwards required args verbatim. Live REST execution lives in
// the smoke-test gate; this test pins the kernel-side contract.

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestDriveConvenienceRouting drives every Drive Tier A convenience handler
// with the spec's required-arg payload and pins the captured op_id to the
// spec §4.1 row. Reuses workspaceCapturingDispatcher from
// docs_sheets_slides_routing_test.go.
func TestDriveConvenienceRouting(t *testing.T) {
	cases := []struct {
		tool        string
		wantOpID    string
		args        string
		requiredArg string
	}{
		{
			tool:        "drive_find",
			wantOpID:    "drive.files.list",
			args:        `{"query":"name contains 'budget'"}`,
			requiredArg: "query",
		},
		{
			tool:        "drive_get_file",
			wantOpID:    "drive.files.get",
			args:        `{"fileId":"FILE123"}`,
			requiredArg: "fileId",
		},
		{
			// Write tool: confirmed:true clears the §6.1 REQUIRES_CONFIRMATION
			// gate so Dispatch is reached.
			tool:        "drive_share",
			wantOpID:    "drive.permissions.create",
			args:        `{"fileId":"FILE123","role":"reader","type":"user","emailAddress":"x@example.com","confirmed":true}`,
			requiredArg: "fileId",
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

// TestDriveCatalogEntriesResolve uses the embedded catalog (the dispatcher
// reaches it via defaultCatalog) and asserts each of the three Drive op_ids
// resolves to a discovery-rest variant pointing at the typed-rest-sdk
// adapter — without it the convenience handler would surface OP_NOT_FOUND.
func TestDriveCatalogEntriesResolve(t *testing.T) {
	cat := defaultCatalog()
	if cat == nil {
		t.Fatal("defaultCatalog() returned nil; embedded catalog must load")
	}
	expected := map[string]string{
		"drive.files.list":         "drive.v3.rest.files.list",
		"drive.files.get":          "drive.v3.rest.files.get",
		"drive.permissions.create": "drive.v3.rest.permissions.create",
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
			t.Errorf("op %q missing from embedded catalog; gum-6ne wiring incomplete", id)
		}
	}
}
