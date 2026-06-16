// Package profile provides the expression-profile DSL parser and response applier.
//
// The Phase 4 DSL is a minimal TOML-like format:
//
//	default_format = "toon"
//	projection = ["id", "subject", "from"]
//	flatten_singletons = true
//	omit_zero_counts = true
//	sort_by = "date"
//	limit = 50
//
// Profiles are stored as variant.output_profile in the catalog; the applier is
// invoked by the dispatch shapeResponse step (step 8) when a variant carries a
// non-empty output_profile field.
package profile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Profile is the parsed representation of an expression-profile DSL source.
type Profile struct {
	// Name is the profile's identifier (used by override_bindings and inherits).
	Name string

	// Inherits is the name of a base profile this profile builds on top of.
	// Resolution is one level deep — chains are rejected. Spec §9.3.
	Inherits string

	// DefaultFormat is "toon", "json", or "raw". Empty means "inherit from invocation".
	DefaultFormat string

	// Projection is the ordered list of field names to retain. Empty means keep all.
	Projection []string

	// FlattenSingletons, when true, unwraps single-element arrays to their contained value.
	FlattenSingletons bool

	// OmitZeroCounts, when true, drops integer fields whose value is 0.
	OmitZeroCounts bool

	// SortBy is the field name to sort array results by. Empty means preserve order.
	SortBy string

	// Limit is the maximum number of array elements to return. 0 means no limit.
	Limit int

	// OverrideBindings maps op_id (or variant_id) to a profile name. Spec §9.2:
	// the binding is evaluated after the three-level resolution; it substitutes
	// the named profile as the effective expression profile for the target op
	// without modifying catalog data.
	OverrideBindings map[string]string

	// KeepFields is the recursive post-upstream allowlist. Dot paths address
	// nested fields (e.g. "messages.id"). Empty means keep all. Spec §9.1 step 2.
	KeepFields []string

	// DropFields is the recursive post-upstream denylist applied AFTER KeepFields.
	// Empty means drop nothing. Spec §9.1 step 2.
	DropFields []string

	// StripNulls, when true, removes null, empty-string, empty-object, and
	// empty-array fields. In unit tests all fields are treated as safe.
	// Spec §9.1 step 3.
	StripNulls bool

	// Flatten, when true, unwraps common envelopes: {"items":[...]}, {"data":[...]},
	// or provider-configured wrappers — returning just the inner array.
	// Differs from FlattenSingletons which unwraps single-element arrays.
	// Spec §9.1 step 4.
	Flatten bool

	// CollapseArrays, when non-nil, truncates arrays and records omitted_count.
	// Spec §9.1 step 5.
	CollapseArrays *CollapseArraysSpec

	// TruncateStrings, when non-nil, truncates long string values.
	// Spec §9.1 step 6.
	TruncateStrings *TruncateStringsSpec

	// Dedupe, when non-nil, collapses rows with identical concatenated key.
	// Spec §9.1 step 7.
	Dedupe *DedupeSpec

	// OnEmpty is a sentinel string emitted when shaping produces zero records
	// from a non-empty upstream. Spec §9.1 step (post-pipeline).
	OnEmpty string

	// Recovery controls filesystem tee side-effects: "none", "local_artifact",
	// "resource_link". The side-effect write is performed by
	// internal/dispatch's writeTeeArtifact after step 7 (post-upstream-projection,
	// pre-host-shaping), per spec §9.0.
	// Spec: expression-profile-dsl.md Field Reference: recovery.
	Recovery string

	// FieldMaskMode controls how the upstream field-mask is applied: "upstream"
	// (default — mask applied upstream), "dual_fetch" (one shaped request plus
	// one unmasked recovery fetch; allowed only for variants with
	// risk_class="read" AND annotations.idempotent=true — see
	// ValidateDualFetchGate), or "none" (no upstream mask, host-side shaping
	// only). Spec §9.1.
	FieldMaskMode string

	// TeeMode controls the filesystem tee write semantics: "off", "failures",
	// "always". When empty, the dispatcher applies the spec §9 default:
	// "always" when Recovery != "none", else "off". Spec: expression-profile-dsl.md
	// Field Reference: tee_mode.
	TeeMode string

	// Tests is the list of [[tests]] fixture entries declared in the profile
	// file. `gum profile test` iterates these to verify token ceilings,
	// expected format, lossiness, and result counts (docs/expression-profile-dsl.md
	// "Test Format"; spec §12.1).
	Tests []TestFixture
}

