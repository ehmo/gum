package adapters_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

func TestGumCallWithoutDispatcherFailsClosed(t *testing.T) {
	cr := adapters.NewCodeRunner()
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{
			"language": "risor",
			"source":   `gum_call("catalog.read", {})`,
		},
	}

	_, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if err == nil {
		t.Fatal("Execute succeeded; want gum_call to fail closed when dispatcher is not wired")
	}
	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T %v; want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeUnsupportedCapability {
		t.Fatalf("ErrCode = %q; want %q", se.ErrCode, dispatch.ErrCodeUnsupportedCapability)
	}
	if se.Detail["capability"] != "gum_call" {
		t.Fatalf("capability detail = %v; want gum_call", se.Detail["capability"])
	}
}

func TestGumCallReturnsDispatchResult(t *testing.T) {
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		if inv.OpID != "catalog.read" {
			t.Fatalf("OpID = %q; want catalog.read", inv.OpID)
		}
		if inv.RequestedVariantID != "catalog.read.v1" {
			t.Fatalf("RequestedVariantID = %q; want catalog.read.v1", inv.RequestedVariantID)
		}
		if inv.Caller != dispatch.CallerRisor {
			t.Fatalf("Caller = %q; want %q", inv.Caller, dispatch.CallerRisor)
		}
		if inv.AllowWrite || inv.AllowDestructive {
			t.Fatalf("risk flags = write:%v destructive:%v; want both false", inv.AllowWrite, inv.AllowDestructive)
		}
		if inv.Args["name"] != "alpha" {
			t.Fatalf("Args[name] = %v; want alpha", inv.Args["name"])
		}
		return &dispatch.ShapedResponse{
			Format:            "json",
			StructuredContent: map[string]any{"value": "from_dispatch"},
		}, nil
	}}

	got := execScript(t, context.Background(), mock, `
let r = gum_call("catalog.read", {"name": "alpha"}, "catalog.read.v1")
gum_print(r["value"])
`)
	if got != "from_dispatch" {
		t.Fatalf("printed result = %q; want dispatcher result", got)
	}
}
