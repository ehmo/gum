package mcp

// Red-team failing tests for gum-awf — Tier A convenience tool ABI verification.
//
// Spec anchor: spec.md §4.1 lines 343-368.
//
// These tests reference ConvenienceABI and ConvenienceToolABI which do NOT yet
// exist in the mcp package. They will fail to compile until the Green team adds
// those exports (suggested file: internal/mcp/tier_a_abi.go).
//
// Two top-level tests:
//   - TestTierAConvenienceABI    — per-row ABI contract for all 18 tools
//   - TestConvenienceHighStakesConfirmation — schema shape for 8 write-confirmation tools

import (
	"encoding/json"
	"testing"
)

// validFormats is the closed set of wire formats a convenience tool may declare.
var validFormats = map[string]bool{
	"toon":     true,
	"csv":      true,
	"json":     true,
	"markdown": true,
}

// schemaHasOptionalProp returns true iff raw declares `name` in its
// "properties" block AND `name` is NOT listed in the "required" array.
// Uses stdlib encoding/json only (no third-party JSON Schema library).
func schemaHasOptionalProp(raw json.RawMessage, name string) bool {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return false
	}
	if _, exists := schema.Properties[name]; !exists {
		return false
	}
	for _, r := range schema.Required {
		if r == name {
			return false // in required → not optional
		}
	}
	return true
}

// TestTierAConvenienceABI verifies the ABI binding contract for every one of
// the 18 Tier A convenience tools (spec.md §4.1 lines 343-368).
//
// This test MUST fail to compile until the Green team adds:
//
//	type ConvenienceABI struct { ... }
//	func ConvenienceToolABI(name string) *ConvenienceABI
func TestTierAConvenienceABI(t *testing.T) {
	t.Helper()

	// -------------------------------------------------------------------------
	// Per-tool checks
	// -------------------------------------------------------------------------
	for _, n := range tierAConvenienceToolNamesList {
		n := n
		t.Run(n, func(t *testing.T) {
			// 1. ConvenienceToolABI must return a non-nil binding.
			abi := ConvenienceToolABI(n) // compile error until Green team adds this
			if abi == nil {
				t.Fatalf("ConvenienceToolABI(%q) returned nil; want a valid *ConvenienceABI", n)
			}

			// 2. OpID must be non-empty and consistent with convenienceOpRouting.
			if abi.OpID == "" {
				t.Errorf("abi.OpID is empty for tool %q", n)
			}
			if expected, ok := convenienceOpRouting[n]; ok {
				if abi.OpID != expected {
					t.Errorf("abi.OpID = %q; want %q (from convenienceOpRouting)", abi.OpID, expected)
				}
			} else {
				t.Errorf("tool %q not found in convenienceOpRouting; maps must be consistent", n)
			}

			// 3. VariantRule must be "default" OR a non-empty string that starts
			//    with the tool's service prefix.
			//    flights_search MUST be "flights.v1.plugin.search" (spec line 366).
			if abi.VariantRule == "" {
				t.Errorf("abi.VariantRule is empty for tool %q; want \"default\" or a fixed variant_id", n)
			}
			if n == "flights_search" {
				if abi.VariantRule != "flights.v1.plugin.search" {
					t.Errorf("flights_search VariantRule = %q; want \"flights.v1.plugin.search\" (spec §4.1 line 366)", abi.VariantRule)
				}
			} else {
				// All non-flights tools must use "default".
				if abi.VariantRule != "default" {
					// Fallback: allow a fixed variant only if it shares the service prefix.
					// Derive prefix from tool name: everything before the first underscore.
					prefix := n
					for i, c := range n {
						if c == '_' {
							prefix = n[:i]
							break
						}
					}
					if len(abi.VariantRule) == 0 {
						t.Errorf("tool %q: VariantRule is empty; want \"default\"", n)
					} else if abi.VariantRule[:len(prefix)] != prefix {
						t.Errorf("tool %q: VariantRule = %q does not start with service prefix %q and is not \"default\"",
							n, abi.VariantRule, prefix)
					}
				}
			}

			// 4. OutputProfile must be non-empty.
			if abi.OutputProfile == "" {
				t.Errorf("abi.OutputProfile is empty for tool %q; spec §4.1 requires a non-empty output profile", n)
			}

			// 5. Formats must be non-empty and every element must be in the
			//    closed set {"toon","csv","json","markdown"}.
			if len(abi.Formats) == 0 {
				t.Errorf("abi.Formats is empty for tool %q; want at least one valid wire format", n)
			}
			for _, f := range abi.Formats {
				if !validFormats[f] {
					t.Errorf("tool %q: abi.Formats contains %q which is not in {toon,csv,json,markdown}", n, f)
				}
			}

			// 6. ConfirmationPassthrough must be consistent with isWriteConfirmationTool.
			if abi.ConfirmationPassthrough != isWriteConfirmationTool(n) {
				t.Errorf("tool %q: abi.ConfirmationPassthrough=%v but isWriteConfirmationTool=%v; maps must agree",
					n, abi.ConfirmationPassthrough, isWriteConfirmationTool(n))
			}
		})
	}

	// -------------------------------------------------------------------------
	// Loop-independent aggregate assertions
	// -------------------------------------------------------------------------

	// Exactly 8 tools must have ConfirmationPassthrough=true (spec §4.1).
	confirmCount := 0
	for _, n := range tierAConvenienceToolNamesList {
		abi := ConvenienceToolABI(n)
		if abi != nil && abi.ConfirmationPassthrough {
			confirmCount++
		}
	}
	if confirmCount != 8 {
		t.Errorf("exactly 8 convenience tools must have ConfirmationPassthrough=true (spec §4.1); got %d", confirmCount)
	}

	// Exactly 1 tool must have a fixed variant (VariantRule != "default"),
	// and it MUST be flights_search with value "flights.v1.plugin.search".
	fixedCount := 0
	for _, n := range tierAConvenienceToolNamesList {
		abi := ConvenienceToolABI(n)
		if abi != nil && abi.VariantRule != "default" {
			fixedCount++
			if n != "flights_search" {
				t.Errorf("tool %q has a fixed variant %q; only flights_search is allowed a fixed variant (spec §4.1 line 366)", n, abi.VariantRule)
			}
		}
	}
	if fixedCount != 1 {
		t.Errorf("exactly 1 tool must have a fixed variant (VariantRule != \"default\"); got %d", fixedCount)
	}

	// Confirm the one fixed-variant tool is flights_search.
	flightsABI := ConvenienceToolABI("flights_search")
	if flightsABI != nil && flightsABI.VariantRule != "flights.v1.plugin.search" {
		t.Errorf("flights_search VariantRule = %q; want \"flights.v1.plugin.search\" (spec §4.1 line 366)", flightsABI.VariantRule)
	}
}