// TestFixture is one [[tests]] entry from the profile DSL (docs/expression-profile-dsl.md
// Test Format). Fields are optional except `fixture`; absent expectations are
// not checked.
type TestFixture struct {
	// Name is the human-readable label for this fixture (test name).
	Name string

	// Profile is the named profile this fixture targets. When empty, the
	// fixture is assumed to target the enclosing profile file's content.
	Profile string

	// Fixture is the path to the JSON input fixture, resolved relative to the
	// profile file's directory.
	Fixture string

	// ExpectFormat is the expected output format ("toon"|"json"|"raw"). Empty
	// means the format is not asserted.
	ExpectFormat string

	// ExpectMaxTokens is the maximum cl100k_base tokens the shaped output may
	// consume. 0 means no ceiling assertion.
	ExpectMaxTokens int

	// ExpectLossySet reports whether ExpectLossy was declared in the profile.
	// When false, lossiness is not asserted.
	ExpectLossySet bool
	ExpectLossy    bool

	// ExpectResultCountSet reports whether ExpectResultCount was declared.
	ExpectResultCountSet bool
	ExpectResultCount    int

	// ExpectOmittedCountSet reports whether ExpectOmittedCount was declared.
	ExpectOmittedCountSet bool
	ExpectOmittedCount    int

	// ExpectFields is the list of field names that MUST be present in the
	// shaped output. Empty means no field-presence assertion.
	ExpectFields []string
}

// CollapseArraysSpec is the configuration for the collapse_arrays pipeline stage.
// Spec: expression-profile-dsl.md Sub-Fields: collapse_arrays.
type CollapseArraysSpec struct {
	// MaxItems is the maximum number of array items to retain. Must be >= 0.
	// 0 is valid only when OnEmpty is also set (DSL constraint; not enforced here).
	MaxItems int
}

// TruncateStringsSpec is the configuration for the truncate_strings pipeline stage.
// Spec: expression-profile-dsl.md Sub-Fields: truncate_strings.
type TruncateStringsSpec struct {
	// DefaultChars is the default character limit for fields not listed in Fields.
	// 0 means no default truncation.
	DefaultChars int

	// Fields overrides the character limit per field name or dot path.
	// Map value must be >= 1.
	Fields map[string]int
}

// DedupeSpec is the configuration for the dedupe pipeline stage.
// Spec: expression-profile-dsl.md Sub-Fields: dedupe.
type DedupeSpec struct {
	// By is the ordered list of key fields. All fields must exist in each row.
	By []string
}

