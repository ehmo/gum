// Spec §9.1 acceptance for field_mask_mode="dual_fetch".
//
// v0.1.0 ships no second upstream fetch, so the kernel refuses to activate
// the mode instead of pretending it ran. These tests pin both halves of that
// contract: an eligible variant is still rejected, and a dispatch that never
// asked for dual_fetch keeps the §11 omit-when-false audit shape.

package dispatch_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
)

// markVariantIdempotent flips the kernel-catalog gum.code variant to carry
// annotations.idempotent=true so the spec §9.1 eligibility gate accepts it.
// The kernel fixture is owned by lifecycle_test.go and is intentionally
// minimal; this helper avoids forking the fixture for one test.
func markVariantIdempotent(t *testing.T, c *catalog.Catalog) {
	t.Helper()
	for i := range c.Ops {
		for j := range c.Ops[i].Variants {
			c.Ops[i].Variants[j].Annotations = &catalog.Annotation{Idempotent: true}
		}
	}
}

// TestDualFetchRejectedWhenVariantEligible drives a dispatch through gum.code
// with the variant patched to satisfy the eligibility gate (risk_class=read
// AND annotations.idempotent=true) and asserts the kernel still refuses.
//
// dual_fetch promises the caller two upstream requests: the shaped one and an
// unmasked recovery fetch that feeds the stage-9 artifact. The kernel makes
// one. Accepting the mode therefore charged one request, wrote a masked
// artifact, and stamped the audit log with a second request that never
// happened. Refusing is the honest answer until the fetch exists.
func TestDualFetchRejectedWhenVariantEligible(t *testing.T) {
	c := loadKernelCatalog(t)
	markVariantIdempotent(t, c)
	sink := &recordingAuditSink{}
	disp := dispatch.NewDispatcherWithConfig(c, map[string]dispatch.Adapter{
		"code.risor": adapters.NewCodeRunner(),
	}, dispatch.DispatcherConfig{Audit: sink})

	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:          "gum.code",
		Args:          map[string]any{"language": "risor", "source": `gum_print("dual_fetch_test")`},
		Format:        "json",
		Caller:        dispatch.CallerMCP,
		OutputProfile: &profile.Profile{FieldMaskMode: profile.FieldMaskModeDualFetch},
	})
	if err == nil {
		t.Fatal("dual_fetch on an eligible variant returned nil error; want INVALID_ARGS")
	}
	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v (%T); want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeInvalidArgs {
		t.Errorf("error_code = %q; want %q", se.ErrCode, dispatch.ErrCodeInvalidArgs)
	}
	if se.Detail["field"] != "field_mask_mode" {
		t.Errorf("detail.field = %v; want field_mask_mode", se.Detail["field"])
	}
	if se.Detail["value"] != profile.FieldMaskModeDualFetch {
		t.Errorf("detail.value = %v; want dual_fetch", se.Detail["value"])
	}
	if !strings.Contains(se.Message, "not implemented") {
		t.Errorf("message = %q; want it to name the missing second fetch", se.Message)
	}
	if len(sink.entries) != 0 {
		t.Errorf("audit entries=%d; want 0 (no upstream call was made)", len(sink.entries))
	}
}

// TestDualFetchNeverStampsAudit pins the negative half: no dispatch may write
// dual_fetch:true, because no dispatch performs a second fetch. Spec §11's
// omit-when-false rule keeps the key absent.
func TestDualFetchNeverStampsAudit(t *testing.T) {
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
// its own reason, before the not-implemented refusal would apply. The audit
// sink must stay empty because the executor is never reached.
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
	if len(sink.entries) != 0 {
		t.Errorf("audit entries=%d after gate rejection; want 0 (gate fires before executor success audit)", len(sink.entries))
	}
}
