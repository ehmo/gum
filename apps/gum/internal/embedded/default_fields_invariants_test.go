package embedded_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/fieldmask"
)

// curatedDefaultFieldsOps pins the shipped artifact against the curated table
// in apps/gum/cmd/gen-catalog/default_fields_data.go. That file is the source;
// this list is the contract that `gen-catalog -apply-default-fields` actually
// ran before the catalog was committed. A two-sided pin is the point: adding a
// mask to the generator and forgetting to regenerate fails here with a diff.
var curatedDefaultFieldsOps = []string{
	"calendar.calendarList.list",
	"calendar.events.get",
	"calendar.events.instances",
	"calendar.events.list",
	"drive.about.get",
	"gmail.users.labels.list",
	"sheets.spreadsheets.get",
	"tasks.tasklists.list",
	"tasks.tasks.list",
	"youtube.playlistItems.list",
	"youtube.search.list",
}

// TestCatalogCarriesDefaultFields is the gum-a2wy repro. Spec §772 makes a
// non-empty `default_fields` on at least one variant a MUST, and two live paths
// read it: internal/dispatch/lifecycle.go step 3c falls back to it when a
// profile omits `field_mask`, and cmd/gum/call.go builds the --fields
// completion from it. The shipped catalog carried it on 0 of 228 variants, so
// both paths were dark.
func TestCatalogCarriesDefaultFields(t *testing.T) {
	cat := loadEmbeddedCatalog(t)

	var got []string
	variants := 0
	for i := range cat.Ops {
		carrying := 0
		for j := range cat.Ops[i].Variants {
			if strings.TrimSpace(cat.Ops[i].Variants[j].DefaultFields) != "" {
				carrying++
			}
		}
		if carrying == 0 {
			continue
		}
		if carrying != len(cat.Ops[i].Variants) {
			t.Errorf("%s: %d of %d variants carry default_fields; the mask is per-op, so all or none",
				cat.Ops[i].OpID, carrying, len(cat.Ops[i].Variants))
		}
		got = append(got, cat.Ops[i].OpID)
		variants += carrying
	}
	sort.Strings(got)

	if len(got) == 0 {
		t.Fatal("no variant carries default_fields; spec §772 requires at least one")
	}
	if strings.Join(got, ",") != strings.Join(curatedDefaultFieldsOps, ",") {
		t.Fatalf("ops carrying default_fields:\n  got  %v\n  want %v\n"+
			"re-run: go run ./cmd/gen-catalog -apply-default-fields", got, curatedDefaultFieldsOps)
	}
	t.Logf("default_fields on %d variant(s) across %d op(s)", variants, len(got))
}

// TestDefaultFieldsAreWellFormedReadMasks checks the two properties that make a
// shipped mask safe, independent of which ops carry one.
//
// Grammar, because dispatch puts the string straight on the wire as Google's
// `fields` query arg: an unparseable mask is a 400, and a mask Google parses
// but that names nothing returns an empty body with no error at all.
//
// Read-class only, because a write or destructive response carries the created
// resource's id and etag. A default mask that dropped those would break the
// caller's next call, and nothing in the §9.1 pipeline would report it.
func TestDefaultFieldsAreWellFormedReadMasks(t *testing.T) {
	cat := loadEmbeddedCatalog(t)

	for i := range cat.Ops {
		for j := range cat.Ops[i].Variants {
			v := &cat.Ops[i].Variants[j]
			if v.DefaultFields == "" {
				continue
			}
			name := cat.Ops[i].OpID + "/" + v.VariantID
			parsed, err := fieldmask.Parse(v.DefaultFields)
			if err != nil {
				t.Errorf("%s: default_fields %q does not parse: %v", name, v.DefaultFields, err)
				continue
			}
			for _, path := range parsed.Paths() {
				for _, seg := range path {
					if seg == "*" {
						t.Errorf("%s: default_fields selects %q; a wildcard default saves nothing",
							name, strings.Join(path, "/"))
					}
				}
			}
			if v.RiskClass != catalog.RiskClassRead {
				t.Errorf("%s: risk_class %q carries default_fields; stage-1 masks are curated for read ops only",
					name, v.RiskClass)
			}
		}
	}
}