// MergeProfiles overlays layers from highest precedence (first) onto lowest
// precedence (last). For each field, the first layer that declares a non-zero
// value wins. Spec §9.2 three-level hierarchy: pass layers in order
// (project-local, user-global, catalog-embedded).
//
// MergeProfiles never returns nil. If no layers are provided or every layer is
// nil, returns a zero-value *Profile.
func MergeProfiles(layers ...*Profile) *Profile {
	out := &Profile{}
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		if out.Name == "" {
			out.Name = layer.Name
		}
		if out.Inherits == "" {
			out.Inherits = layer.Inherits
		}
		if out.DefaultFormat == "" {
			out.DefaultFormat = layer.DefaultFormat
		}
		if len(out.Projection) == 0 {
			out.Projection = layer.Projection
		}
		if !out.FlattenSingletons {
			out.FlattenSingletons = layer.FlattenSingletons
		}
		if !out.OmitZeroCounts {
			out.OmitZeroCounts = layer.OmitZeroCounts
		}
		if out.SortBy == "" {
			out.SortBy = layer.SortBy
		}
		if out.Limit == 0 {
			out.Limit = layer.Limit
		}
		for op, prof := range layer.OverrideBindings {
			if out.OverrideBindings == nil {
				out.OverrideBindings = make(map[string]string)
			}
			if _, present := out.OverrideBindings[op]; !present {
				out.OverrideBindings[op] = prof
			}
		}
		if len(out.KeepFields) == 0 {
			out.KeepFields = layer.KeepFields
		}
		if len(out.DropFields) == 0 {
			out.DropFields = layer.DropFields
		}
		if !out.StripNulls {
			out.StripNulls = layer.StripNulls
		}
		if !out.Flatten {
			out.Flatten = layer.Flatten
		}
		if out.CollapseArrays == nil {
			out.CollapseArrays = layer.CollapseArrays
		}
		if out.TruncateStrings == nil {
			out.TruncateStrings = layer.TruncateStrings
		}
		if out.Dedupe == nil {
			out.Dedupe = layer.Dedupe
		}
		if out.OnEmpty == "" {
			out.OnEmpty = layer.OnEmpty
		}
		if out.Recovery == "" {
			out.Recovery = layer.Recovery
		}
		if out.TeeMode == "" {
			out.TeeMode = layer.TeeMode
		}
		if out.FieldMaskMode == "" {
			out.FieldMaskMode = layer.FieldMaskMode
		}
	}
	return out
}

// ResolveInherits returns a single merged profile for p by overlaying p on top
// of the base profile named by p.Inherits, looked up in registry. Spec §9.3:
// at most one level of inheritance — if the base itself declares Inherits, it
// is silently ignored. Returns (p, nil) when p has no Inherits set. Returns an
// error when the named base is not found in registry.
func ResolveInherits(p *Profile, registry map[string]*Profile) (*Profile, error) {
	if p == nil || p.Inherits == "" {
		return p, nil
	}
	base, ok := registry[p.Inherits]
	if !ok {
		return nil, fmt.Errorf("profile %q: inherits=%q not found in registry", p.Name, p.Inherits)
	}
	// Strip the base's own Inherits to enforce one-level-deep rule.
	baseCopy := *base
	baseCopy.Inherits = ""
	merged := MergeProfiles(p, &baseCopy)
	merged.Inherits = "" // resolved
	return merged, nil
}

// validFormats is the set of allowed values for default_format.
var validFormats = map[string]bool{
	"toon": true,
	"json": true,
	"raw":  true,
}

// validFieldMaskModes is the closed enum for field_mask_mode (spec §9.1).
// Empty defaults to "upstream" at the executor; the parser stores the literal.
var validFieldMaskModes = map[string]bool{
	"upstream":   true,
	"dual_fetch": true,
	"none":       true,
}

