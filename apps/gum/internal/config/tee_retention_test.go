package config_test

import (
	"testing"

	"github.com/ehmo/gum/internal/config"
)

// TeeRetentionHours is the only typed reader over the flat value store, so it
// is where a bad `gum config set output.tee_retention_hours=...` has to be
// absorbed. It returns 0 for everything unusable and every caller maps 0 to the
// spec §9.0 default, because the alternative reading -- treat the typo as the
// window -- either expires artifacts the moment they are written or advertises
// an `artifact_expires_at` that no scan window can honour (bead gum-sd58).
func TestTeeRetentionHours(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		raw  string
		want int
	}{
		{name: "unset", set: false, want: 0},
		{name: "plain", set: true, raw: "72", want: 72},
		{name: "padded", set: true, raw: "  48\t", want: 48},
		{name: "zero", set: true, raw: "0", want: 0},
		{name: "negative", set: true, raw: "-1", want: 0},
		{name: "empty", set: true, raw: "", want: 0},
		{name: "not a number", set: true, raw: "twenty-four", want: 0},
		{name: "float", set: true, raw: "24.5", want: 0},
		{name: "trailing unit", set: true, raw: "24h", want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &config.Config{}
			if tc.set {
				c.Set("output.tee_retention_hours", tc.raw)
			}
			if got := c.TeeRetentionHours(); got != tc.want {
				t.Errorf("TeeRetentionHours() with %q = %d; want %d", tc.raw, got, tc.want)
			}
		})
	}
}

// A nil receiver reaches this method whenever config.Load failed and the caller
// kept going on defaults, which is what the MCP results handler does.
func TestTeeRetentionHoursOnNilConfig(t *testing.T) {
	var c *config.Config
	if got := c.TeeRetentionHours(); got != 0 {
		t.Errorf("TeeRetentionHours() on a nil config = %d; want 0", got)
	}
}
