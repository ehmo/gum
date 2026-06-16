package risor

import (
	"context"
	"reflect"
	"testing"

	"github.com/deepnoodle-ai/risor/v2/pkg/object"
)

// TestRisorObjectToGoNilInputs covers both the nil-pointer guard and the
// *object.NilType branch — gum.code expects either form to round-trip as
// Go nil so callers can `if v == nil { ... }`.
func TestRisorObjectToGoNilInputs(t *testing.T) {
	if got := risorObjectToGo(nil); got != nil {
		t.Errorf("nil input: got %v; want nil", got)
	}
	if got := risorObjectToGo(object.Nil); got != nil {
		t.Errorf("NilType: got %v; want nil", got)
	}
}

// TestRisorObjectToGoInt pins the documented Int→int contract (NOT int64)
// so HTTP-status assertions like `code.(int) == 200` keep working.
func TestRisorObjectToGoInt(t *testing.T) {
	got := risorObjectToGo(object.NewInt(200))
	if v, ok := got.(int); !ok || v != 200 {
		t.Errorf("got %#v (%T); want int(200)", got, got)
	}
}

// TestRisorObjectToGoFloat covers the float branch — Risor's Interface()
// returns float64 unchanged.
func TestRisorObjectToGoFloat(t *testing.T) {
	got := risorObjectToGo(object.NewFloat(3.5))
	if v, ok := got.(float64); !ok || v != 3.5 {
		t.Errorf("got %#v (%T); want float64(3.5)", got, got)
	}
}

// TestRisorObjectToGoString covers the string branch.
func TestRisorObjectToGoString(t *testing.T) {
	got := risorObjectToGo(object.NewString("hello"))
	if v, ok := got.(string); !ok || v != "hello" {
		t.Errorf("got %#v (%T); want string(\"hello\")", got, got)
	}
}

// TestRisorObjectToGoBool covers the bool branch.
func TestRisorObjectToGoBool(t *testing.T) {
	got := risorObjectToGo(object.True)
	if v, ok := got.(bool); !ok || !v {
		t.Errorf("got %#v (%T); want bool(true)", got, got)
	}
}

// TestRisorObjectToGoMapRecursesAndCoercesInt drives the *object.Map
// branch and proves the recursion path: a nested Int inside the map
// becomes Go int (not int64) just like the top-level Int branch.
func TestRisorObjectToGoMapRecursesAndCoercesInt(t *testing.T) {
	m := object.NewMap(map[string]object.Object{
		"status": object.NewInt(200),
		"body":   object.NewString("ok"),
	})
	got := risorObjectToGo(m)
	want := map[string]any{
		"status": 200,
		"body":   "ok",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v; want %#v", got, want)
	}
}

// TestRisorObjectToGoDefaultFallback drives the default branch:
// objects without a Go equivalent (e.g. Builtin functions) fall through
// to obj.Interface() or, when that returns nil, obj.Inspect(). Either
// way the test must get a non-nil value back so risor.code callers can
// at least see something for opaque objects.
func TestRisorObjectToGoDefaultFallback(t *testing.T) {
	b := object.NewBuiltin("noop", func(ctx context.Context, args ...object.Object) (object.Object, error) {
		return object.Nil, nil
	})
	got := risorObjectToGo(b)
	if got == nil {
		t.Fatal("got nil; want non-nil fallback (Interface or Inspect)")
	}
}

// TestRisorObjectToGoListRecurses drives the *object.List branch and the
// per-element recursion: each Int element must coerce to Go int.
func TestRisorObjectToGoListRecurses(t *testing.T) {
	l := object.NewList([]object.Object{
		object.NewInt(1),
		object.NewInt(2),
		object.NewString("three"),
	})
	got := risorObjectToGo(l)
	want := []any{1, 2, "three"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v; want %#v", got, want)
	}
}
