package auditlog

import (
	"strings"
	"testing"
	"time"
)

// marshalEntry drops an optional key when the call site passes the zero
// value explicitly rather than leaving the key out. Both drop arms and the
// canonical-key marshal failure had no caller.

func TestMarshalEntryDropsExplicitFalseOptional(t *testing.T) {
	line, err := marshalEntry(map[string]any{"shaping_bypassed": false}, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("marshalEntry = %v; want nil", err)
	}
	if strings.Contains(string(line), "shaping_bypassed") {
		t.Fatalf("line = %s; want shaping_bypassed omitted when explicitly false", line)
	}
}

func TestMarshalEntryDropsNilOptional(t *testing.T) {
	line, err := marshalEntry(map[string]any{"panic": nil}, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("marshalEntry = %v; want nil", err)
	}
	if strings.Contains(string(line), "panic") {
		t.Fatalf("line = %s; want panic omitted when nil", line)
	}
}

func TestMarshalEntryCanonicalKeyMarshalFailure(t *testing.T) {
	_, err := marshalEntry(map[string]any{"op_id": make(chan int)}, time.Unix(0, 0).UTC())
	if err == nil {
		t.Fatal("marshalEntry = nil; want the unmarshalable op_id to surface an error")
	}
	if !strings.Contains(err.Error(), "chan") {
		t.Fatalf("err = %v; want it to name the unsupported type", err)
	}
}
