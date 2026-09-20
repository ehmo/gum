package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// rawFailDispatcher fails with a plain error, the way an injected collaborator
// or a future adapter path can.
type rawFailDispatcher struct{ err error }

func (d rawFailDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return nil, d.err
}

// Spec §1421: every failure the agent sees carries a parseable envelope with a
// stable error_code. dispatchAndShape used to fall through to free text for any
// error that was not a *dispatch.StructuredError, so the agent got a bare
// sentence with isError=true and nothing to branch on.
func TestDispatchAndShapeAlwaysEmitsErrorCode(t *testing.T) {
	srv := NewServer(rawFailDispatcher{err: errors.New("plugin.mcp: no host configured")})

	res, err := srv.dispatchAndShape(context.Background(), &dispatch.Invocation{OpID: "plug.x"})
	if err != nil {
		t.Fatalf("dispatchAndShape returned a transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false; a failure must be flagged")
	}

	text := firstText(res)
	var obj map[string]any
	if uerr := json.Unmarshal([]byte(text), &obj); uerr != nil {
		t.Fatalf("error content is not JSON (%v); got: %s", uerr, text)
	}
	if obj["error_code"] != "SERVICE_DOWN" {
		t.Errorf("error_code = %v; want SERVICE_DOWN; got: %s", obj["error_code"], text)
	}
	if obj["message"] != "plugin.mcp: no host configured" {
		t.Errorf("message = %v; the original cause must survive; got: %s", obj["message"], text)
	}
	if obj["retryable"] != false {
		t.Errorf("retryable = %v; want false; got: %s", obj["retryable"], text)
	}
}

// A structured failure keeps its own code: the fallback is not a rewrite.
func TestDispatchAndShapeKeepsStructuredCode(t *testing.T) {
	srv := NewServer(rawFailDispatcher{
		err: dispatch.NewStructuredError(dispatch.ErrCodeScopeMissing, "need drive scope").
			WithDetail("scope", "drive.readonly"),
	})

	res, err := srv.dispatchAndShape(context.Background(), &dispatch.Invocation{OpID: "drive.files.list"})
	if err != nil {
		t.Fatalf("dispatchAndShape returned a transport error: %v", err)
	}
	var obj map[string]any
	if uerr := json.Unmarshal([]byte(firstText(res)), &obj); uerr != nil {
		t.Fatalf("error content is not JSON: %v", uerr)
	}
	if obj["error_code"] != "SCOPE_MISSING" {
		t.Errorf("error_code = %v; want SCOPE_MISSING preserved", obj["error_code"])
	}
	if obj["scope"] != "drive.readonly" {
		t.Errorf("detail key scope = %v; want it flattened onto the envelope", obj["scope"])
	}
}
