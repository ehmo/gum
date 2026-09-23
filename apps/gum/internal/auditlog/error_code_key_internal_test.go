package auditlog

import (
	"strings"
	"testing"
	"time"
)

// error_code is the §11 key a failed dispatch adds. It belongs to the
// canonical block, so its position is fixed rather than alphabetical, and it
// drops when null like the other optional keys.

func TestMarshalEntryEmitsErrorCodeInCanonicalBlock(t *testing.T) {
	line, err := marshalEntry(map[string]any{
		"op_id":      "gmail.messages.list",
		"error_code": "AUTH_REQUIRED",
		// Sorts before "error_code" alphabetically, so it proves the key is
		// emitted from the canonical order list, not from the extras pass.
		"aaa_extension": "x",
	}, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("marshalEntry = %v; want nil", err)
	}

	got := string(line)
	codeAt := strings.Index(got, `"error_code"`)
	if codeAt == -1 {
		t.Fatalf("line = %s; want error_code emitted", got)
	}
	extraAt := strings.Index(got, `"aaa_extension"`)
	if extraAt == -1 {
		t.Fatalf("line = %s; want the extension key emitted", got)
	}
	if codeAt > extraAt {
		t.Errorf("line = %s; want error_code in the canonical block, before the alphabetical extras", got)
	}
	if opAt := strings.Index(got, `"op_id"`); opAt == -1 || opAt > codeAt {
		t.Errorf("line = %s; want op_id before error_code", got)
	}
	if !strings.Contains(got, `"error_code":"AUTH_REQUIRED"`) {
		t.Errorf("line = %s; want the code value verbatim", got)
	}
}

func TestMarshalEntryDropsNilErrorCode(t *testing.T) {
	line, err := marshalEntry(map[string]any{"error_code": nil}, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("marshalEntry = %v; want nil", err)
	}
	if strings.Contains(string(line), "error_code") {
		t.Fatalf("line = %s; want error_code omitted when nil", line)
	}
}
