// Spec §9.1 + §11 acceptance: when an invocation carries an
// expression-profile with field_mask_mode="dual_fetch" and the resolved
// variant satisfies the gate (risk_class=read AND annotations.idempotent=
// true), the dispatch succeeds and the audit-log entry includes
// dual_fetch:true. When the gate fails the dispatch returns INVALID_ARGS
// before any executor call.

package dispatch_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
)

// markVariantIdempotent flips the kernel-catalog gum.code variant to carry
// annotations.idempotent=true so the spec §9.1 gate accepts it. The kernel
// fixture is owned by lifecycle_test.go and is intentionally minimal; this
// helper avoids forking the fixture for one test.
func markVariantIdempotent(t *testing.T, c *catalog.Catalog) {
	t.Helper()
	for i := range c.Ops {
		for j := range c.Ops[i].Variants {
			c.Ops[i].Variants[j].Annotations = &catalog.Annotation{Idempotent: true}
		}
	}
}

// TestDualFetchAuditFlagEmittedOnReadIdempotent drives a complete dispatch
// through gum.code (risk_class=read) with the kernel variant patched to
// carry annotations.idempotent=true, and asserts the audit entry includes
// dual_fetch:true. Spec §11 omit-when-false rule keeps the key absent on
// non-dual_fetch dispatches; the companion test below pins that.
func TestDualFetchAuditFlagEmittedOnReadIdempotent(t *testing.T) {
	c := loadKernelCatalog(t)
	markVariantIdempotent(t, c)
	sink := &recordingAuditSink{}
	disp := dispatch.NewDispatcherWithConfig(c, map[string]dispatch.Adapter{
		"code.risor": adapters.NewCodeRunner(),
	}, dispatch.DispatcherConfig{Audit: sink})

	inv := &dispatch.Invocation{
		OpID:          "gum.code",
		Args:          map[string]any{"language": "risor", "source": `gum_print("dual_fetch_test")`},
		Format:        "json",
		Caller:        dispatch.CallerMCP,
		OutputProfile: &profile.Profile{FieldMaskMode: profile.FieldMaskModeDualFetch},
	}
	if _, err := disp.Dispatch(context.Background(), inv); err != nil {
		t.Fatalf("Dispatch with dual_fetch profile + eligible variant: %v", err)
	}
	if len(sink.entries) != 1 {
		t.Fatalf("audit entries=%d; want 1", len(sink.entries))
	}
	got, present := sink.entries[0]["dual_fetch"]
	if !present {
		t.Fatalf("audit entry missing dual_fetch key; entry=%v", sink.entries[0])
	}
	if dual, _ := got.(bool); !dual {
		t.Errorf("audit dual_fetch=%v; want true", got)
	}
}

// TestDualFetchAuditFlagAbsentByDefault confirms the omit-when-false §11 rule
// applies: a dispatch without a dual_fetch profile MUST NOT carry the key.
func TestDualFetchAuditFlagAbsentByDefault(t *testing.T) {
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
		t.Errorf("audit entry should omit dual_fetch when false (§11); entry=%v", sink.entries[0])
	}
}

// TestDualFetchGateRejectsNonIdempotentVariant asserts a dual_fetch profile
// applied to a read variant WITHOUT annotations.idempotent=true triggers
// INVALID_ARGS before the executor is reached. The audit sink must NOT
// receive an entry (executor-step audit is gated on success).
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
	if !strings.Contains(err.Error(), "field_mask_mode") {
		t.Errorf("err = %v; want field_mask_mode field in envelope", err)
	}
	if len(sink.entries) != 0 {
		t.Errorf("audit entries=%d after gate rejection; want 0 (gate fires before executor success audit)", len(sink.entries))
	}
}
