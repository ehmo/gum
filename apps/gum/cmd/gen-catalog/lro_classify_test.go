package main

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// syntheticDiscovery is a minimal Discovery document carrying the three
// response schemas the classifier has to tell apart: a longrunning Operation
// under each of its two known spellings, the list payload whose name also
// contains "Operation", and an ordinary resource.
const syntheticDiscovery = `{
  "schemas": {
    "Operation": {"properties": {"done": {"type": "boolean"}, "error": {"$ref": "Status"}, "name": {"type": "string"}}},
    "GoogleLongrunningOperation": {"properties": {"done": {"type": "boolean"}, "error": {"$ref": "Status"}}},
    "ListOperationsResponse": {"properties": {"operations": {"type": "array"}, "nextPageToken": {"type": "string"}}},
    "Message": {"properties": {"id": {"type": "string"}}}
  }
}`

func syntheticDiscoveryDoc(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(syntheticDiscovery), &doc); err != nil {
		t.Fatalf("unmarshal synthetic discovery: %v", err)
	}
	return doc
}

func methodWithResponse(ref string) map[string]any {
	if ref == "" {
		return map[string]any{"id": "svc.res.act"}
	}
	return map[string]any{"id": "svc.res.act", "response": map[string]any{"$ref": ref}}
}

func TestMethodReturnsLRO(t *testing.T) {
	doc := syntheticDiscoveryDoc(t)

	cases := []struct {
		name string
		ref  string
		want bool
	}{
		{"operation", "Operation", true},
		{"google_longrunning_operation", "GoogleLongrunningOperation", true},
		{"list_operations_response", "ListOperationsResponse", false},
		{"ordinary_resource", "Message", false},
		{"no_response", "", false},
		{"ref_not_in_document", "Absent", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := methodReturnsLRO(doc, methodWithResponse(tc.ref)); got != tc.want {
				t.Fatalf("methodReturnsLRO(%q) = %v; want %v", tc.ref, got, tc.want)
			}
		})
	}
}

func TestMethodReturnsLROWithoutSchemas(t *testing.T) {
	if methodReturnsLRO(map[string]any{}, methodWithResponse("Operation")) {
		t.Fatal("methodReturnsLRO() = true for a document with no schemas block")
	}
}

func TestStampLROReturnCoversRESTVariantsOnly(t *testing.T) {
	op := &catalog.Op{
		OpID: "svc.act",
		Variants: []catalog.Variant{
			{VariantID: "a", BackendKind: catalog.BackendKindTypedRestSDK},
			{VariantID: "b", BackendKind: catalog.BackendKindDiscoveryREST, Capabilities: []string{"json_response"}},
			{VariantID: "c", BackendKind: catalog.BackendKindRawHTTP},
			{VariantID: "d", BackendKind: catalog.BackendKindGRPCSDK},
			{VariantID: "e", BackendKind: catalog.BackendKindMCPPlugin},
		},
	}

	if got := stampLROReturn(op); got != 3 {
		t.Fatalf("stampLROReturn() = %d; want 3", got)
	}
	for _, v := range op.Variants {
		wantAtom := v.VariantID == "a" || v.VariantID == "b" || v.VariantID == "c"
		if got := slices.Contains(v.Capabilities, catalog.CapabilityLROReturn); got != wantAtom {
			t.Fatalf("variant %s carries lro_return = %v; want %v", v.VariantID, got, wantAtom)
		}
	}
	if len(op.Variants[1].Capabilities) != 2 {
		t.Fatalf("variant b capabilities = %v; want the declared atom kept alongside lro_return", op.Variants[1].Capabilities)
	}
}

func TestStampLROReturnIsIdempotent(t *testing.T) {
	op := &catalog.Op{
		OpID: "svc.act",
		Variants: []catalog.Variant{
			{VariantID: "a", BackendKind: catalog.BackendKindDiscoveryREST, Capabilities: []string{catalog.CapabilityLROReturn}},
		},
	}

	if got := stampLROReturn(op); got != 0 {
		t.Fatalf("stampLROReturn() = %d on an already-classified variant; want 0", got)
	}
	if got := len(op.Variants[0].Capabilities); got != 1 {
		t.Fatalf("capabilities = %v; want no duplicate atom", op.Variants[0].Capabilities)
	}
}
