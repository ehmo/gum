package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestParseGainTime exercises the --since/--until parser. Empty → zero time
// (open-ended bound); RFC3339 and RFC3339Nano both accepted; anything else
// fails with a flag-prefixed message so the user knows which flag misparsed.
func TestParseGainTime(t *testing.T) {
	cases := []struct {
		name      string
		flag      string
		raw       string
		wantZero  bool
		wantUTC   bool
		wantErr   bool
		errSubstr string
	}{
		{name: "empty_returns_zero", flag: "--since", raw: "", wantZero: true},
		{name: "rfc3339_basic", flag: "--since", raw: "2026-05-25T12:00:00Z", wantUTC: true},
		{name: "rfc3339_with_offset_converts_to_utc", flag: "--until", raw: "2026-05-25T05:00:00-07:00", wantUTC: true},
		{name: "rfc3339_nano", flag: "--since", raw: "2026-05-25T12:00:00.123456789Z", wantUTC: true},
		{name: "garbage_fails", flag: "--since", raw: "not-a-date", wantErr: true, errSubstr: "--since"},
		{name: "date_only_fails", flag: "--until", raw: "2026-05-25", wantErr: true, errSubstr: "--until"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseGainTime(tc.flag, tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseGainTime(%q) err = nil, want error", tc.raw)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("err = %q, want substring %q", err, tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGainTime(%q) err = %v", tc.raw, err)
			}
			if tc.wantZero && !got.IsZero() {
				t.Errorf("parseGainTime(%q) = %v, want zero time", tc.raw, got)
			}
			if tc.wantUTC && got.Location() != time.UTC {
				t.Errorf("parseGainTime(%q) location = %v, want UTC", tc.raw, got.Location())
			}
		})
	}
}

// TestDefaultFixtureReplayDir verifies the resolver returns a path that ends in
// testdata/fixtures/gain-replay. When the runtime-relative candidate exists, it
// is preferred over the bare relative fallback so the CLI works from any cwd.
func TestDefaultFixtureReplayDir(t *testing.T) {
	got := defaultFixtureReplayDir()
	if !strings.HasSuffix(filepath.ToSlash(got), "testdata/fixtures/gain-replay") {
		t.Errorf("defaultFixtureReplayDir = %q, want suffix testdata/fixtures/gain-replay", got)
	}
	// When the resolved path is absolute the runtime-anchored branch executed
	// successfully; it must point at an existing directory.
	if filepath.IsAbs(got) {
		if _, err := os.Stat(got); err != nil {
			t.Errorf("resolved absolute path does not exist: %v", err)
		}
	}
}
