package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

func loadCuratedSchemaFixture(t *testing.T) *defaultFieldsSchemaFixture {
	t.Helper()
	fixture, err := loadDefaultFieldsSchema(filepath.Join("testdata", defaultFieldsSchemaFile))
	if err != nil {
		t.Fatalf("load schema fixture: %v", err)
	}
	return fixture
}

// TestDefaultFieldsMatchDiscoverySchema is the load-bearing check on
// default_fields_data.go. Google's partial-response filter does not reject an
// unknown selector: it drops it. A mask with one typo therefore returns a
// well-formed, empty-ish body and nothing anywhere reports a fault, so the
// only place a typo can be caught is here, against the response schema the API
// actually publishes.
func TestDefaultFieldsMatchDiscoverySchema(t *testing.T) {
	fixture := loadCuratedSchemaFixture(t)
	masks := tierADefaultFields()
	if len(masks) == 0 {
		t.Fatal("tierADefaultFields() is empty")
	}
	if err := validateCuratedDefaultFields(masks, fixture); err != nil {
		t.Fatalf("curated default_fields rejected by the upstream schema: %v", err)
	}
	for opID := range masks {
		if _, ok := fixture.Ops[opID]; !ok {
			t.Errorf("%s: curated but absent from the schema fixture", opID)
		}
	}
	for opID := range fixture.Ops {
		if _, ok := masks[opID]; !ok {
			t.Errorf("%s: in the schema fixture but no longer curated; regenerate the fixture", opID)
		}
	}
}

// TestDefaultFieldsValidatorRejectsBadMasks is the mutation check for the test
// above. Without it a validator that returned nil unconditionally would look
// identical from the outside.
func TestDefaultFieldsValidatorRejectsBadMasks(t *testing.T) {
	fixture := loadCuratedSchemaFixture(t)

	tests := []struct {
		name    string
		opID    string
		mask    string
		wantSub string
	}{
		{
			name:    "misspelled top-level field",
			opID:    "calendar.events.list",
			mask:    "nextPageTokn,items(id)",
			wantSub: "not a field of the upstream response schema",
		},
		{
			name:    "misspelled nested field",
			opID:    "calendar.events.list",
			mask:    "items(id,summry)",
			wantSub: "items/summry",
		},
		{
			name:    "field from a sibling resource",
			opID:    "tasks.tasklists.list",
			mask:    "items(id,title,due)",
			wantSub: "items/due",
		},
		{
			name:    "descends into a scalar",
			opID:    "gmail.users.labels.list",
			mask:    "labels(id(name))",
			wantSub: "scalar or opaque object",
		},
		{
			name:    "wildcard",
			opID:    "gmail.users.labels.list",
			mask:    "labels(*)",
			wantSub: "curated default must name its fields",
		},
		{
			name:    "unparseable",
			opID:    "gmail.users.labels.list",
			mask:    "labels(id",
			wantSub: "unmatched",
		},
		{
			name:    "op with no fixture entry",
			opID:    "drive.files.list",
			mask:    "files(id)",
			wantSub: "no response schema in",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCuratedDefaultFields(map[string]string{tc.opID: tc.mask}, fixture)
			if err == nil {
				t.Fatalf("validate(%q) = nil; want an error", tc.mask)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("validate(%q) = %v; want it to mention %q", tc.mask, err, tc.wantSub)
			}
		})
	}
}

// TestApplyDefaultFieldsWritesEveryVariant runs the real apply pass against a
// copy of the shipped catalog and checks the two things it promises: every
// variant of a curated op gets the mask, and catalog.json.sha256 is rewritten
// over the bytes actually written.
func TestApplyDefaultFieldsWritesEveryVariant(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "embedded", "catalog.json"))
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	var blank catalog.Catalog
	if err := json.Unmarshal(src, &blank); err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	for i := range blank.Ops {
		for j := range blank.Ops[i].Variants {
			blank.Ops[i].Variants[j].DefaultFields = ""
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	stripped, err := json.MarshalIndent(&blank, "", "  ")
	if err != nil {
		t.Fatalf("encode catalog: %v", err)
	}
	if err := os.WriteFile(path, stripped, 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	// applyDefaultFields resolves the fixture from the module root, which is
	// two levels up from this package.
	t.Chdir(filepath.Join("..", ".."))
	if err := applyDefaultFields(path); err != nil {
		t.Fatalf("applyDefaultFields: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read applied catalog: %v", err)
	}
	var applied catalog.Catalog
	if err := json.Unmarshal(out, &applied); err != nil {
		t.Fatalf("parse applied catalog: %v", err)
	}
	masks := tierADefaultFields()
	seen := 0
	for i := range applied.Ops {
		want, curated := masks[applied.Ops[i].OpID]
		for j := range applied.Ops[i].Variants {
			got := applied.Ops[i].Variants[j].DefaultFields
			switch {
			case curated && got != want:
				t.Errorf("%s/%s: default_fields = %q; want %q",
					applied.Ops[i].OpID, applied.Ops[i].Variants[j].VariantID, got, want)
			case !curated && got != "":
				t.Errorf("%s/%s: default_fields = %q; want empty",
					applied.Ops[i].OpID, applied.Ops[i].Variants[j].VariantID, got)
			}
		}
		if curated {
			seen++
		}
	}
	if seen != len(masks) {
		t.Fatalf("applied %d curated op(s); want %d", seen, len(masks))
	}

	sumLine, err := os.ReadFile(path + ".sha256")
	if err != nil {
		t.Fatalf("read checksum: %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(sumLine)), "  catalog.json") {
		t.Fatalf("checksum line %q does not name catalog.json", strings.TrimSpace(string(sumLine)))
	}
}
