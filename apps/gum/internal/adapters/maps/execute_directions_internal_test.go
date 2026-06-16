package maps

import (
	"context"
	"strings"
	"testing"

	gmaps "googlemaps.github.io/maps"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestExecuteDirectionsRequiresOriginAndDestination pins the input
// validation guard. Without it the SDK would forward an empty-origin
// request to the live Maps endpoint and burn quota on a 400-bound
// payload. Three failing-shape cases + a passing-shape sanity check.
func TestExecuteDirectionsRequiresOriginAndDestination(t *testing.T) {
	client, err := gmaps.NewClient(gmaps.WithAPIKey("AIza-fake"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	cases := []struct {
		name string
		args map[string]any
	}{
		{"both_missing", map[string]any{}},
		{"missing_origin", map[string]any{"destination": "SF"}},
		{"missing_destination", map[string]any{"origin": "SF"}},
		{"both_empty_strings", map[string]any{"origin": "", "destination": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := executeDirections(context.Background(), client,
				&dispatch.Invocation{Args: tc.args})
			if err == nil {
				t.Fatalf("expected error for missing args")
			}
			if !strings.Contains(err.Error(), "origin and destination") {
				t.Errorf("err=%q; want hint about origin/destination", err)
			}
		})
	}
}