// Parse parses a profile DSL source string and returns a Profile.
// Returns an error if the source contains unknown keys or malformed values.
//
// Section handling:
//   - Top-level (no section header): keys populate the Profile root.
//   - `[[tests]]`: opens a new TestFixture entry; subsequent keys until the
//     next section header populate that entry. Multiple [[tests]] blocks
//     accumulate into Profile.Tests.
func Parse(src string) (*Profile, error) {
	p := &Profile{}
	lines := strings.Split(src, "\n")
	var curTest *TestFixture // when non-nil, keys go to the current [[tests]] entry
	for lineNum, line := range lines {
		// Trim trailing whitespace.
		line = strings.TrimRight(line, " \t\r")
		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		// Section header: [[tests]] opens a new fixture entry.
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if trimmed == "[[tests]]" {
				p.Tests = append(p.Tests, TestFixture{})
				curTest = &p.Tests[len(p.Tests)-1]
				continue
			}
			return nil, fmt.Errorf("profile: line %d: unknown section header %q (only [[tests]] supported)", lineNum+1, trimmed)
		}
		// Split on first "=".
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			return nil, fmt.Errorf("profile: line %d: expected key = value, got %q", lineNum+1, line)
		}
		key := strings.TrimSpace(line[:idx])
		rawVal := strings.TrimSpace(line[idx+1:])

		if curTest != nil {
			if err := parseTestFixtureKey(curTest, key, rawVal, lineNum+1); err != nil {
				return nil, err
			}
			continue
		}

		switch key {
		case "default_format":
			val, err := parseStringLiteral(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: default_format: %w", lineNum+1, err)
			}
			if !validFormats[val] {
				return nil, fmt.Errorf("profile: line %d: default_format: invalid value %q (must be toon, json, or raw)", lineNum+1, val)
			}
			p.DefaultFormat = val
		case "projection":
			vals, err := parseStringArray(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: projection: %w", lineNum+1, err)
			}
			p.Projection = vals
		case "flatten_singletons":
			val, err := parseBool(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: flatten_singletons: %w", lineNum+1, err)
			}
			p.FlattenSingletons = val
		case "omit_zero_counts":
			val, err := parseBool(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: omit_zero_counts: %w", lineNum+1, err)
			}
			p.OmitZeroCounts = val
		case "sort_by":
			val, err := parseStringLiteral(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: sort_by: %w", lineNum+1, err)
			}
			p.SortBy = val
		case "limit":
			val, err := parseInt(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: limit: %w", lineNum+1, err)
			}
			if val < 0 {
				return nil, fmt.Errorf("profile: line %d: limit: must be >= 0", lineNum+1)
			}
			p.Limit = val
		case "field_mask_mode":
			val, err := parseStringLiteral(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: field_mask_mode: %w", lineNum+1, err)
			}
			if !validFieldMaskModes[val] {
				return nil, fmt.Errorf("profile: line %d: field_mask_mode: invalid value %q (must be upstream, dual_fetch, or none)", lineNum+1, val)
			}
			p.FieldMaskMode = val
		case "inherits":
			val, err := parseStringLiteral(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: inherits: %w", lineNum+1, err)
			}
			p.Inherits = val
		case "keep_fields":
			vals, err := parseStringArray(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: keep_fields: %w", lineNum+1, err)
			}
			p.KeepFields = vals
		case "drop_fields":
			vals, err := parseStringArray(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: drop_fields: %w", lineNum+1, err)
			}
			p.DropFields = vals
		case "strip_nulls":
			val, err := parseBool(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: strip_nulls: %w", lineNum+1, err)
			}
			p.StripNulls = val
		case "flatten":
			val, err := parseBool(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: flatten: %w", lineNum+1, err)
			}
			p.Flatten = val
		case "on_empty":
			val, err := parseStringLiteral(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: on_empty: %w", lineNum+1, err)
			}
			p.OnEmpty = val
		case "recovery":
			val, err := parseStringLiteral(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: recovery: %w", lineNum+1, err)
			}
			if !validRecovery[val] {
				return nil, fmt.Errorf("profile: line %d: recovery: invalid value %q (must be none, local_artifact, or resource_link)", lineNum+1, val)
			}
			p.Recovery = val
		case "tee_mode":
			val, err := parseStringLiteral(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: tee_mode: %w", lineNum+1, err)
			}
			if !validTeeModes[val] {
				return nil, fmt.Errorf("profile: line %d: tee_mode: invalid value %q (must be off, failures, or always)", lineNum+1, val)
			}
			p.TeeMode = val
		case "collapse_arrays":
			spec, err := parseCollapseArrays(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: collapse_arrays: %w", lineNum+1, err)
			}
			p.CollapseArrays = spec
		case "truncate_strings":
			spec, err := parseTruncateStrings(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: truncate_strings: %w", lineNum+1, err)
			}
			p.TruncateStrings = spec
		case "dedupe":
			spec, err := parseDedupe(rawVal)
			if err != nil {
				return nil, fmt.Errorf("profile: line %d: dedupe: %w", lineNum+1, err)
			}
			p.Dedupe = spec
		default:
			return nil, fmt.Errorf("profile: line %d: unknown key %q", lineNum+1, key)
		}
	}
	return p, nil
}