// TestConvenienceHighStakesConfirmation verifies that every write-confirmation
// tool (confirmation_passthrough=yes in spec §4.1) satisfies three invariants:
//
//  1. ConvenienceToolABI(n).ConfirmationPassthrough == true
//  2. The input schema declares both "confirmed" and "confirmation_token" as properties.
//  3. Neither "confirmed" nor "confirmation_token" appears in the schema's
//     "required" array (they are optional per spec §6.1 confirmation token semantics).
func TestConvenienceHighStakesConfirmation(t *testing.T) {
	// The 8 write-confirmation tools from spec §4.1 (hard-coded here for clarity;
	// we cross-check against writeConfirmationToolSet below for DRYness).
	expectedWriteConfirmTools := []string{
		"gmail_send",
		"gmail_create_draft",
		"drive_share",
		"calendar_create_event",
		"calendar_update_event",
		"docs_create",
		"sheets_write",
		"tasks_create",
	}

	// Cross-check: every tool in expectedWriteConfirmTools must appear in
	// writeConfirmationToolSet (so we're not testing phantom tools).
	for _, n := range expectedWriteConfirmTools {
		if !writeConfirmationToolSet[n] {
			t.Errorf("tool %q is in the spec §4.1 confirmation list but NOT in writeConfirmationToolSet; "+
				"the Green team must add it", n)
		}
	}

	// Cross-check: every tool in writeConfirmationToolSet must appear in our
	// expected list (catches undocumented additions).
	expectedSet := make(map[string]bool, len(expectedWriteConfirmTools))
	for _, n := range expectedWriteConfirmTools {
		expectedSet[n] = true
	}
	for n := range writeConfirmationToolSet {
		if !expectedSet[n] {
			t.Errorf("writeConfirmationToolSet contains %q which is NOT in the spec §4.1 table; "+
				"remove it or update the spec", n)
		}
	}

	// Per-tool assertions.
	for _, n := range expectedWriteConfirmTools {
		n := n
		t.Run(n, func(t *testing.T) {
			// 1. ABI binding must have ConfirmationPassthrough=true.
			abi := ConvenienceToolABI(n) // compile error until Green team adds this
			if abi == nil {
				t.Fatalf("ConvenienceToolABI(%q) returned nil", n)
			}
			if !abi.ConfirmationPassthrough {
				t.Errorf("abi.ConfirmationPassthrough=false for write tool %q; want true (spec §4.1)", n)
			}

			// 2 & 3. Input schema must declare confirmed and confirmation_token as
			//        optional (present in properties, absent from required).
			raw := convenienceToolSchema(n)

			if !schemaHasOptionalProp(raw, "confirmed") {
				t.Errorf("tool %q: schema is missing optional property \"confirmed\" "+
					"(spec §4.1 inputSchema rule / §6.1 confirmation token semantics)", n)
			}
			if !schemaHasOptionalProp(raw, "confirmation_token") {
				t.Errorf("tool %q: schema is missing optional property \"confirmation_token\" "+
					"(spec §4.1 inputSchema rule / §6.1 confirmation token semantics)", n)
			}
		})
	}
}
