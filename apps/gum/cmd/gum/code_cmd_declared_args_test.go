package main

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
)

// TestCodeInvocationArgsAreDeclared asserts that every argument key the
// `gum code` CLI can stamp is a parameter the embedded gum.code op declares.
// Step 1 validation rejects an unknown argument before dispatch, so a flag
// that stamps an undeclared key does not degrade: it fails the whole run with
// INVALID_ARGS. The two older --timeout-sec tests discarded cmd.Execute's
// error, which is how such a flag stayed green while breaking every
// invocation that used it.
func TestCodeInvocationArgsAreDeclared(t *testing.T) {
	args, err := codeInvocationArgs("risor", `gum_print(1)`, true, 3,
		[]string{"gmail.users.messages.delete:msg-1"})
	if err != nil {
		t.Fatalf("codeInvocationArgs: %v", err)
	}

	declared := declaredCodeParams(t)
	for key := range args {
		if !declared[key] {
			t.Errorf("`gum code` stamps %q, which gum.code does not declare; "+
				"the invocation fails step 1 validation with INVALID_ARGS "+
				"(declared: %v)", key, sortedParamNames(declared))
		}
	}
}

// declaredCodeParams reads the gum.code parameter names out of the embedded
// catalog the shipped binary dispatches against.
func declaredCodeParams(t *testing.T) map[string]bool {
	t.Helper()
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	for i := range cat.Ops {
		if cat.Ops[i].OpID != "gum.code" {
			continue
		}
		names := map[string]bool{}
		for _, pair := range append(append([][]string{}, cat.Ops[i].ParamsRequired...), cat.Ops[i].ParamsOptional...) {
			if len(pair) > 0 {
				names[pair[0]] = true
			}
		}
		return names
	}
	t.Fatal("embedded catalog has no gum.code op")
	return nil
}

func sortedParamNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
