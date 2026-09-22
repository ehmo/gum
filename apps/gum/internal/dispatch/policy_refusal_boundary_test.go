// Package dispatch — gum-ixky boundary tests.
//
// An adapter error that already knows its own §7 envelope must keep it
// across the dispatch boundary. mapRateLimited is the single funnel for
// step-7 adapter failures, so a Google policy refusal has to survive it
// with its AUTH_REQUIRED code, missing_components and retryable=false
// intact instead of being flattened to an opaque upstream string.
package dispatch_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

func dispatchUpstream(t *testing.T, ue *adapters.UpstreamError, reqID string) error {
	t.Helper()
	c := loadKernelCatalog(t)
	disp := dispatch.NewDispatcher(c, map[string]dispatch.Adapter{
		"code.risor": &rateLimitedAdapter{err: ue},
	})
	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:      "gum.code",
		Args:      map[string]any{"language": "risor", "source": `gum_print("x")`},
		Format:    "json",
		RequestID: reqID,
	})
	if err == nil {
		t.Fatal("Dispatch returned nil; want the upstream refusal")
	}
	return err
}

func TestDispatchKeepsUpstreamPolicyRefusalEnvelope(t *testing.T) {
	err := dispatchUpstream(t, &adapters.UpstreamError{
		HTTPStatus:   http.StatusForbidden,
		GoogleStatus: "PERMISSION_DENIED",
		Message:      "Request is prohibited by organisation's policy.",
		ErrorReason:  "USER_BLOCKED_BY_ADMIN",
		ErrorDomain:  "googleapis.com",
		OpID:         "google.drive.files.list",
		AuthStrategy: "byo_oauth",
	}, "test-policy-1")

	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T (%v); want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeAuthRequired {
		t.Errorf("ErrCode = %q; want AUTH_REQUIRED", se.ErrCode)
	}
	if se.Retryable {
		t.Error("Retryable = true; the caller cannot retry past an admin block")
	}
	got, _ := se.Detail["missing_components"].([]string)
	if len(got) != 1 || got[0] != "workspace_admin_trust" {
		t.Errorf("missing_components = %v; want [workspace_admin_trust]", se.Detail["missing_components"])
	}
	if want := "gum auth setup google.drive.files.list"; se.Detail["setup_command"] != want {
		t.Errorf("setup_command = %v; want %q", se.Detail["setup_command"], want)
	}
}

// 429 keeps priority: a rate-limit reply is transient and must stay
// RATE_LIMITED even if the body also carried a mapped reason.
func TestDispatchPrefersRateLimitedOverPolicyEnvelope(t *testing.T) {
	err := dispatchUpstream(t, &adapters.UpstreamError{
		HTTPStatus:  http.StatusTooManyRequests,
		ErrorReason: "USER_BLOCKED_BY_ADMIN",
	}, "test-policy-2")

	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T (%v); want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeRateLimited {
		t.Errorf("ErrCode = %q; want RATE_LIMITED for HTTP 429", se.ErrCode)
	}
}

// A 403 the table does not name keeps the pre-gum-ixky shape, so the fix
// cannot silently relabel every permission failure as an auth gap.
func TestDispatchLeavesUnmappedForbiddenAlone(t *testing.T) {
	err := dispatchUpstream(t, &adapters.UpstreamError{
		HTTPStatus:  http.StatusForbidden,
		ErrorReason: "IAM_PERMISSION_DENIED",
		Message:     "caller lacks permission",
	}, "test-policy-3")

	if dispatch.IsStructuredError(err, dispatch.ErrCodeAuthRequired) {
		t.Fatalf("unmapped 403 became AUTH_REQUIRED: %v", err)
	}
}
