package dispatch_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestDispatchResolveVariantErrorLogsAndReturns pins Dispatch's
// `resolveVariant err → logEvent(EventResolveVariant) + return nil, serr3`
// arm (lifecycle.go:350-353). When the policy gate passes (unknown
// ops are deferred to resolveVariant per evaluatePolicy's
// short-circuit) but the op is not in the catalog, resolveVariant
// returns OP_NOT_FOUND. Dispatch MUST surface that envelope verbatim
// (not wrap it) so the host receives a stable, parseable error
// rather than a generic "dispatch failed" message.
func TestDispatchResolveVariantErrorLogsAndReturns(t *testing.T) {
	c := loadKernelCatalog(t)
	disp := dispatch.NewDispatcher(c, nil)
	inv := &dispatch.Invocation{
		OpID:   "definitely.not.in.catalog",
		Args:   map[string]any{},
		Format: "json",
	}

	_, err := disp.Dispatch(context.Background(), inv)
	if err == nil {
		t.Fatal("Dispatch(unknown op)=nil err; want OP_NOT_FOUND envelope from resolveVariant")
	}
	if !strings.Contains(err.Error(), "OP_NOT_FOUND") {
		t.Errorf("err=%q; want OP_NOT_FOUND envelope (resolveVariant must surface stable code)", err)
	}
}
