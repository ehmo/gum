package catalog

import (
	"errors"
	"strings"
	"testing"
)

// TestRiskOverrideReasonRejectsDenylistedCharacters is the spec §5.4 denylist.
// Before this gate the field reached catalog.json, the audit log,
// gum.describe_op and `gum catalog list-overrides` unchecked, so an ANSI
// escape written into a plugin manifest rendered as terminal control output
// and a bidi override reordered the operator's view of the reason.
func TestRiskOverrideReasonRejectsDenylistedCharacters(t *testing.T) {
	cases := []struct {
		name   string
		reason string
	}{
		{"ansi escape", "read-only \x1b[31msearch\x1b[0m; no mutation"},
		{"bell", "read-only search\x07"},
		{"newline", "read-only search\nrisk: none"},
		{"delete", "read-only search\x7f"},
		{"zero width space", "read-only\u200bsearch"},
		{"zero width joiner", "read-only\u200dsearch"},
		{"left-to-right mark", "read-only\u200esearch"},
		{"line separator", "read-only\u2028search"},
		{"paragraph separator", "read-only\u2029search"},
		{"byte order mark", "\ufeffread-only search"},
		{"arabic letter mark", "read-only\u061csearch"},
		{"right-to-left override", "read-only\u202esearch"},
		{"left-to-right embedding", "read-only\u202asearch"},
		{"first strong isolate", "read-only\u2068search"},
		{"pop directional isolate", "read-only\u2069search"},
		{"less than", "POST but <b>read-only</b>"},
		{"greater than", "POST -> read-only"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRiskOverrideReason("flights.search.v1", tc.reason)
			if err == nil {
				t.Fatalf("ValidateRiskOverrideReason(%q)=nil; want ErrRiskOverrideReasonInvalid", tc.reason)
			}
			if !errors.Is(err, ErrRiskOverrideReasonInvalid) {
				t.Fatalf("ValidateRiskOverrideReason()=%v; want ErrRiskOverrideReasonInvalid", err)
			}
			if !strings.Contains(err.Error(), "flights.search.v1") {
				t.Fatalf("error %q does not name the variant", err)
			}
		})
	}
}

// TestRiskOverrideReasonAcceptsOrdinaryProse holds the denylist to the four
// classes §7 lists. Everything else is permitted, so a reason may carry
// non-ASCII letters, apostrophes, brackets and ampersands.
func TestRiskOverrideReasonAcceptsOrdinaryProse(t *testing.T) {
	cases := []string{
		"",
		"FlightsFrontendService.search uses POST but returns read-only search results; no state mutation.",
		"Uses POST for the query body (RFC 7231 §4.3.3) & returns nothing mutable.",
		"L'opération ne modifie aucun état: lecture seule.",
		"読み取り専用: 状態を変更しません。",
		strings.Repeat("x", MaxRiskOverrideReasonRunes),
		strings.Repeat("é", MaxRiskOverrideReasonRunes),
	}

	for _, reason := range cases {
		if err := ValidateRiskOverrideReason("flights.search.v1", reason); err != nil {
			t.Fatalf("ValidateRiskOverrideReason(%q)=%v; want nil", reason, err)
		}
	}
}

// TestRiskOverrideReasonRejectsOverlongValue pins the 200-codepoint bound.
// The bound is counted in codepoints, not bytes, so a 200-rune multibyte
// string passes while 201 ASCII characters do not. The audit-log schema
// declares maxLength 200 for the same field, so a longer value would be
// written and then fail validation downstream.
func TestRiskOverrideReasonRejectsOverlongValue(t *testing.T) {
	long := strings.Repeat("x", MaxRiskOverrideReasonRunes+1)
	err := ValidateRiskOverrideReason("flights.search.v1", long)
	if !errors.Is(err, ErrRiskOverrideReasonInvalid) {
		t.Fatalf("ValidateRiskOverrideReason(201 runes)=%v; want ErrRiskOverrideReasonInvalid", err)
	}
	if !strings.Contains(err.Error(), "201 codepoints") {
		t.Fatalf("error %q does not report the length", err)
	}

	// 200 multibyte runes is 400 bytes and must still pass.
	if err := ValidateRiskOverrideReason("flights.search.v1", strings.Repeat("é", MaxRiskOverrideReasonRunes)); err != nil {
		t.Fatalf("ValidateRiskOverrideReason(200 multibyte runes)=%v; want nil", err)
	}
}

