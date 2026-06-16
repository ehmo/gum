package auditlog_test

import (
	"os"
	"testing"

	"github.com/ehmo/gum/internal/auditlog"
)

// TestSyncAppendMarshalErrorWritesSentinel pins the marshalEntry error
// arm of syncAppend: a non-JSON-encodable value (chan, func) MUST
// trigger handleFailure → audit.broken sentinel write, NOT a panic.
// This protects the "Append never panics" invariant from §11 against
// hand-rolled entry maps that include an unrendererable Go value.
func TestSyncAppendMarshalErrorWritesSentinel(t *testing.T) {
	dir := t.TempDir()
	w, err := auditlog.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// chan int is not encodable by encoding/json.
	w.Append(map[string]any{
		"op_id":     "gmail.send",
		"args_hash": "abc",
		"bad":       make(chan int),
	})

	// audit.broken sentinel MUST exist now.
	if _, err := os.Stat(w.SentinelPath()); err != nil {
		t.Errorf("sentinel missing after marshal failure: %v", err)
	}
}
