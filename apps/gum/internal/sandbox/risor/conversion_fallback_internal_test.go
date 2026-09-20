package risor

import (
	"math"
	"testing"
	"time"

	"github.com/deepnoodle-ai/risor/v2/pkg/object"
)

// TestPrintValueFallsBackWhenJSONRefusesTheValue pins the encoder-failure arm
// of printValue. JSON has no form for an infinity, and dropping the print is
// worse than printing the Go rendering: the script's output stream is the
// gum.code response body, so a silent gap there loses a line the script asked
// for.
func TestPrintValueFallsBackWhenJSONRefusesTheValue(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"positive_infinity", math.Inf(1), "+Inf"},
		{"negative_infinity", math.Inf(-1), "-Inf"},
		{"nan", math.NaN(), "NaN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := printValue(tc.value); got != tc.want {
				t.Errorf("printValue(%v) = %q; want %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestRisorObjectToGoKeepsANativeInterfaceValue pins the default branch's first
// arm. Objects outside the switch that do carry a Go value must hand that value
// back, not the Inspect() string, or a []byte response would reach the caller
// as the text "bytes(...)".
func TestRisorObjectToGoKeepsANativeInterfaceValue(t *testing.T) {
	if got, ok := risorObjectToGo(object.NewBytes([]byte("ab"))).([]byte); !ok || string(got) != "ab" {
		t.Errorf("Bytes: got %#v; want []byte(\"ab\")", got)
	}
	stamp := time.Unix(0, 0).UTC()
	if got, ok := risorObjectToGo(object.NewTime(stamp)).(time.Time); !ok || !got.Equal(stamp) {
		t.Errorf("Time: got %#v; want %v", got, stamp)
	}
}
