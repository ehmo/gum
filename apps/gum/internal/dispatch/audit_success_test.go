// Spec §11 acceptance: every successful dispatch appends one audit entry
// carrying the normative shape (op_id, variant_id, args_hash, client_id,
// risk_class, risk_override). gum-4ck wires the success-path emission;
// dispatch/audit.go owns the entry-builder helper.

package dispatch_test

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// recordingAuditSink captures every audit entry the dispatcher appends.
type recordingAuditSink struct {
	entries []map[string]any
}

func (s *recordingAuditSink) Append(e map[string]any) {
	s.entries = append(s.entries, e)
}

// TestDispatchAppendsAuditOnSuccess wires a recording sink, runs one
// successful gum.code dispatch, and asserts the entry carries every
// dispatch-supplied field §11 requires.
func TestDispatchAppendsAuditOnSuccess(t *testing.T) {
	c := loadKernelCatalog(t)
	sink := &recordingAuditSink{}
	disp := dispatch.NewDispatcherWithConfig(c, map[string]dispatch.Adapter{
		"code.risor": adapters.NewCodeRunner(),
	}, dispatch.DispatcherConfig{Audit: sink})

	inv := &dispatch.Invocation{
		OpID:      "gum.code",
		Args:      map[string]any{"language": "risor", "source": `gum_print("audit_test")`},
		Format:    "json",
		Caller:    dispatch.CallerMCP,
		RequestID: "test-audit-success",
	}
	if _, err := disp.Dispatch(context.Background(), inv); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if got := len(sink.entries); got != 1 {
		t.Fatalf("expected 1 audit entry, got %d", got)
	}
	e := sink.entries[0]
	for _, k := range []string{"op_id", "variant_id", "args_hash", "client_id", "risk_class", "risk_override"} {
		if _, ok := e[k]; !ok {
			t.Errorf("audit entry missing required key %q (entry=%v)", k, e)
		}
	}
	if got, _ := e["op_id"].(string); got != "gum.code" {
		t.Errorf("op_id=%q; want gum.code", got)
	}
	if got, _ := e["client_id"].(string); got != "mcp" {
		t.Errorf("client_id=%q; want mcp (from inv.Caller=CallerMCP)", got)
	}
	if got, _ := e["risk_override"].(bool); got != false {
		t.Errorf("risk_override=%v; want false for a non-override variant", got)
	}
	if got, _ := e["args_hash"].(string); len(got) != 64 {
		t.Errorf("args_hash=%q; want 64-char SHA-256 hex", got)
	}
}

// TestDispatchAuditEntryClientIDFallback asserts the client_id field falls
// back to "unknown" when no Caller is set (so audit lines never have a
// blank field — operators can grep for "unknown" to find unstamped code
// paths).
func TestDispatchAuditEntryClientIDFallback(t *testing.T) {
	c := loadKernelCatalog(t)
	sink := &recordingAuditSink{}
	disp := dispatch.NewDispatcherWithConfig(c, map[string]dispatch.Adapter{
		"code.risor": adapters.NewCodeRunner(),
	}, dispatch.DispatcherConfig{Audit: sink})

	_, _ = disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:   "gum.code",
		Args:   map[string]any{"language": "risor", "source": `gum_print("x")`},
		Format: "json",
		// Caller intentionally unset.
	})
	if len(sink.entries) != 1 {
		t.Fatalf("expected 1 entry; got %d", len(sink.entries))
	}
	if got, _ := sink.entries[0]["client_id"].(string); got != "unknown" {
		t.Errorf("client_id=%q; want unknown", got)
	}
}
