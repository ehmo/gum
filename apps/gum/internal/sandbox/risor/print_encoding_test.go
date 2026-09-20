package risor_test

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/sandbox/risor"
)

// TestGumPrintEncodesStructuredValuesAsJSON pins the wire form of gum_print.
//
// gum.code's response body is the concatenated print stream, and the MCP layer
// hands that body to the client as the `data` member of a §13 result. A Go
// `map[a:1 b:x]` dump in that slot is not parseable by any client, and the
// canonical §6.3 usage `gum_print(gum_parallel([...]))` printed exactly that.
// A string stays verbatim: quoting it would break every script that builds its
// own text output.
func TestGumPrintEncodesStructuredValuesAsJSON(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"string_is_verbatim", `gum_print("hi")`, "hi"},
		{"int", `gum_print(42)`, "42"},
		{"float", `gum_print(1.5)`, "1.5"},
		{"bool", `gum_print(true)`, "true"},
		{"nil_is_null", `gum_print(nil)`, "null"},
		{"list", `gum_print([1, 2, 3])`, "[1,2,3]"},
		{"map_sorted_keys", `gum_print({"b": "x", "a": 1})`, `{"a":1,"b":"x"}`},
		{"list_of_maps", `gum_print([{"a": 1}])`, `[{"a":1}]`},
		{"nested", `gum_print({"r": [{"i": 0}]})`, `{"r":[{"i":0}]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := risor.Run(context.Background(), tc.source, risor.Options{})
			if err != nil {
				t.Fatalf("Run(%s): %v", tc.source, err)
			}
			if got := string(out.Printed); got != tc.want {
				t.Errorf("gum_print output = %q; want %q", got, tc.want)
			}
		})
	}
}
