package embedded_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// TestCatalogIntegrity is the v0.1 CI integrity gate proving the embedded
// catalog.json bytes match the committed catalog.json.sha256 digest. Any
// silent corruption — disk bitrot, accidental edit, or unauthorized
// modification — that desynchronizes the two files fails this test.
//
// Spec / catalog-abi anchor: build-time/runtime integrity per §14 packaging.
//
// File format (sha256sum-canonical):
//
//	<64 lowercase hex chars><two spaces>catalog.json<newline>
func TestCatalogIntegrity(t *testing.T) {
	if len(embedded.CatalogJSON) == 0 {
		t.Skip("embedded.CatalogJSON is empty (build without embedded catalog); integrity check is vacuous")
	}

	rawChecksum := strings.TrimSpace(string(embedded.CatalogJSONSHA256))
	if rawChecksum == "" {
		t.Fatal("embedded.CatalogJSONSHA256 is empty — Green must commit catalog.json.sha256")
	}

	// Accept either bare hex or sha256sum form. Extract first whitespace-delimited token.
	committedHex := strings.Fields(rawChecksum)[0]
	if len(committedHex) != 64 {
		t.Fatalf("committed checksum first token must be 64 hex chars, got %d: %q", len(committedHex), committedHex)
	}
	if _, err := hex.DecodeString(committedHex); err != nil {
		t.Fatalf("committed checksum is not valid hex: %v", err)
	}

	sum := sha256.Sum256(embedded.CatalogJSON)
	computedHex := hex.EncodeToString(sum[:])

	if !strings.EqualFold(computedHex, committedHex) {
		t.Fatalf("catalog.json integrity check failed:\n  committed (sha256): %s\n  computed (sha256):  %s\n  Run `cd apps/gum && go run ./cmd/gen-catalog` to regenerate both files in lockstep, OR `sha256sum internal/embedded/catalog.json > internal/embedded/catalog.json.sha256` if you intentionally edited catalog.json.",
			committedHex, computedHex)
	}
}

// TestCatalogChecksumFileFormat enforces the sha256sum-canonical format so a
// downstream `sha256sum -c` would succeed against the committed file.
func TestCatalogChecksumFileFormat(t *testing.T) {
	if len(embedded.CatalogJSONSHA256) == 0 {
		t.Fatal("embedded.CatalogJSONSHA256 is empty — Green must commit catalog.json.sha256")
	}
	s := string(embedded.CatalogJSONSHA256)

	// MUST end with exactly one trailing newline (sha256sum convention).
	if !strings.HasSuffix(s, "\n") {
		t.Errorf("catalog.json.sha256 must end with a trailing newline (sha256sum canonical form)")
	}

	// Single line content: "<hex>  catalog.json".
	line := strings.TrimRight(s, "\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("catalog.json.sha256 must contain exactly one line, got %d lines", strings.Count(line, "\n")+1)
	}

	parts := strings.SplitN(line, "  ", 2)
	if len(parts) != 2 {
		t.Fatalf("catalog.json.sha256 line must be `<hex><two-spaces>catalog.json`, got %q", line)
	}

	if len(parts[0]) != 64 {
		t.Errorf("first column must be 64 hex chars, got %d: %q", len(parts[0]), parts[0])
	}

	if parts[1] != "catalog.json" {
		t.Errorf("second column must be the literal filename `catalog.json`, got %q", parts[1])
	}
}
