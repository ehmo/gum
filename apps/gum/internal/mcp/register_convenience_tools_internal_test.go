package mcp

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestRegisterConvenienceToolsEmptyRosterEarlyExits pins
// registerConvenienceTools' `len(rosterData) == 0 → return` guard
// (server.go:191-193). When the embedded roster is missing or empty,
// the registration MUST be a no-op rather than panicking or registering
// junk tools.
func TestRegisterConvenienceToolsEmptyRosterEarlyExits(t *testing.T) {
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	})
	// Sanity: server constructed without panic.
	before := len(s.ConvenienceToolNames())

	// Drive the early-exit arm with explicit nil/empty input.
	s.registerConvenienceTools(nil)
	s.registerConvenienceTools([]byte{})

	after := len(s.ConvenienceToolNames())
	if after != before {
		t.Errorf("empty roster added %d tools; want zero new registrations", after-before)
	}
}

// TestRegisterConvenienceToolsMalformedRosterReturns pins the
// `json.Unmarshal err → return` arm (server.go:195-197). When the embedded
// JSON is corrupt the registration MUST silently bail out — a malformed
// data/tier-a-roster.v1.json shouldn't abort server startup or panic;
// the meta-tools registered before this point are still valid.
func TestRegisterConvenienceToolsMalformedRosterReturns(t *testing.T) {
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	})
	before := len(s.ConvenienceToolNames())

	// Not valid JSON.
	s.registerConvenienceTools([]byte("{not json"))

	after := len(s.ConvenienceToolNames())
	if after != before {
		t.Errorf("malformed roster registered %d tools; want zero", after-before)
	}
}
