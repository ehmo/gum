package dispatch_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// loadKernelCatalog reads the minimal gum.code test fixture from testdata.
func loadKernelCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	data, err := os.ReadFile("testdata/kernel-catalog.json")
	if err != nil {
		t.Fatalf("loadKernelCatalog: %v", err)
	}
	var c catalog.Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("loadKernelCatalog unmarshal: %v", err)
	}
	return &c
}

// TestParseAndValidateRejectsEmptyOpID sends an Invocation with empty OpID
// through Dispatch and expects an error containing "INVALID_ARGS".
func TestParseAndValidateRejectsEmptyOpID(t *testing.T) {
	c := loadKernelCatalog(t)
	disp := dispatch.NewDispatcher(c, nil)
	inv := &dispatch.Invocation{
		OpID:   "",
		Args:   map[string]any{"language": "risor", "source": `gum_print("x")`},
		Format: "json",
	}
	_, err := disp.Dispatch(context.Background(), inv)
	if err == nil {
		t.Fatal("expected error for empty OpID, got nil")
	}
	if !strings.Contains(err.Error(), "INVALID_ARGS") {
		t.Errorf("expected error to contain INVALID_ARGS, got: %v", err)
	}
}

// TestDispatchRoutesGumCodeToCodeRunner registers a code.risor adapter, gives
// a snapshot containing a gum.code op, calls Dispatch with a trivial Risor
// program, and asserts the response body contains "hi".
func TestDispatchRoutesGumCodeToCodeRunner(t *testing.T) {
	c := loadKernelCatalog(t)
	runner := adapters.NewCodeRunner()
	disp := dispatch.NewDispatcher(c, map[string]dispatch.Adapter{
		"code.risor": runner,
	})
	inv := &dispatch.Invocation{
		OpID:      "gum.code",
		Args:      map[string]any{"language": "risor", "source": `gum_print("hi")`},
		Format:    "json",
		RequestID: "test-route-1",
	}
	resp, err := disp.Dispatch(context.Background(), inv)
	if err != nil {
		t.Fatalf("Dispatch returned unexpected error: %v", err)
	}
	if !strings.Contains(string(resp.Body), "hi") {
		t.Errorf("response body does not contain 'hi': %s", resp.Body)
	}
}

// capturingHandler is an slog.Handler that records log records for assertion.
type capturingHandler struct {
	records []slog.Record
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(name string) slog.Handler       { return h }

// TestDispatchEmitsStructuredLogPerStep captures slog events during a successful
// dispatch and asserts that each of the 9 lifecycle step names appears at least
// once in the log output as the `event` field.
//
// Spec §14.1 rule 4: every dispatch event entry MUST carry event, op_id,
// variant_id_resolved, risk_class, caller, duration_ms. TestDispatchLogEntryRequiredFields
// enforces the field-presence side; this test enforces the closed-enum side.
func TestDispatchEmitsStructuredLogPerStep(t *testing.T) {
	c := loadKernelCatalog(t)
	runner := adapters.NewCodeRunner()
	disp := dispatch.NewDispatcher(c, map[string]dispatch.Adapter{
		"code.risor": runner,
	})

	h := &capturingHandler{}
	logger := slog.New(h)
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	inv := &dispatch.Invocation{
		OpID:      "gum.code",
		Args:      map[string]any{"language": "risor", "source": `gum_print("step_test")`},
		Format:    "json",
		RequestID: "test-log-steps",
		Caller:    dispatch.CallerCLI,
	}
	_, _ = disp.Dispatch(context.Background(), inv)

	// Collect event values and request_id presence.
	eventNames := map[string]bool{}
	hasRequestID := false
	for _, rec := range h.records {
		rec.Attrs(func(a slog.Attr) bool {
			if a.Key == "event" {
				eventNames[a.Value.String()] = true
			}
			if a.Key == "request_id" {
				hasRequestID = true
			}
			return true
		})
	}

	required := []string{
		"parse_and_validate",
		"evaluate_policy",
		"resolve_variant",
		"cache_check",
		"resolve_auth",
		"token_bucket",
		"execute_adapter",
		"shape_response",
		"record_and_return",
	}
	for _, s := range required {
		if !eventNames[s] {
			t.Errorf("expected slog event %q to be emitted, but it was not", s)
		}
	}
	if !hasRequestID {
		t.Error("no slog record had a request_id attribute")
	}
}

// TestDispatchLogEntryRequiredFields asserts spec §14.1 rule 4: every dispatch
// event log entry carries the six required structured fields (event, op_id,
// variant_id_resolved, risk_class, caller, duration_ms). Pre-resolution events
// (parse_and_validate, evaluate_policy, resolve_variant) carry empty strings
// for variant_id_resolved and risk_class — empty is "present" by the rule;
// the keys MUST be there.
func TestDispatchLogEntryRequiredFields(t *testing.T) {
	c := loadKernelCatalog(t)
	runner := adapters.NewCodeRunner()
	disp := dispatch.NewDispatcher(c, map[string]dispatch.Adapter{
		"code.risor": runner,
	})

	h := &capturingHandler{}
	logger := slog.New(h)
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	inv := &dispatch.Invocation{
		OpID:      "gum.code",
		Args:      map[string]any{"language": "risor", "source": `gum_print("rule4")`},
		Format:    "json",
		RequestID: "test-rule4",
		Caller:    dispatch.CallerMCP,
	}
	_, _ = disp.Dispatch(context.Background(), inv)

	required := []string{
		"event",
		"op_id",
		"variant_id_resolved",
		"risk_class",
		"caller",
		"duration_ms",
	}

	if len(h.records) == 0 {
		t.Fatal("no slog records captured")
	}
	for i, rec := range h.records {
		present := map[string]bool{}
		var eventVal, callerVal string
		var durationVal int64 = -1
		rec.Attrs(func(a slog.Attr) bool {
			present[a.Key] = true
			switch a.Key {
			case "event":
				eventVal = a.Value.String()
			case "caller":
				callerVal = a.Value.String()
			case "duration_ms":
				durationVal = a.Value.Int64()
			}
			return true
		})
		for _, key := range required {
			if !present[key] {
				t.Errorf("record %d (event=%q): missing required key %q", i, eventVal, key)
			}
		}
		if callerVal != string(dispatch.CallerMCP) {
			t.Errorf("record %d (event=%q): caller=%q; want %q", i, eventVal, callerVal, dispatch.CallerMCP)
		}
		if durationVal < 0 {
			t.Errorf("record %d (event=%q): duration_ms non-integer or missing (got %d)", i, eventVal, durationVal)
		}
	}
}
