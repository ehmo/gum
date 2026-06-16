package plugins

import (
	"errors"
	"fmt"
	"testing"
)

// TestClassifySpawnError exercises every branch of the spec §8.4 mapping.
// Each sentinel error must produce its dedicated stable code, and any
// unrecognised error falls through to SERVICE_DOWN — the same in-flight
// code the LLM sees, so quarantine telemetry stays consistent with what
// the user already received.
func TestClassifySpawnError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"untrusted_executable", ErrExecutableUntrusted, "PLUGIN_EXECUTABLE_UNTRUSTED"},
		{"manifest_not_found", ErrManifestNotFound, "PLUGIN_MANIFEST_NOT_FOUND"},
		{"manifest_invalid", ErrManifestInvalid, "PLUGIN_MANIFEST_INVALID"},
		{"unsupported_shape", ErrUnsupportedShape, "PLUGIN_SHAPE_UNSUPPORTED"},
		{"unsupported_schema_version", ErrUnsupportedSchemaVersion, "PLUGIN_MANIFEST_SCHEMA_UNSUPPORTED"},
		{"wrapped_sentinel_still_matches", fmt.Errorf("setup: %w", ErrManifestInvalid), "PLUGIN_MANIFEST_INVALID"},
		{"unknown_error_is_service_down", errors.New("kernel panic"), "SERVICE_DOWN"},
		{"nil_error_is_service_down", nil, "SERVICE_DOWN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySpawnError(tc.err); got != tc.want {
				t.Errorf("classifySpawnError(%v) = %q; want %q", tc.err, got, tc.want)
			}
		})
	}
}
