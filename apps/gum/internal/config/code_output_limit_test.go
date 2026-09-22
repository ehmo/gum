package config_test

import (
	"testing"

	"github.com/ehmo/gum/internal/config"
)

// CodeOutputLimitBytes is the typed reader over `gum config set
// code.output_limit_bytes=...` (spec §6.1). Every unusable value returns 0 and
// every caller reads 0 as "apply the 4096-byte spec default", because the
// alternative -- honour the typo -- would shrink a gum.code script's whole
// output budget to nothing and truncate every print (bead gum-04sz).
func TestCodeOutputLimitBytes(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		raw  string
		want int
	}{
		{name: "unset", set: false, want: 0},
		{name: "plain", set: true, raw: "8192", want: 8192},
		{name: "padded", set: true, raw: "  1024\t", want: 1024},
		{name: "zero", set: true, raw: "0", want: 0},
		{name: "negative", set: true, raw: "-4096", want: 0},
		{name: "empty", set: true, raw: "", want: 0},
		{name: "not a number", set: true, raw: "four thousand", want: 0},
		{name: "float", set: true, raw: "4096.5", want: 0},
		{name: "trailing unit", set: true, raw: "4kb", want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &config.Config{}
			if tc.set {
				c.Set("code.output_limit_bytes", tc.raw)
			}
			if got := c.CodeOutputLimitBytes(); got != tc.want {
				t.Errorf("CodeOutputLimitBytes() with %q = %d; want %d", tc.raw, got, tc.want)
			}
		})
	}
}
