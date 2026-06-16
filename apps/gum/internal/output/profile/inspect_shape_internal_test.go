package profile

import "testing"

// TestInspectShapeBranches pins each decode arm of inspectShape so
// regressions that mis-classify a JSON shape (e.g. forgetting the
// "data" fallback or returning omitted_count from a top-level array)
// are caught.
func TestInspectShapeBranches(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantRc  int
		wantOc  int
	}{
		{"invalid_json_returns_zeros", "not json", 0, 0},
		{"empty_body_returns_zeros", "", 0, 0},
		{"top_level_array_returns_length", `[1,2,3]`, 3, 0},
		{"map_items_branch", `{"items":[1,2]}`, 2, 0},
		{"map_data_branch", `{"data":[1,2,3,4]}`, 4, 0},
		{"map_messages_branch", `{"messages":[{},{},{}]}`, 3, 0},
		{"map_omitted_count_only", `{"omitted_count":7}`, 0, 7},
		{"map_items_and_omitted", `{"items":[1],"omitted_count":5}`, 1, 5},
		{"scalar_value_returns_zeros", `42`, 0, 0},
		{"string_value_returns_zeros", `"hello"`, 0, 0},
		{"null_value_returns_zeros", `null`, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc, oc := inspectShape([]byte(tc.body), ApplyOutput{})
			if rc != tc.wantRc || oc != tc.wantOc {
				t.Errorf("inspectShape(%q) = (%d,%d); want (%d,%d)",
					tc.body, rc, oc, tc.wantRc, tc.wantOc)
			}
		})
	}
}
