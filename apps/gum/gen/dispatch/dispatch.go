// Package dispatch holds the compile-time REST dispatch stubs for the
// dispatch-curated op surface (spec §5.7). One stub_*.go file per eligible
// variant is emitted by cmd/gen-catalog; this file is the handwritten runtime
// they share.
//
// The stub_*.go files are gitignored and rebuilt by `make stubs`. This file,
// dispatch_test.go and variant_ids_test.go are hand-authored, tracked by three
// negation rules in .gitignore, and the generator never touches them. Do not
// `rm -rf` the directory to force a rebuild; run the generator, which sweeps
// stub_*.go only.
//
// Until 2026-08-06 the ignore rule covered the whole tree, so these three files
// lived on one machine and no fresh checkout could compile the package. CI hid
// it: with an empty gen/dispatch, `go test ./...` saw 49 packages and never
// missed the 50th.
//
//	stub_<variant>.go            executeREST                 internal/adapters
//	┌──────────────────────┐     ┌──────────────────────┐     ┌───────────────┐
//	│ init(): Register(id) │────▶│ registry[id]         │     │               │
//	│ stub_<id>(ctx,...)   │────▶│ resolve id → variant │────▶│ TypedRestSDK  │
//	│   opID,method,path   │     │ assert binding match │     │   .Execute    │
//	└──────────────────────┘     └──────────────────────┘     └───────────────┘
//	                                       │
//	                             internal/embedded/catalog.json
//
// Nothing imports this package yet: v0.1.0 routes every op through
// internal/dispatch, and the stubs exist to pin the §5.7 contract (a stub per
// curated variant, context threaded to the HTTP call) ahead of v0.2.0 replacing
// each body with a typed google.golang.org/api call chain.
package dispatch

//go:generate go run ../../cmd/gen-catalog -offline-stubs-only -out=../../internal/embedded/catalog.json -stubs-out=.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	idispatch "github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embedded"
)

// StubFunc is the signature every generated stub satisfies.
type StubFunc func(ctx context.Context, inv *idispatch.Invocation, creds *idispatch.Credentials) (*idispatch.Response, error)

// registry maps variant_id to its generated stub. Written only from the init()
// functions of the stub files, which the runtime serialises before any other
// code in the package can run, so no lock is needed on the read path.
var registry = map[string]StubFunc{}

// Register records a stub under its variant_id. The generated init() functions
// are its only production caller. It panics rather than returning an error: a
// duplicate or malformed registration is a generator defect, and failing at
// package load makes it impossible to ship a binary whose dispatch table
// silently shadows one variant with another.
func Register(variantID string, fn StubFunc) {
	if variantID == "" {
		panic("gen/dispatch: Register called with an empty variant_id")
	}
	if fn == nil {
		panic("gen/dispatch: Register called with a nil stub for " + variantID)
	}
	if _, dup := registry[variantID]; dup {
		panic("gen/dispatch: duplicate stub registration for " + variantID)
	}
	registry[variantID] = fn
}

// Lookup returns the stub registered for variantID.
func Lookup(variantID string) (StubFunc, bool) {
	fn, ok := registry[variantID]
	return fn, ok
}

// RegisteredVariantIDs returns every registered variant_id in sorted order.
func RegisteredVariantIDs() []string {
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

var (
	indexOnce  sync.Once
	indexByID  map[string]*idispatch.ResolvedVariant
	indexError error
)

// variantIndex decodes the embedded catalog once and indexes every variant by
// variant_id. Building it lazily keeps the cost off package load, which the
// 197 stub init() functions already pay for.
func variantIndex() (map[string]*idispatch.ResolvedVariant, error) {
	indexOnce.Do(func() {
		var cat catalog.Catalog
		if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
			indexError = fmt.Errorf("gen/dispatch: decode embedded catalog: %w", err)
			return
		}
		idx := make(map[string]*idispatch.ResolvedVariant, len(cat.Ops))
		for i := range cat.Ops {
			op := &cat.Ops[i]
			for j := range op.Variants {
				v := &op.Variants[j]
				adapterKey := ""
				if v.Binding != nil {
					adapterKey = v.Binding.AdapterKey
				}
				deprecated := false
				for _, did := range op.DeprecatedVariantIDs {
					if did == v.VariantID {
						deprecated = true
						break
					}
				}
				idx[v.VariantID] = &idispatch.ResolvedVariant{
					OpID:       op.OpID,
					Variant:    v,
					AdapterKey: adapterKey,
					Deprecated: deprecated,
				}
			}
		}
		indexByID = idx
	})
	return indexByID, indexError
}

var (
	executorOnce sync.Once
	executor     *adapters.TypedRestSDK
)

func restSDK() *adapters.TypedRestSDK {
	executorOnce.Do(func() { executor = adapters.NewTypedRestSDK() })
	return executor
}

// executeREST is the shared body of every generated stub. It resolves the
// variant out of the embedded catalog and hands it to the typed-rest-sdk
// adapter, which builds the request with http.NewRequestWithContext and so
// satisfies the §5.7 context-propagation contract for all 197 stubs at once.
//
// The opID, method and path the generator bakes into each stub are checked
// against the catalog rather than used to build the request. Two reasons:
//
//  1. The catalog binding carries fields the generator does not pass through.
//     Four of the stubbed variants set binding.http.header_params, which routes
//     an arg (e.g. fieldMask) to an HTTP header instead of the query string. A
//     request synthesised from method+path alone would silently send those args
//     as query params.
//  2. It turns catalog drift into a loud error at the call site. If the catalog
//     is regenerated and the stubs are not, the mismatch surfaces here instead
//     of firing a request against a stale URL template.
func executeREST(ctx context.Context, inv *idispatch.Invocation, creds *idispatch.Credentials, opID, variantID, method, path string) (*idispatch.Response, error) {
	if inv == nil {
		return nil, fmt.Errorf("gen/dispatch: %s: nil invocation", variantID)
	}
	idx, err := variantIndex()
	if err != nil {
		return nil, err
	}
	rv, ok := idx[variantID]
	if !ok {
		return nil, fmt.Errorf("gen/dispatch: %s: variant is not in the embedded catalog; regenerate the stubs", variantID)
	}
	if rv.OpID != opID {
		return nil, fmt.Errorf("gen/dispatch: %s: stub was generated for op %s, catalog binds it to %s; regenerate the stubs",
			variantID, opID, rv.OpID)
	}
	if rv.Variant == nil || rv.Variant.Binding == nil || rv.Variant.Binding.HTTP == nil {
		return nil, fmt.Errorf("gen/dispatch: %s: variant lost its HTTP binding; regenerate the stubs", variantID)
	}
	if got := rv.Variant.Binding.HTTP; got.Method != method || got.Path != path {
		return nil, fmt.Errorf("gen/dispatch: %s: stub pins %s %s, catalog binds %s %s; regenerate the stubs",
			variantID, method, path, got.Method, got.Path)
	}
	return restSDK().Execute(ctx, inv, rv, creds)
}
