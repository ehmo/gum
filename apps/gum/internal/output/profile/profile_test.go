package profile_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/output/profile"
)

func testdataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata")
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdataDir(t), name))
	if err != nil {
		t.Fatalf("readTestdata %s: %v", name, err)
	}
	return data
}

// catchPanicProfile wraps fn in a recover. Returns (message, true) if fn panicked.
func catchPanicProfile(fn func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("panic: %v", r)
			panicked = true
		}
	}()
	fn()
	return "", false
}

// TestExpressionProfileParseRoundTrip parses a profile DSL string, serialises it
// back with Serialize(), and re-parses to confirm structural equality.
func TestExpressionProfileParseRoundTrip(t *testing.T) {
	defer goleak.VerifyNone(t)

	src := `default_format = "toon"
projection = ["id", "subject", "from"]
flatten_singletons = true
omit_zero_counts = true
sort_by = "date"
limit = 50
`
	var p *profile.Profile
	var err error
	panicMsg, panicked := catchPanicProfile(func() {
		p, err = profile.Parse(src)
	})
	if panicked {
		t.Fatalf("Parse panicked: %s — green team must implement Parse", panicMsg)
	}
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Assert all fields parsed correctly.
	if p.DefaultFormat != "toon" {
		t.Errorf("DefaultFormat = %q; want %q", p.DefaultFormat, "toon")
	}
	if len(p.Projection) != 3 || p.Projection[0] != "id" || p.Projection[1] != "subject" || p.Projection[2] != "from" {
		t.Errorf("Projection = %v; want [id subject from]", p.Projection)
	}
	if !p.FlattenSingletons {
		t.Error("FlattenSingletons = false; want true")
	}
	if !p.OmitZeroCounts {
		t.Error("OmitZeroCounts = false; want true")
	}
	if p.SortBy != "date" {
		t.Errorf("SortBy = %q; want %q", p.SortBy, "date")
	}
	if p.Limit != 50 {
		t.Errorf("Limit = %d; want 50", p.Limit)
	}

	// Serialize → re-parse → compare.
	var serialized string
	panicMsg, panicked = catchPanicProfile(func() {
		serialized = p.Serialize()
	})
	if panicked {
		t.Fatalf("Serialize panicked: %s — green team must implement Serialize", panicMsg)
	}

	var p2 *profile.Profile
	panicMsg, panicked = catchPanicProfile(func() {
		p2, err = profile.Parse(serialized)
	})
	if panicked {
		t.Fatalf("Parse(Serialize(...)) panicked: %s", panicMsg)
	}
	if err != nil {
		t.Fatalf("Parse(Serialize(...)): %v", err)
	}

	if p2.DefaultFormat != p.DefaultFormat {
		t.Errorf("round-trip DefaultFormat: got %q, want %q", p2.DefaultFormat, p.DefaultFormat)
	}
	if len(p2.Projection) != len(p.Projection) {
		t.Errorf("round-trip Projection length: got %d, want %d", len(p2.Projection), len(p.Projection))
	}
	if p2.FlattenSingletons != p.FlattenSingletons {
		t.Errorf("round-trip FlattenSingletons: got %v, want %v", p2.FlattenSingletons, p.FlattenSingletons)
	}
	if p2.OmitZeroCounts != p.OmitZeroCounts {
		t.Errorf("round-trip OmitZeroCounts: got %v, want %v", p2.OmitZeroCounts, p.OmitZeroCounts)
	}
	if p2.SortBy != p.SortBy {
		t.Errorf("round-trip SortBy: got %q, want %q", p2.SortBy, p.SortBy)
	}
	if p2.Limit != p.Limit {
		t.Errorf("round-trip Limit: got %d, want %d", p2.Limit, p.Limit)
	}
}

// TestExpressionProfileParseMinimal ensures parsing an empty source returns a
// zero-value Profile without error.
func TestExpressionProfileParseMinimal(t *testing.T) {
	defer goleak.VerifyNone(t)

	var p *profile.Profile
	var err error
	panicMsg, panicked := catchPanicProfile(func() {
		p, err = profile.Parse("")
	})
	if panicked {
		t.Fatalf("Parse empty panicked: %s — green team must implement Parse", panicMsg)
	}
	if err != nil {
		t.Fatalf("Parse empty: %v", err)
	}
	if p.DefaultFormat != "" {
		t.Errorf("DefaultFormat = %q; want empty", p.DefaultFormat)
	}
	if len(p.Projection) != 0 {
		t.Errorf("Projection = %v; want empty", p.Projection)
	}
	if p.Limit != 0 {
		t.Errorf("Limit = %d; want 0", p.Limit)
	}
}

