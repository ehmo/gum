package dispatch

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
)

// minStubCount is the coverage threshold bead gum-61d set for the generated
// dispatch surface. cmd/gen-catalog selects the Workspace-family typed REST
// variants, the densest cluster of routine reads and writes the dispatcher
// serves; fewer than 50 stubs means the selection predicate broke.
const minStubCount = 50

// expectedStubVariantIDs recomputes cmd/gen-catalog's selection predicate
// (gen_dispatch_stubs.go WriteDispatchStubs) against the embedded catalog. The
// predicate is duplicated rather than imported because the generator lives in
// package main; keeping the copy here is what makes the set comparison below a
// real check on the committed stubs instead of a tautology.
func expectedStubVariantIDs(t *testing.T) []string {
	t.Helper()
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	var ids []string
	for i := range cat.Ops {
		op := &cat.Ops[i]
		if op.ServiceFamily != "workspace" {
			continue
		}
		for j := range op.Variants {
			v := &op.Variants[j]
			if v.BackendKind != catalog.BackendKindTypedRestSDK {
				continue
			}
			if v.InterfaceKind != catalog.InterfaceKindDiscoveryREST {
				continue
			}
			if v.Binding == nil || v.Binding.HTTP == nil {
				continue
			}
			ids = append(ids, v.VariantID)
		}
	}
	sort.Strings(ids)
	return ids
}

// sanitizeIdent mirrors cmd/gen-catalog's variant_id to identifier mapping. A
// divergence here means the file-name assertion below is checking the wrong
// names, so the copy is deliberate and must track the generator.
func sanitizeIdent(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// TestRegisteredStubsMatchCatalog is the staleness gate. The stub tree is
// generated and gitignored, so a checkout can hold stubs built from an older
// catalog. Comparing the registered set against the predicate recomputed from
// the embedded catalog names both directions of drift: a variant the catalog
// gained and the stubs lack, and a stub for a variant the catalog dropped.
func TestRegisteredStubsMatchCatalog(t *testing.T) {
	want := expectedStubVariantIDs(t)
	got := RegisteredVariantIDs()

	inWant := make(map[string]bool, len(want))
	for _, id := range want {
		inWant[id] = true
	}
	inGot := make(map[string]bool, len(got))
	for _, id := range got {
		inGot[id] = true
	}

	var missing, extra []string
	for _, id := range want {
		if !inGot[id] {
			missing = append(missing, id)
		}
	}
	for _, id := range got {
		if !inWant[id] {
			extra = append(extra, id)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d catalog variant(s) have no stub; run `go generate ./gen/dispatch`: %v",
			len(missing), missing)
	}
	if len(extra) > 0 {
		// Two causes. Either the catalog dropped a variant and the stale stub
		// survived, or the variant comes from an in-source builder
		// (BuildGmailTierBOps and friends, which emitStubsOffline appends
		// before generating) that has not been baked into catalog.json yet.
		// The second is the more dangerous one: executeREST resolves against
		// the embedded catalog, so such a stub fails at call time.
		t.Errorf("%d stub(s) registered for variants absent from the embedded catalog; "+
			"regenerate catalog.json if they come from an in-source builder, otherwise `make stubs`: %v",
			len(extra), extra)
	}
}

// TestStubCountMeetsThreshold pins bead gum-61d's floor. It fails loudly if the
// generator's predicate silently narrows (e.g. a service_family rename drops
// most Workspace ops) and leaves a near-empty dispatch surface that still
// passes the set-equality test above.
func TestStubCountMeetsThreshold(t *testing.T) {
	if got := len(RegisteredVariantIDs()); got < minStubCount {
		t.Fatalf("%d stubs registered, want at least %d (bead gum-61d)", got, minStubCount)
	}
}

// TestStubbedVariantBindingsAreWellFormed checks every stubbed variant's HTTP
// binding is usable by the adapter. TypedRestSDK.resolveRequestURL accepts two
// shapes: an absolute URL, or a relative path it concatenates onto
// "https://www.googleapis.com". Both failure modes are silent at generation
// time and only surface as a bad request at call time:
//
//   - a relative path without a leading "/" concatenates into
//     "https://www.googleapis.comcalendar/v3/..." — a different host;
//   - an absolute "http://" path is rejected by validateCredentialURL on every
//     credentialed call, which is all of them.
func TestStubbedVariantBindingsAreWellFormed(t *testing.T) {
	idx, err := variantIndex()
	if err != nil {
		t.Fatalf("variantIndex: %v", err)
	}
	for _, id := range RegisteredVariantIDs() {
		rv, ok := idx[id]
		if !ok {
			t.Errorf("%s: registered but absent from the embedded catalog", id)
			continue
		}
		if rv.Variant == nil || rv.Variant.Binding == nil || rv.Variant.Binding.HTTP == nil {
			t.Errorf("%s: no HTTP binding", id)
			continue
		}
		h := rv.Variant.Binding.HTTP
		if h.Method == "" || h.Method != strings.ToUpper(h.Method) {
			t.Errorf("%s: method %q is empty or not uppercase", id, h.Method)
		}
		switch {
		case strings.HasPrefix(h.Path, "https://"):
		case strings.HasPrefix(h.Path, "http://"):
			t.Errorf("%s: absolute path %q uses http; credentialed calls require https", id, h.Path)
		case !strings.HasPrefix(h.Path, "/"):
			t.Errorf("%s: relative path %q has no leading slash; it would resolve to https://www.googleapis.com%s", id, h.Path, h.Path)
		}
		if rv.OpID == "" {
			t.Errorf("%s: catalog entry has an empty op_id", id)
		}
	}
}

// TestStubFilesMatchRegistry pairs the files on disk with the registry. A
// stub_*.go file that exists but registers nothing (a hand-edit, or a partial
// generator run) is invisible to the set-equality test, because that test only
// sees what init() registered.
func TestStubFilesMatchRegistry(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read stub directory: %v", err)
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "stub_") || !strings.HasSuffix(name, ".go") {
			continue
		}
		onDisk[name] = true
	}

	for _, id := range RegisteredVariantIDs() {
		want := "stub_" + sanitizeIdent(id) + ".go"
		if !onDisk[want] {
			t.Errorf("%s is registered but %s is missing", id, want)
			continue
		}
		delete(onDisk, want)
	}
	for name := range onDisk {
		t.Errorf("%s exists but registers no variant; run `go generate ./gen/dispatch`", name)
	}
}