// parseStringLiteral parses a quoted string literal, e.g. "toon".
func parseStringLiteral(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("expected quoted string, got %q", s)
	}
	// Unescape basic JSON string.
	var result string
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return "", fmt.Errorf("invalid string literal %q: %w", s, err)
	}
	return result, nil
}

// parseStringArray parses a JSON array of strings, e.g. ["id", "name"].
func parseStringArray(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	var result []string
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil, fmt.Errorf("expected string array, got %q: %w", s, err)
	}
	return result, nil
}

// parseBool parses a boolean literal: "true" or "false".
func parseBool(s string) (bool, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("expected true or false, got %q", s)
}

// parseInt parses a non-negative integer literal.
func parseInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("expected integer, got %q: %w", s, err)
	}
	return n, nil
}

// validRecovery / validTeeModes are the closed enums for the recovery and
// tee_mode keys (docs/expression-profile-dsl.md Field Reference). Cross-field
// rules (recovery=resource_link requires tee_mode=always) are enforced by the
// dedicated validators, not here.
var validRecovery = map[string]bool{"none": true, "local_artifact": true, "resource_link": true}
var validTeeModes = map[string]bool{"off": true, "failures": true, "always": true}

// splitTopLevelCommas splits an inline-table body on commas that are not nested
// inside a {…}, […], or "…" — so `default_chars = 500, fields = { a = 1 }`
// splits into two entries, not three.
func splitTopLevelCommas(s string) []string {
	var parts []string
	depth, start := 0, 0
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' && (i == 0 || s[i-1] != '\\'):
			inStr = !inStr
		case inStr:
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// parseInlineTable parses a TOML inline table `{ k = v, k = v }` into a map of
// key → raw (un-parsed) value strings. Values may themselves be nested inline
// tables or arrays; callers parse each value with the appropriate helper.
func parseInlineTable(s string) (map[string]string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil, fmt.Errorf("expected inline table { ... }, got %q", s)
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	out := map[string]string{}
	if inner == "" {
		return out, nil
	}
	for _, part := range splitTopLevelCommas(inner) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			return nil, fmt.Errorf("inline-table entry %q missing '='", part)
		}
		k := strings.TrimSpace(part[:eq])
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("inline-table duplicate key %q", k)
		}
		out[k] = strings.TrimSpace(part[eq+1:])
	}
	return out, nil
}

// parseCollapseArrays parses `{ max_items = N }`.
func parseCollapseArrays(raw string) (*CollapseArraysSpec, error) {
	tbl, err := parseInlineTable(raw)
	if err != nil {
		return nil, err
	}
	spec := &CollapseArraysSpec{}
	for k, v := range tbl {
		switch k {
		case "max_items":
			n, err := parseInt(v)
			if err != nil {
				return nil, fmt.Errorf("max_items: %w", err)
			}
			if n < 0 {
				return nil, fmt.Errorf("max_items: must be >= 0")
			}
			spec.MaxItems = n
		default:
			return nil, fmt.Errorf("unknown sub-key %q", k)
		}
	}
	return spec, nil
}