// TestExpressionProfileParseUnknownKey ensures that an unknown key returns an error.
func TestExpressionProfileParseUnknownKey(t *testing.T) {
	defer goleak.VerifyNone(t)

	var err error
	panicMsg, panicked := catchPanicProfile(func() {
		_, err = profile.Parse(`unknown_field = "bad"`)
	})
	if panicked {
		t.Fatalf("Parse with unknown key panicked: %s", panicMsg)
	}
	if err == nil {
		t.Error("Parse with unknown key should return error, got nil")
	}
}

// TestExpressionProfileGoldens applies each profile + input fixture pair to
// the applier and compares the output to the golden TOON file.
func TestExpressionProfileGoldens(t *testing.T) {
	defer goleak.VerifyNone(t)

	goldens := []struct {
		profileFile string
		inputFile   string
		goldenFile  string
	}{
		{
			profileFile: "gmail-list-profile.toml",
			inputFile:   "gmail-list-input.json",
			goldenFile:  "gmail-list-golden.toon",
		},
		{
			profileFile: "calendar-events-profile.toml",
			inputFile:   "calendar-events-input.json",
			goldenFile:  "calendar-events-golden.toon",
		},
		{
			profileFile: "drive-files-profile.toml",
			inputFile:   "drive-files-input.json",
			goldenFile:  "drive-files-golden.toon",
		},
	}

	for _, tc := range goldens {
		t.Run(tc.profileFile, func(t *testing.T) {
			profileSrc := string(readTestdata(t, tc.profileFile))
			inputBody := readTestdata(t, tc.inputFile)
			wantToon := readTestdata(t, tc.goldenFile)

			var p *profile.Profile
			var err error
			panicMsg, panicked := catchPanicProfile(func() {
				p, err = profile.Parse(profileSrc)
			})
			if panicked {
				t.Fatalf("Parse profile %s panicked: %s", tc.profileFile, panicMsg)
			}
			if err != nil {
				t.Fatalf("Parse profile %s: %v", tc.profileFile, err)
			}

			var out profile.ApplyOutput
			panicMsg, panicked = catchPanicProfile(func() {
				out, err = profile.Apply(p, profile.ApplyInput{
					Body:       inputBody,
					UserFormat: "toon",
				})
			})
			if panicked {
				t.Fatalf("Apply panicked: %s — green team must implement Apply", panicMsg)
			}
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			if out.Format != "toon" {
				t.Errorf("Format = %q; want %q", out.Format, "toon")
			}
			if !out.ProfileApplied {
				t.Error("ProfileApplied = false; want true")
			}

			// Compare output body to golden. Normalize trailing whitespace for robustness.
			gotStr := strings.TrimRight(string(out.Body), "\n")
			wantStr := strings.TrimRight(string(wantToon), "\n")
			if gotStr != wantStr {
				t.Errorf("output mismatch\ngot:\n%s\n\nwant:\n%s", gotStr, wantStr)
			}
		})
	}
}

// TestApplyPreservesJSONFormatWhenRequested ensures Apply returns JSON when
// UserFormat = "json", even if Profile.DefaultFormat = "toon".
// TestDefsPreservation asserts that profile shaping never strips a top-level
// $defs section. JSON Schema $defs (and the older "definitions" key) host
// referenceable schema fragments that other schemas point to via $ref —
// projecting them out would break MCP tool schemas served via gum://schema/<ref>.
// Spec §9 anchor: profile shaping is lossy for data but lossless for schema
// metadata.
func TestDefsPreservation(t *testing.T) {
	defer goleak.VerifyNone(t)

	p := &profile.Profile{
		DefaultFormat: "json",
		Projection:    []string{"type"},
	}

	body := []byte(`{
		"type": "object",
		"properties": {"id": {"$ref": "#/$defs/Id"}},
		"$defs": {
			"Id": {"type": "string", "pattern": "^[a-z]+$"}
		}
	}`)

	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal output: %v\nbody: %s", err, string(out.Body))
	}

	defs, ok := got["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("$defs missing or wrong type in output; got = %v", got)
	}
	idDef, ok := defs["Id"].(map[string]any)
	if !ok {
		t.Fatalf("$defs.Id missing or wrong type; defs = %v", defs)
	}
	if idDef["type"] != "string" {
		t.Errorf("$defs.Id.type = %v; want string", idDef["type"])
	}
	if idDef["pattern"] != "^[a-z]+$" {
		t.Errorf("$defs.Id.pattern = %v; want ^[a-z]+$", idDef["pattern"])
	}

	if got["type"] != "object" {
		t.Errorf("projected key 'type' = %v; want \"object\"", got["type"])
	}
	if _, hasProps := got["properties"]; hasProps {
		t.Errorf("'properties' should have been projected out but is present")
	}
}

