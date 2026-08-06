package dispatch

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
)

// TestCatalogDeclTypesAreKnown closes the silent hole in checkArgType: an
// unrecognized declType falls through the switch and returns "", so the param
// gets no type checking at all and nobody finds out. gum.code shipped
// destructive_budget as "int" while the switch only knew "integer", which is
// exactly that failure. Every type the embedded catalog declares must be one
// checkArgType handles.
func TestCatalogDeclTypesAreKnown(t *testing.T) {
	t.Parallel()
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	seen := map[string][]string{}
	for i := range cat.Ops {
		op := &cat.Ops[i]
		for _, pairs := range [][][]string{op.ParamsRequired, op.ParamsOptional} {
			for _, pair := range pairs {
				if len(pair) != 2 {
					t.Errorf("%s: malformed param pair %v; want [name, type]", op.OpID, pair)
					continue
				}
				seen[pair[1]] = append(seen[pair[1]], op.OpID+"."+pair[0])
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("no declared param types found in the embedded catalog; the walk is wrong")
	}
	for declType, params := range seen {
		if _, ok := declaredArgTypes[declType]; !ok {
			t.Errorf("declType %q is not in declaredArgTypes, so %v are never type-checked",
				declType, params)
		}
	}
}

// TestCheckArgTypeIntegerRejectsFractions is the MCP-path guard. JSON numbers
// decode to float64, so an agent sending {"maxResults": 2.5} used to pass
// validation and reach Google as a malformed query value. Whole floats must
// still pass: 2 also arrives as float64.
func TestCheckArgTypeIntegerRejectsFractions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		val      any
		declType string
		wantErr  bool
	}{
		{"whole float64", float64(2), "integer", false},
		{"negative whole float64", float64(-7), "integer", false},
		{"zero float64", float64(0), "integer", false},
		{"fractional float64", 2.5, "integer", true},
		{"tiny fraction float64", 1.0000001, "integer", true},
		{"whole float32", float32(3), "integer", false},
		{"fractional float32", float32(3.5), "integer", true},
		{"NaN", math.NaN(), "integer", true},
		{"positive infinity", math.Inf(1), "integer", true},
		{"negative infinity", math.Inf(-1), "integer", true},
		{"plain int", 4, "integer", false},
		{"uint64", uint64(4), "integer", false},
		{"numeric string", "12", "integer", false},
		{"non-numeric string", "twelve", "integer", true},
		{"fractional string", "2.5", "integer", true},
		{"bool for integer", true, "integer", true},
		// "int" is the spelling the currently embedded gum.code carries. It
		// must behave identically to "integer" or destructive_budget loses its
		// type check.
		{"int alias whole", float64(2), "int", false},
		{"int alias fractional", 2.5, "int", true},
		{"int alias wrong kind", true, "int", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkArgType("budget", tc.val, tc.declType)
			if tc.wantErr && got == "" {
				t.Errorf("checkArgType(%v as %s) = \"\"; want a type error", tc.val, tc.declType)
			}
			if !tc.wantErr && got != "" {
				t.Errorf("checkArgType(%v as %s) = %q; want no error", tc.val, tc.declType, got)
			}
		})
	}
}

// TestCheckArgTypeUnknownDeclTypeIsPermissive pins the runtime behaviour the
// guard test above compensates for: an unknown declType is accepted rather than
// failing the call. Changing this to a hard error would break every caller the
// moment the catalog gains a type, so the enforcement stays at build time.
func TestCheckArgTypeUnknownDeclTypeIsPermissive(t *testing.T) {
	t.Parallel()
	if got := checkArgType("x", struct{}{}, "not-a-type"); got != "" {
		t.Errorf("checkArgType with unknown declType = %q; want \"\" (permissive)", got)
	}
}
