package dispatch

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	idispatch "github.com/ehmo/gum/internal/dispatch"
)

// noopStub is a registrable StubFunc for registry tests. It is never invoked.
func noopStub(context.Context, *idispatch.Invocation, *idispatch.Credentials) (*idispatch.Response, error) {
	return nil, nil
}

// registerForTest registers a synthetic stub and removes it afterwards. The
// registry is package-global and the variant-id tests assert it matches the
// catalog exactly, so a leaked synthetic entry would fail an unrelated test.
func registerForTest(t *testing.T, id string) {
	t.Helper()
	Register(id, noopStub)
	t.Cleanup(func() { delete(registry, id) })
}

// mustPanic runs fn and fails unless it panics with a message containing want.
func mustPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic containing %q, got none", want)
		}
		if msg, _ := r.(string); !strings.Contains(msg, want) {
			t.Fatalf("panic = %v, want a message containing %q", r, want)
		}
	}()
	fn()
}

// anyRegisteredVariant returns one registered variant_id together with the
// catalog entry the stubs were generated from, so drift tests can build inputs
// that differ from the catalog in exactly one field.
func anyRegisteredVariant(t *testing.T) (string, *idispatch.ResolvedVariant) {
	t.Helper()
	ids := RegisteredVariantIDs()
	if len(ids) == 0 {
		t.Fatal("no stubs registered; run `go generate ./gen/dispatch`")
	}
	idx, err := variantIndex()
	if err != nil {
		t.Fatalf("variantIndex: %v", err)
	}
	rv, ok := idx[ids[0]]
	if !ok {
		t.Fatalf("registered variant %s is absent from the embedded catalog", ids[0])
	}
	return ids[0], rv
}

// TestRegisterRejectsMalformedRegistrations pins the three generator defects
// that must fail at package load rather than at dispatch time: an empty id, a
// nil stub, and a duplicate id (which would otherwise let one variant silently
// shadow another).
func TestRegisterRejectsMalformedRegistrations(t *testing.T) {
	mustPanic(t, "empty variant_id", func() { Register("", noopStub) })
	mustPanic(t, "nil stub", func() { Register("gen.dispatch.test.nilstub", nil) })

	registerForTest(t, "gen.dispatch.test.duplicate")
	mustPanic(t, "duplicate stub registration", func() {
		Register("gen.dispatch.test.duplicate", noopStub)
	})
}

// TestLookupRoundTrip verifies a registered stub is retrievable and an
// unregistered id reports miss rather than returning a nil func.
func TestLookupRoundTrip(t *testing.T) {
	registerForTest(t, "gen.dispatch.test.lookup")

	fn, ok := Lookup("gen.dispatch.test.lookup")
	if !ok {
		t.Fatal("Lookup missed a just-registered stub")
	}
	if fn == nil {
		t.Fatal("Lookup returned ok with a nil stub")
	}
	if _, ok := Lookup("gen.dispatch.test.absent"); ok {
		t.Error("Lookup reported ok for an unregistered variant_id")
	}
}

// TestRegisteredVariantIDsAreSortedAndComplete pins the ordering guarantee the
// variant-id tests rely on for set comparison, and that the slice covers the
// whole registry.
func TestRegisteredVariantIDsAreSortedAndComplete(t *testing.T) {
	ids := RegisteredVariantIDs()
	if len(ids) != len(registry) {
		t.Fatalf("RegisteredVariantIDs returned %d ids, registry holds %d", len(ids), len(registry))
	}
	if !sort.StringsAreSorted(ids) {
		t.Error("RegisteredVariantIDs is not sorted")
	}
	for _, id := range ids {
		if _, ok := registry[id]; !ok {
			t.Errorf("RegisteredVariantIDs returned %q, which is not in the registry", id)
		}
	}
}

// TestExecuteRESTRejectsNilInvocation covers the guard that keeps a nil
// dereference out of the adapter.
func TestExecuteRESTRejectsNilInvocation(t *testing.T) {
	id, rv := anyRegisteredVariant(t)
	h := rv.Variant.Binding.HTTP
	_, err := executeREST(context.Background(), nil, &idispatch.Credentials{}, rv.OpID, id, h.Method, h.Path)
	if err == nil || !strings.Contains(err.Error(), "nil invocation") {
		t.Fatalf("err = %v, want a nil-invocation error", err)
	}
}

// TestExecuteRESTRejectsCatalogDrift pins the three drift checks. Each fires
// when the catalog has been regenerated but the stubs have not, and each must
// fail before any HTTP request leaves the process — a stub firing at a stale
// URL template is the failure this guards against.
func TestExecuteRESTRejectsCatalogDrift(t *testing.T) {
	id, rv := anyRegisteredVariant(t)
	h := rv.Variant.Binding.HTTP
	inv := &idispatch.Invocation{OpID: rv.OpID}
	creds := &idispatch.Credentials{Token: "drift-test-token"}

	cases := []struct {
		name            string
		opID, variantID string
		method, path    string
		want            string
	}{
		{
			name:      "unknown variant",
			opID:      rv.OpID,
			variantID: "gen.dispatch.test.absent.variant",
			method:    h.Method,
			path:      h.Path,
			want:      "not in the embedded catalog",
		},
		{
			name:      "op id drift",
			opID:      rv.OpID + ".renamed",
			variantID: id,
			method:    h.Method,
			path:      h.Path,
			want:      "catalog binds it to " + rv.OpID,
		},
		{
			name:      "http binding drift",
			opID:      rv.OpID,
			variantID: id,
			method:    "TRACE",
			path:      h.Path,
			want:      "catalog binds " + h.Method,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := executeREST(context.Background(), inv, creds, tc.opID, tc.variantID, tc.method, tc.path)
			if err == nil {
				t.Fatalf("expected an error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "regenerate the stubs") {
				t.Errorf("err = %v, want it to name the fix (regenerate the stubs)", err)
			}
		})
	}
}

// TestExecuteRESTPropagatesContext pins spec §5.7 for the generated surface:
// the context a stub receives must reach the HTTP call. Invoking a real
// registered stub with an already-cancelled context must return
// context.Canceled without waiting on the network, which is only possible if
// ctx was threaded through to the request. The timeout guard turns a
// regression into a failure instead of a hung suite.
func TestExecuteRESTPropagatesContext(t *testing.T) {
	id, rv := anyRegisteredVariant(t)
	stub, ok := Lookup(id)
	if !ok {
		t.Fatalf("stub for %s vanished between listing and lookup", id)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		_, err := stub(ctx, &idispatch.Invocation{OpID: rv.OpID}, &idispatch.Credentials{Token: "ctx-test-token"})
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want it to wrap context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stub did not return within 5s of a cancelled context — ctx is not reaching the HTTP call")
	}
}