// parseTruncateStrings parses `{ default_chars = N, fields = { name = N } }`.
func parseTruncateStrings(raw string) (*TruncateStringsSpec, error) {
	tbl, err := parseInlineTable(raw)
	if err != nil {
		return nil, err
	}
	spec := &TruncateStringsSpec{}
	for k, v := range tbl {
		switch k {
		case "default_chars":
			n, err := parseInt(v)
			if err != nil {
				return nil, fmt.Errorf("default_chars: %w", err)
			}
			if n < 0 {
				return nil, fmt.Errorf("default_chars: must be >= 0")
			}
			spec.DefaultChars = n
		case "fields":
			sub, err := parseInlineTable(v)
			if err != nil {
				return nil, fmt.Errorf("fields: %w", err)
			}
			spec.Fields = map[string]int{}
			for fk, fv := range sub {
				n, err := parseInt(fv)
				if err != nil {
					return nil, fmt.Errorf("fields.%s: %w", fk, err)
				}
				if n < 1 {
					return nil, fmt.Errorf("fields.%s: must be >= 1", fk)
				}
				spec.Fields[fk] = n
			}
		default:
			return nil, fmt.Errorf("unknown sub-key %q", k)
		}
	}
	return spec, nil
}

// parseDedupe parses `{ by = ["a", "b"] }`.
func parseDedupe(raw string) (*DedupeSpec, error) {
	tbl, err := parseInlineTable(raw)
	if err != nil {
		return nil, err
	}
	spec := &DedupeSpec{}
	for k, v := range tbl {
		switch k {
		case "by":
			arr, err := parseStringArray(v)
			if err != nil {
				return nil, fmt.Errorf("by: %w", err)
			}
			spec.By = arr
		default:
			return nil, fmt.Errorf("unknown sub-key %q", k)
		}
	}
	return spec, nil
}

// parseTestFixtureKey populates a [[tests]] entry with one key/value pair.
// Unknown keys are rejected to match the strict-parsing contract of the rest
// of the DSL.
func parseTestFixtureKey(t *TestFixture, key, rawVal string, lineNum int) error {
	switch key {
	case "name":
		val, err := parseStringLiteral(rawVal)
		if err != nil {
			return fmt.Errorf("profile: line %d: tests.name: %w", lineNum, err)
		}
		t.Name = val
	case "profile":
		val, err := parseStringLiteral(rawVal)
		if err != nil {
			return fmt.Errorf("profile: line %d: tests.profile: %w", lineNum, err)
		}
		t.Profile = val
	case "fixture":
		val, err := parseStringLiteral(rawVal)
		if err != nil {
			return fmt.Errorf("profile: line %d: tests.fixture: %w", lineNum, err)
		}
		t.Fixture = val
	case "expect_format":
		val, err := parseStringLiteral(rawVal)
		if err != nil {
			return fmt.Errorf("profile: line %d: tests.expect_format: %w", lineNum, err)
		}
		if !validFormats[val] {
			return fmt.Errorf("profile: line %d: tests.expect_format: invalid value %q (must be toon, json, or raw)", lineNum, val)
		}
		t.ExpectFormat = val
	case "expect_max_tokens":
		val, err := parseInt(rawVal)
		if err != nil {
			return fmt.Errorf("profile: line %d: tests.expect_max_tokens: %w", lineNum, err)
		}
		if val < 0 {
			return fmt.Errorf("profile: line %d: tests.expect_max_tokens: must be >= 0", lineNum)
		}
		t.ExpectMaxTokens = val
	case "expect_lossy":
		val, err := parseBool(rawVal)
		if err != nil {
			return fmt.Errorf("profile: line %d: tests.expect_lossy: %w", lineNum, err)
		}
		t.ExpectLossy = val
		t.ExpectLossySet = true
	case "expect_result_count":
		val, err := parseInt(rawVal)
		if err != nil {
			return fmt.Errorf("profile: line %d: tests.expect_result_count: %w", lineNum, err)
		}
		t.ExpectResultCount = val
		t.ExpectResultCountSet = true
	case "expect_omitted_count":
		val, err := parseInt(rawVal)
		if err != nil {
			return fmt.Errorf("profile: line %d: tests.expect_omitted_count: %w", lineNum, err)
		}
		t.ExpectOmittedCount = val
		t.ExpectOmittedCountSet = true
	case "expect_fields":
		vals, err := parseStringArray(rawVal)
		if err != nil {
			return fmt.Errorf("profile: line %d: tests.expect_fields: %w", lineNum, err)
		}
		t.ExpectFields = vals
	default:
		return fmt.Errorf("profile: line %d: tests.%s: unknown key", lineNum, key)
	}
	return nil
}

