package main

import (
	"log/slog"
	"testing"
)

// TestParseLogLevel locks the closed enum spec §14.1 rule 3: debug|info|warn|
// error (plus "warning" as a friendly synonym for warn), case- and
// whitespace-tolerant. Any other value returns (0, false) so the caller can
// surface a structured CLI_ARG_INVALID instead of silently choosing a default.
func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		in        string
		wantLevel slog.Level
		wantOK    bool
	}{
		{"debug", slog.LevelDebug, true},
		{"INFO", slog.LevelInfo, true},        // case-insensitive
		{" warn ", slog.LevelWarn, true},      // trimmed
		{"warning", slog.LevelWarn, true},     // friendly synonym
		{"Error", slog.LevelError, true},      // mixed case
		{"trace", 0, false},                   // not in the closed enum
		{"", 0, false},                        // empty rejected
		{"info debug", 0, false},              // composite rejected
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseLogLevel(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("parseLogLevel(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if ok && got != tc.wantLevel {
				t.Errorf("parseLogLevel(%q) level = %v, want %v", tc.in, got, tc.wantLevel)
			}
		})
	}
}