// TestProfileComposition covers the spec §9.2 three-level overlay hierarchy
// and §9.3 single-level Inherits resolution.
func TestProfileComposition(t *testing.T) {
	defer goleak.VerifyNone(t)

	t.Run("overlay-precedence", func(t *testing.T) {
		// Three layers: project-local (highest precedence) → user-global →
		// catalog-embedded (lowest precedence).
		project := &profile.Profile{
			DefaultFormat: "json",
			SortBy:        "subject",
		}
		user := &profile.Profile{
			DefaultFormat: "raw", // should NOT win — project already set
			Projection:    []string{"id", "subject"},
			Limit:         25,
		}
		catalog := &profile.Profile{
			DefaultFormat:     "toon",
			Projection:        []string{"id"}, // should NOT win — user already set
			FlattenSingletons: true,
			Limit:             10, // should NOT win
		}

		out := profile.MergeProfiles(project, user, catalog)

		if out.DefaultFormat != "json" {
			t.Errorf("DefaultFormat = %q; want json (project wins)", out.DefaultFormat)
		}
		if out.SortBy != "subject" {
			t.Errorf("SortBy = %q; want subject (project wins)", out.SortBy)
		}
		if len(out.Projection) != 2 || out.Projection[0] != "id" || out.Projection[1] != "subject" {
			t.Errorf("Projection = %v; want [id subject] (user wins, project undeclared)", out.Projection)
		}
		if out.Limit != 25 {
			t.Errorf("Limit = %d; want 25 (user wins, project undeclared)", out.Limit)
		}
		if !out.FlattenSingletons {
			t.Errorf("FlattenSingletons = false; want true (catalog wins, others undeclared)")
		}
	})

	t.Run("nil-and-empty-layers", func(t *testing.T) {
		out := profile.MergeProfiles(nil, nil, nil)
		if out == nil {
			t.Fatal("MergeProfiles(nil...) returned nil; expected non-nil zero profile")
		}
		if out.DefaultFormat != "" {
			t.Errorf("DefaultFormat = %q; want empty", out.DefaultFormat)
		}
	})

	t.Run("override-bindings-first-wins", func(t *testing.T) {
		project := &profile.Profile{
			OverrideBindings: map[string]string{
				"gmail.messages.list": "tiny",
			},
		}
		user := &profile.Profile{
			OverrideBindings: map[string]string{
				"gmail.messages.list":  "medium", // shadowed by project
				"calendar.events.list": "compact",
			},
		}

		out := profile.MergeProfiles(project, user)

		if out.OverrideBindings["gmail.messages.list"] != "tiny" {
			t.Errorf("gmail.messages.list = %q; want tiny (project wins)", out.OverrideBindings["gmail.messages.list"])
		}
		if out.OverrideBindings["calendar.events.list"] != "compact" {
			t.Errorf("calendar.events.list = %q; want compact (only user declared)", out.OverrideBindings["calendar.events.list"])
		}
	})

	t.Run("inherits-one-level", func(t *testing.T) {
		registry := map[string]*profile.Profile{
			"_base": {
				Name:          "_base",
				DefaultFormat: "toon",
				Limit:         100,
			},
		}
		child := &profile.Profile{
			Name:       "child",
			Inherits:   "_base",
			Projection: []string{"id"},
			Limit:      10, // overrides _base
		}

		resolved, err := profile.ResolveInherits(child, registry)
		if err != nil {
			t.Fatalf("ResolveInherits: %v", err)
		}
		if resolved.DefaultFormat != "toon" {
			t.Errorf("DefaultFormat = %q; want toon (inherited)", resolved.DefaultFormat)
		}
		if resolved.Limit != 10 {
			t.Errorf("Limit = %d; want 10 (child overrides)", resolved.Limit)
		}
		if len(resolved.Projection) != 1 || resolved.Projection[0] != "id" {
			t.Errorf("Projection = %v; want [id]", resolved.Projection)
		}
		if resolved.Inherits != "" {
			t.Errorf("Inherits = %q; want empty after resolution", resolved.Inherits)
		}
	})

	t.Run("inherits-chain-rejected-after-one-level", func(t *testing.T) {
		registry := map[string]*profile.Profile{
			"grand":  {Name: "grand", Limit: 50},
			"parent": {Name: "parent", Inherits: "grand", DefaultFormat: "toon"},
		}
		child := &profile.Profile{
			Name:       "child",
			Inherits:   "parent",
			Projection: []string{"id"},
		}

		resolved, err := profile.ResolveInherits(child, registry)
		if err != nil {
			t.Fatalf("ResolveInherits: %v", err)
		}
		if resolved.DefaultFormat != "toon" {
			t.Errorf("DefaultFormat = %q; want toon (from parent)", resolved.DefaultFormat)
		}
		if resolved.Limit != 0 {
			t.Errorf("Limit = %d; want 0 (grand was NOT chained — one level only per §9.3)", resolved.Limit)
		}
	})

	t.Run("inherits-unknown-base-errors", func(t *testing.T) {
		registry := map[string]*profile.Profile{}
		child := &profile.Profile{
			Name:     "child",
			Inherits: "missing",
		}
		_, err := profile.ResolveInherits(child, registry)
		if err == nil {
			t.Fatal("ResolveInherits with unknown base: expected error, got nil")
		}
	})

	t.Run("no-inherits-passthrough", func(t *testing.T) {
		registry := map[string]*profile.Profile{}
		p := &profile.Profile{Name: "leaf", Limit: 7}
		resolved, err := profile.ResolveInherits(p, registry)
		if err != nil {
			t.Fatalf("ResolveInherits: %v", err)
		}
		if resolved != p {
			t.Errorf("ResolveInherits returned a different pointer; want passthrough when Inherits is empty")
		}
	})
}

