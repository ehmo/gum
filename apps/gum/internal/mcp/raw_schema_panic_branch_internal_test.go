package mcp

import (
	"strings"
	"testing"
)

// TestRawSchemaInvalidJSONPanicsLoudly pins the
// `json.Compact err → panic("rawSchema: invalid JSON: ...")` arm.
// All callers of rawSchema hand it static build-time literals; the
// panic is the deliberate fail-loud guard for mis-edits where a stray
// comma or unbalanced brace would otherwise produce a malformed
// embedded tool schema. A silent (best-effort) recovery would let
// broken schemas ship and surface as MCP wire errors at runtime.
// The test pins both the panic AND the prefix string the build
// engineer needs to grep for in CI logs.
func TestRawSchemaInvalidJSONPanicsLoudly(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("rawSchema(invalid)=normal return; want panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("recover()=%T %v; want string", r, r)
		}
		if !strings.HasPrefix(msg, "rawSchema: invalid JSON:") {
			t.Errorf("panic msg=%q; want 'rawSchema: invalid JSON:' prefix", msg)
		}
	}()
	_ = rawSchema(`{"unbalanced":`)
}