// Serialize produces a canonical DSL representation of the Profile that round-trips
// through Parse. Keys are emitted only when they carry non-zero / non-default values.
// Keys are emitted in this fixed order: default_format, projection, flatten_singletons,
// omit_zero_counts, sort_by, limit.
func (p *Profile) Serialize() string {
	var sb strings.Builder
	if p.DefaultFormat != "" {
		fmt.Fprintf(&sb, "default_format = %q\n", p.DefaultFormat)
	}
	if len(p.Projection) > 0 {
		b, _ := json.Marshal(p.Projection)
		fmt.Fprintf(&sb, "projection = %s\n", b)
	}
	if p.FlattenSingletons {
		fmt.Fprintf(&sb, "flatten_singletons = true\n")
	}
	if p.OmitZeroCounts {
		fmt.Fprintf(&sb, "omit_zero_counts = true\n")
	}
	if p.SortBy != "" {
		fmt.Fprintf(&sb, "sort_by = %q\n", p.SortBy)
	}
	if p.Limit > 0 {
		fmt.Fprintf(&sb, "limit = %d\n", p.Limit)
	}
	if p.FieldMaskMode != "" {
		fmt.Fprintf(&sb, "field_mask_mode = %q\n", p.FieldMaskMode)
	}
	if p.Inherits != "" {
		fmt.Fprintf(&sb, "inherits = %q\n", p.Inherits)
	}
	if len(p.KeepFields) > 0 {
		b, _ := json.Marshal(p.KeepFields)
		fmt.Fprintf(&sb, "keep_fields = %s\n", b)
	}
	if len(p.DropFields) > 0 {
		b, _ := json.Marshal(p.DropFields)
		fmt.Fprintf(&sb, "drop_fields = %s\n", b)
	}
	if p.StripNulls {
		fmt.Fprintf(&sb, "strip_nulls = true\n")
	}
	if p.Flatten {
		fmt.Fprintf(&sb, "flatten = true\n")
	}
	if p.CollapseArrays != nil {
		fmt.Fprintf(&sb, "collapse_arrays = { max_items = %d }\n", p.CollapseArrays.MaxItems)
	}
	if p.TruncateStrings != nil {
		var inner strings.Builder
		fmt.Fprintf(&inner, "default_chars = %d", p.TruncateStrings.DefaultChars)
		if len(p.TruncateStrings.Fields) > 0 {
			keys := make([]string, 0, len(p.TruncateStrings.Fields))
			for k := range p.TruncateStrings.Fields {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var fb strings.Builder
			for i, k := range keys {
				if i > 0 {
					fb.WriteString(", ")
				}
				fmt.Fprintf(&fb, "%s = %d", k, p.TruncateStrings.Fields[k])
			}
			fmt.Fprintf(&inner, ", fields = { %s }", fb.String())
		}
		fmt.Fprintf(&sb, "truncate_strings = { %s }\n", inner.String())
	}
	if p.Dedupe != nil {
		b, _ := json.Marshal(p.Dedupe.By)
		fmt.Fprintf(&sb, "dedupe = { by = %s }\n", b)
	}
	if p.OnEmpty != "" {
		fmt.Fprintf(&sb, "on_empty = %q\n", p.OnEmpty)
	}
	if p.Recovery != "" {
		fmt.Fprintf(&sb, "recovery = %q\n", p.Recovery)
	}
	if p.TeeMode != "" {
		fmt.Fprintf(&sb, "tee_mode = %q\n", p.TeeMode)
	}
	return sb.String()
}
