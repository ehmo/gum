package dispatch

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ehmo/gum/internal/testutil/golden"
)

// TestStructuredErrorEnvelopeGolden pins the JSON-envelope shape of three
// representative error codes under testdata/golden/envelope/ (gum-b22o.2). The
// envelope wire format is normative per spec §1421; a silent change to the
// MarshalJSON method MUST break this test.
func TestStructuredErrorEnvelopeGolden(t *testing.T) {
	cases := []struct {
		name string
		err  *StructuredError
	}{
		{
			name: "op-not-found",
			err: NewStructuredError(ErrCodeOpNotFound, "no such op").
				WithDetail("op_id", "gmail.users.messages.send.nonexistent"),
		},
		{
			name: "rate-limited",
			err: NewStructuredError(ErrCodeRateLimited, "upstream rate-limited (HTTP 429)").
				WithDetail("retry_after_ms", 5000).
				WithRetryable(true),
		},
		{
			name: "service-down",
			err: NewStructuredError(ErrCodeServiceDown, "internal error; see audit log").
				WithRetryable(false),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.err)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			// Re-marshal through a tree so the wire form is stable across
			// Go map-iteration ordering changes; this is what real callers
			// see after the JSON-RPC layer re-emits the envelope anyway.
			var tree any
			if err := json.Unmarshal(raw, &tree); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			pretty, err := json.MarshalIndent(tree, "", "  ")
			if err != nil {
				t.Fatalf("json.MarshalIndent: %v", err)
			}
			pretty = append(pretty, '\n')
			golden.Bytes(t, envelopeGoldenPath(t, tc.name+".json"), pretty)
		})
	}
}

func envelopeGoldenPath(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", "golden", "envelope", name)
}