// TestRiskOverrideRequiresReasonWhenSet is the §7 RISK_OVERRIDE_MISSING_REASON
// rule. An override downgrades the risk class the §12 gate reads, so an
// unexplained one weakens the confirmation prompt with no record of why.
func TestRiskOverrideRequiresReasonWhenSet(t *testing.T) {
	err := validateRiskOverride("flights.search.v1", true, "")
	if !errors.Is(err, ErrRiskOverrideMissingReason) {
		t.Fatalf("validateRiskOverride(override, no reason)=%v; want ErrRiskOverrideMissingReason", err)
	}

	if err := validateRiskOverride("flights.search.v1", false, ""); err != nil {
		t.Fatalf("validateRiskOverride(no override, no reason)=%v; want nil", err)
	}
	if err := validateRiskOverride("flights.search.v1", true, "POST query, read-only result."); err != nil {
		t.Fatalf("validateRiskOverride(override with reason)=%v; want nil", err)
	}
}

// TestOpValidateRejectsPoisonedRiskOverrideReason proves the gate is reached
// from the catalog load path, not only callable in isolation. Op.Validate is
// what cmd/gen-catalog runs over the generated catalog before it writes a
// snapshot.
func TestOpValidateRejectsPoisonedRiskOverrideReason(t *testing.T) {
	op := baseSnapshot().Ops[0]
	op.Variants[0].RiskOverride = true
	op.Variants[0].RiskOverrideReason = "read-only \x1b[31mPOST\x1b[0m"

	err := op.Validate()
	if !errors.Is(err, ErrRiskOverrideReasonInvalid) {
		t.Fatalf("Op.Validate(ansi reason)=%v; want ErrRiskOverrideReasonInvalid", err)
	}
	if !strings.Contains(err.Error(), op.OpID) {
		t.Fatalf("error %q does not name the op", err)
	}

	op.Variants[0].RiskOverrideReason = ""
	if err := op.Validate(); !errors.Is(err, ErrRiskOverrideMissingReason) {
		t.Fatalf("Op.Validate(override, no reason)=%v; want ErrRiskOverrideMissingReason", err)
	}

	op.Variants[0].RiskOverrideReason = "POST query, read-only result."
	if err := op.Validate(); err != nil {
		t.Fatalf("Op.Validate(clean reason)=%v; want nil", err)
	}
}

// TestMergePluginVariantsRefusesPoisonedRiskOverrideReason covers the runtime
// half. plugin-catalog.json is a profile file any local process can append to,
// and the merge assembles the session snapshot at boot without running
// Op.Validate, so the decode step is the only gate that sees a plugin-authored
// reason before the audit log and the terminal do.
func TestMergePluginVariantsRefusesPoisonedRiskOverrideReason(t *testing.T) {
	base := baseSnapshot()
	pc := pluginCatalogWith(pluginRow("acme", "do_thing", func(row map[string]any) {
		row["risk_override"] = true
		row["risk_override_reason"] = "read-only \x1b[2J\x1b[H"
	}))

	got, refused := MergePluginVariants(base, pc, map[string]bool{"acme": true})
	if len(refused) != 1 {
		t.Fatalf("refused=%d; want 1", len(refused))
	}
	if !errors.Is(refused[0], ErrPluginRowMalformed) {
		t.Fatalf("refused[0]=%v; want ErrPluginRowMalformed", refused[0])
	}
	if !strings.Contains(refused[0].Error(), "RISK_OVERRIDE_REASON_INVALID") {
		t.Fatalf("refused[0]=%q; want the RISK_OVERRIDE_REASON_INVALID code", refused[0])
	}
	if findOpByID(got, "plug.acme.do_thing") != nil {
		t.Fatal("poisoned plugin row reached the session snapshot")
	}
}

// TestMergePluginVariantsKeepsCleanRiskOverrideReason proves the new gate does
// not refuse the legitimate override the bundled Flights plugin declares.
func TestMergePluginVariantsKeepsCleanRiskOverrideReason(t *testing.T) {
	const reason = "FlightsFrontendService.search uses POST but returns read-only search results; no state mutation."

	base := baseSnapshot()
	pc := pluginCatalogWith(pluginRow("acme", "do_thing", func(row map[string]any) {
		row["risk_override"] = true
		row["risk_override_reason"] = reason
	}))

	got, refused := MergePluginVariants(base, pc, map[string]bool{"acme": true})
	if len(refused) != 0 {
		t.Fatalf("refused=%v; want none", refused)
	}
	op := findOpByID(got, "plug.acme.do_thing")
	if op == nil {
		t.Fatal("clean plugin row was dropped from the session snapshot")
	}
	if op.Variants[0].RiskOverrideReason != reason {
		t.Fatalf("risk_override_reason=%q; want %q", op.Variants[0].RiskOverrideReason, reason)
	}
}