func TestApplyPreservesJSONFormatWhenRequested(t *testing.T) {
	defer goleak.VerifyNone(t)

	p := &profile.Profile{
		DefaultFormat: "toon",
		Projection:    []string{"id"},
	}

	var out profile.ApplyOutput
	var err error
	panicMsg, panicked := catchPanicProfile(func() {
		out, err = profile.Apply(p, profile.ApplyInput{
			Body:       []byte(`{"id":"abc","name":"test"}`),
			UserFormat: "json",
		})
	})
	if panicked {
		t.Fatalf("Apply panicked: %s — green team must implement Apply", panicMsg)
	}
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.Format != "json" {
		t.Errorf("Format = %q; want json (UserFormat takes precedence)", out.Format)
	}
}

// TestDefsPreservedUnderStripNulls is the audit regression: with StripNulls=true
// and no projection, the $defs fragment must be preserved VERBATIM — including a
// null-like entry that strip_nulls would otherwise delete. Before the fix,
// captureDefs held a live reference and applyStripNulls mutated it in place, so
// the restored $defs lost its null-valued (or empty) members.
func TestDefsPreservedUnderStripNulls(t *testing.T) {
	defer goleak.VerifyNone(t)

	p := &profile.Profile{
		DefaultFormat: "json",
		StripNulls:    true, // no Projection — v keeps the original $defs sub-map
	}

	// $defs holds a fragment with a null-like member ("Placeholder": null) that a
	// $ref could point at; strip_nulls must NOT reach inside the preserved $defs.
	body := []byte(`{
		"value": "x",
		"drop_me": null,
		"$defs": {
			"Id": {"type": "string"},
			"Placeholder": null
		}
	}`)

	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal output: %v\nbody: %s", err, string(out.Body))
	}

	// Top-level null WAS stripped (strip_nulls still applies outside $defs).
	if _, present := got["drop_me"]; present {
		t.Error("top-level null 'drop_me' should have been stripped")
	}
	defs, ok := got["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("$defs missing or wrong type; got = %v", got)
	}
	// The preserved $defs must STILL contain the null-like Placeholder verbatim.
	if _, ok := defs["Placeholder"]; !ok {
		t.Error("$defs.Placeholder was stripped; preserved $defs must be verbatim")
	}
	if _, ok := defs["Id"]; !ok {
		t.Error("$defs.Id missing from preserved fragment")
	}
}

// TestApplyEmptyBodyIsSuccessNotParseError is the live-sweep regression: a 204
// No Content / empty body (successful delete, some writes) must shape to an
// empty success ({}), not a "parse JSON" error.
func TestApplyEmptyBodyIsSuccessNotParseError(t *testing.T) {
	for _, body := range [][]byte{nil, {}, []byte("  \n")} {
		out, err := profile.Apply(&profile.Profile{DefaultFormat: "json"}, profile.ApplyInput{Body: body, UserFormat: "json"})
		if err != nil {
			t.Errorf("Apply(empty body %q) errored: %v; want empty success", body, err)
			continue
		}
		if string(out.Body) != "{}" {
			t.Errorf("Apply(empty body %q) = %q; want {}", body, out.Body)
		}
	}
}
