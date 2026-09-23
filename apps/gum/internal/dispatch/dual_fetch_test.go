// Spec §9.1 entry conditions for field_mask_mode="dual_fetch", exercised
// through the public dispatch API. The forwarding behaviour itself lives in
// dual_fetch_internal_test.go, which needs the in-package adapter fixtures.
//
// These two pin the boundaries: an ineligible variant is refused before any
// upstream request, and a dispatch that never asked for the mode keeps the
// §11 omit-when-false audit shape.

package dispatch_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
)

// TestDualFetchKeyAbsentWithoutMode pins the negative half: a dispatch that
// did not select the mode makes one request, so spec §11's omit-when-false
// rule keeps dual_fetch out of its entry. The key marks the second unmasked
// request and nothing else.
func TestDualFetchKeyAbsentWithoutMode(t *testing.T) {
	c := loadKernelCatalog(t)
	sink := &recordingAuditSink{}
	disp := dispatch.NewDispatcherWithConfig(c, map[string]dispatch.Adapter{
		"code.risor": adapters.NewCodeRunner(),
	}, dispatch.DispatcherConfig{Audit: sink})

	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:   "gum.code",
		Args:   map[string]any{"language": "risor", "source": `gum_print("x")`},
		Format: "json",
		Caller: dispatch.CallerMCP,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(sink.entries) != 1 {
		t.Fatalf("audit entries=%d; want 1", len(sink.entries))
	}
	if _, present := sink.entries[0]["dual_fetch"]; present {
		t.Errorf("audit entry carries dual_fetch; want the key absent; entry=%v", sink.entries[0])
	}
}

// TestDualFetchGateRejectsNonIdempotentVariant keeps the eligibility gate
// covered: a read variant WITHOUT annotations.idempotent=true is rejected for
// its own reason, before the not-implemented refusal would apply. The executor
// is never reached, so the one audit row is the §11 failure row, not a success
// row and not a dual_fetch row.
func TestDualFetchGateRejectsNonIdempotentVariant(t *testing.T) {
	c := loadKernelCatalog(t)
	// Leave kernel fixture as-is: gum.code variant has no Annotations →
	// Idempotent defaults to false, so the gate must reject.
	sink := &recordingAuditSink{}
	disp := dispatch.NewDispatcherWithConfig(c, map[string]dispatch.Adapter{
		"code.risor": adapters.NewCodeRunner(),
	}, dispatch.DispatcherConfig{Audit: sink})

	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:          "gum.code",
		Args:          map[string]any{"language": "risor", "source": `gum_print("rejected")`},
		Format:        "json",
		Caller:        dispatch.CallerMCP,
		OutputProfile: &profile.Profile{FieldMaskMode: profile.FieldMaskModeDualFetch},
	})
	if err == nil {
		t.Fatal("dual_fetch on non-idempotent variant returned nil error; want INVALID_ARGS")
	}
	if !strings.Contains(err.Error(), "INVALID_ARGS") {
		t.Errorf("err = %v; want INVALID_ARGS", err)
	}
	if !strings.Contains(err.Error(), "idempotent") {
		t.Errorf("err = %v; want the gate reason to name idempotent", err)
	}
	if len(sink.entries) != 1 {
		t.Fatalf("audit entries=%d after gate rejection; want 1 (the §11 failure row)", len(sink.entries))
	}
	if got, _ := sink.entries[0]["error_code"].(string); got != string(dispatch.ErrCodeInvalidArgs) {
		t.Errorf("error_code = %q; want %q", got, dispatch.ErrCodeInvalidArgs)
	}
	if _, present := sink.entries[0]["dual_fetch"]; present {
		t.Errorf("gate-rejection row carries dual_fetch; entry=%v", sink.entries[0])
	}
}
