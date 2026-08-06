package embedded_test

import (
	"encoding/json"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// Defaults are load-bearing since gum-3gcv: the dispatcher injects a declared
// `default` into the args before validation, so whatever a field declares is
// what goes on the wire. These tests hold every declared default to that
// contract, because a wrong one is invisible — the call succeeds and returns
// plausible data for the wrong query.

// TestRequestFieldDefaultsDecode fails the build when any declared default
// cannot be decoded to its field's declared type. Without this, a curator's
// typo ("1,000") would reach the dispatcher, which skips the undecodable value
// and silently sends nothing.
func TestRequestFieldDefaultsDecode(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	seen := 0
	for i := range cat.Ops {
		op := &cat.Ops[i]
		for j := range op.RequestFields {
			f := &op.RequestFields[j]
			if !f.HasDefault() {
				continue
			}
			seen++
			v, err := f.DefaultValue()
			if err != nil {
				t.Errorf("op %s: field %s: %v", op.OpID, f.Name, err)
				continue
			}
			if _, err := json.Marshal(v); err != nil {
				t.Errorf("op %s: field %s: default decodes to unmarshalable %#v: %v", op.OpID, f.Name, v, err)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no request field in the catalog declares a default; this test would pass vacuously")
	}
	t.Logf("checked %d declared defaults", seen)
}

// TestRequestFieldDefaultsMatchDeclaredType checks the decoded default against
// the field's declared JSON type. An array field whose default decodes to a bare
// string would be rejected upstream with a type error the caller never caused.
func TestRequestFieldDefaultsMatchDeclaredType(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	for i := range cat.Ops {
		op := &cat.Ops[i]
		for j := range op.RequestFields {
			f := &op.RequestFields[j]
			if !f.HasDefault() {
				continue
			}
			v, err := f.DefaultValue()
			if err != nil {
				continue // reported by TestRequestFieldDefaultsDecode
			}
			want := f.Type
			if want == "" {
				want = "string"
			}
			if got := jsonKindOf(v); got != want {
				t.Errorf("op %s: field %s declares type %q but its default decodes to %q (%#v)",
					op.OpID, f.Name, f.Type, got, v)
			}
		}
	}
}

// TestRequestFieldDefaultsAreEnumMembers keeps an enum field's default inside
// its own enum. A default outside the enum is a guaranteed upstream 400 on every
// call that omits the arg.
func TestRequestFieldDefaultsAreEnumMembers(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	for i := range cat.Ops {
		op := &cat.Ops[i]
		for j := range op.RequestFields {
			f := &op.RequestFields[j]
			if !f.HasDefault() || len(f.Enum) == 0 || f.Type == "array" || f.Type == "object" {
				continue
			}
			if !enumContains(f.Enum, f.Default) {
				t.Errorf("op %s: field %s default %q is not one of its enum %v",
					op.OpID, f.Name, f.Default, f.Enum)
			}
		}
	}
}

// TestRequiredRequestFieldsDeclareNoDefault forbids required+default. The
// dispatcher skips a required field's default on purpose, so the combination
// would advertise a value that never gets sent — the exact drift gum-3gcv
// reported. Rejecting it here keeps `describe` truthful.
func TestRequiredRequestFieldsDeclareNoDefault(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	for i := range cat.Ops {
		op := &cat.Ops[i]
		for j := range op.RequestFields {
			f := &op.RequestFields[j]
			if f.Required && f.HasDefault() {
				t.Errorf("op %s: field %s is required and declares default %q; "+
					"a required field's absence must stay an INVALID_ARGS, so the default would never be sent",
					op.OpID, f.Name, f.Default)
			}
		}
	}
}

// TestGoogleAdsGeoAndLanguageDeclareNoDefault pins the gum-3gcv repro. The
// catalog advertised geoTargetConstants=geoTargetConstants/2840, the dispatcher
// sent nothing, and a caller who omitted the arg got worldwide volume believing
// it was US volume. Now that a declared default IS sent, re-adding one here
// would silently narrow every unqualified Keyword Planner query to the US and to
// English.
func TestGoogleAdsGeoAndLanguageDeclareNoDefault(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	ops := []string{
		"googleads.keywordPlanIdeas.generateKeywordIdeas",
		"googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics",
		"googleads.keywordPlanIdeas.generateKeywordForecastMetrics",
	}
	fields := []string{"geoTargetConstants", "language"}
	for _, opID := range ops {
		op := findOpByID(cat, opID)
		if op == nil {
			t.Errorf("op %s missing from the catalog", opID)
			continue
		}
		for _, name := range fields {
			f := findFieldByName(op, name)
			if f == nil {
				t.Errorf("op %s: field %s missing", opID, name)
				continue
			}
			if f.HasDefault() {
				t.Errorf("op %s: field %s declares default %q; omitting it returns all locations/languages, "+
					"so the declared default would change the query (gum-3gcv)", opID, name, f.Default)
			}
		}
	}
}

func findOpByID(cat *catalog.Catalog, opID string) *catalog.Op {
	for i := range cat.Ops {
		if cat.Ops[i].OpID == opID {
			return &cat.Ops[i]
		}
	}
	return nil
}

func findFieldByName(op *catalog.Op, name string) *catalog.RequestField {
	for i := range op.RequestFields {
		if op.RequestFields[i].Name == name {
			return &op.RequestFields[i]
		}
	}
	return nil
}

func enumContains(enum []string, want string) bool {
	for _, e := range enum {
		if e == want {
			return true
		}
	}
	return false
}

// jsonKindOf names the JSON type of a decoded default using the catalog's own
// type vocabulary.
func jsonKindOf(v any) string {
	switch v.(type) {
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case string:
		return "string"
	case bool:
		return "boolean"
	case int64:
		return "integer"
	case float64:
		return "number"
	}
	return ""
}
