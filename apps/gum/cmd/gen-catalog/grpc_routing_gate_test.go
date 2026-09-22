package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestGeneratedCatalogGateRejectsRoutingHeaders proves the three
// catalog-abi.md `routing_headers` build failures reach the generator gate
// (gum-dvn6 criterion 1). Every offline gen-catalog path calls
// validateGeneratedCatalog before it writes, so a violation cannot enter a
// snapshot through a side door.
func TestGeneratedCatalogGateRejectsRoutingHeaders(t *testing.T) {
	cases := []struct {
		fixture string
		wantErr error
	}{
		{"present.json", nil},
		{"omitted.json", nil},
		{"not-required.json", catalog.ErrGRPCRoutingHeaderNotRequired},
		{"not-found.json", catalog.ErrGRPCRoutingHeaderNotFound},
		{"duplicate.json", catalog.ErrGRPCRoutingHeaderDuplicate},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			path := filepath.Join("..", "..", "internal", "catalog", "testdata", "grpc-routing", tc.fixture)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			var cat catalog.Catalog
			if err := json.Unmarshal(raw, &cat); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}

			err = validateGeneratedCatalog(&cat)
			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("validateGeneratedCatalog = %v; want nil", err)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Fatalf("validateGeneratedCatalog = %v; want %v", err, tc.wantErr)
			}
		})
	}
}
