package maps

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestMapsEndpointBranches pins every observable miss reason — nil rv,
// nil Variant, nil Binding, non-maps prefix, exact-"maps." with no
// suffix — plus the happy path. The dispatch switch routes on this
// string, so a regression that turned "" into "directions" would
// silently forward unintended calls.
func TestMapsEndpointBranches(t *testing.T) {
	cases := []struct {
		name string
		rv   *dispatch.ResolvedVariant
		want string
	}{
		{"nil_rv", nil, ""},
		{"nil_variant", &dispatch.ResolvedVariant{}, ""},
		{"nil_binding", &dispatch.ResolvedVariant{Variant: &catalog.Variant{}}, ""},
		{
			"no_maps_prefix",
			&dispatch.ResolvedVariant{Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "gmail.search"}}},
			"",
		},
		{
			"prefix_only_no_suffix",
			&dispatch.ResolvedVariant{Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "maps."}}},
			"",
		},
		{
			"happy_path_directions",
			&dispatch.ResolvedVariant{Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "maps.directions"}}},
			"directions",
		},
		{
			"happy_path_geocode",
			&dispatch.ResolvedVariant{Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "maps.geocode"}}},
			"geocode",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapsEndpoint(tc.rv); got != tc.want {
				t.Errorf("got=%q; want %q", got, tc.want)
			}
		})
	}
}
