// Spec gum-fo59: typed-REST query serializer MUST produce a separate
// ?k=v pair for each element of a []string / []any argument under the
// default style (repeat). Catalog-declared style="csv" produces ?k=a,b.
//
// TDD red (gum-46uq): typedrestsdk.go:141 uses fmt.Sprintf("%v", v) which
// emits "[a b]" for slice args. This test fails until gum-fo59 lands.

package adapters_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
)

// TestTypedRestSDKQueryRepeatStyle asserts that a []string arg produces
// ?k=a&k=b under the default (repeat) style.
func TestTypedRestSDKQueryRepeatStyle(t *testing.T) {
	verifyNoLeaks(t)

	var gotQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery.Store(r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	executor := adapters.NewTypedRestSDK()
	inv, rv := makeTestInvAndVariant(srv.URL)
	inv.Args["k"] = []string{"a", "b"}

	if _, err := executor.Execute(context.Background(), inv, rv, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	q, _ := gotQuery.Load().(string)
	if q != "k=a&k=b" {
		t.Errorf("query = %q; want %q (repeat style: one pair per element)", q, "k=a&k=b")
	}
}

// TestTypedRestSDKQueryAnySliceRepeatStyle ensures []any (a common shape
// when JSON-decoded args land in the executor) is treated the same way.
func TestTypedRestSDKQueryAnySliceRepeatStyle(t *testing.T) {
	verifyNoLeaks(t)

	var gotQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery.Store(r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	executor := adapters.NewTypedRestSDK()
	inv, rv := makeTestInvAndVariant(srv.URL)
	inv.Args["fields"] = []any{"x", "y", "z"}

	if _, err := executor.Execute(context.Background(), inv, rv, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	q, _ := gotQuery.Load().(string)
	if q != "fields=x&fields=y&fields=z" {
		t.Errorf("query = %q; want %q (repeat style: one pair per element)", q, "fields=x&fields=y&fields=z")
	}
}
