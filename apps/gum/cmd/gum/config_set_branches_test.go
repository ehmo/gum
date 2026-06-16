package main

import (
	"strings"
	"testing"
)

// TestConfigSetEmptyKeyOrValueRejected pins the
// `key == "" || value == "" → "empty key or value"` arm. The
// idx-of-`=` check passes (there IS an equals sign), but trimming
// either side to empty MUST surface a precise rejection rather than
// silently persist an unkeyed or valueless row that would break the
// config file's flat-line schema on the next reload.
func TestConfigSetEmptyKeyOrValueRejected(t *testing.T) {
	withTempConfigRootCLI(t)

	cases := []struct {
		name string
		arg  string
	}{
		{"empty_key", "=value"},
		{"empty_value", "key="},
		{"whitespace_key", "   =value"},
		{"whitespace_value", "key=   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runCLI(t, "config", "set", tc.arg)
			if err == nil {
				t.Fatalf("config set %q: expected err; got nil", tc.arg)
			}
			if !strings.Contains(err.Error(), "empty key or value") {
				t.Errorf("err=%v; want 'empty key or value' wrap", err)
			}
		})
	}
}
