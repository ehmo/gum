package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// TestNewSearchCmdEmptyCatalogTableBranch pins the "snap == nil" arm under
// --format=table: when the embedded catalog blob is empty the CLI must
// degrade to a clear "no results (catalog empty)" message rather than
// panicking on a nil snapshot inside embed.Build.
func TestNewSearchCmdEmptyCatalogTableBranch(t *testing.T) {
	saved := embedded.CatalogJSON
	t.Cleanup(func() { embedded.CatalogJSON = saved })
	embedded.CatalogJSON = nil

	cmd := newSearchCmd()
	cmd.SetArgs([]string{"anything", "--format", "table"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "no results (catalog empty)") {
		t.Errorf("output=%q; want 'no results (catalog empty)' on empty embed", out.String())
	}
}

// TestNewSearchCmdEmptyCatalogJSONBranch pins the json arm of the same
// guard: with --format=json the CLI must return a parseable
// {"results":[]} envelope so scripts piping the output never see a
// missing key.
func TestNewSearchCmdEmptyCatalogJSONBranch(t *testing.T) {
	saved := embedded.CatalogJSON
	t.Cleanup(func() { embedded.CatalogJSON = saved })
	embedded.CatalogJSON = nil

	cmd := newSearchCmd()
	cmd.SetArgs([]string{"anything", "--format", "json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), `"results":[]`) &&
		!strings.Contains(out.String(), `"results": []`) {
		t.Errorf("output=%q; want empty results envelope", out.String())
	}
}

// TestNewSearchCmdEmptyQueryJSONShape pins the papercut (gum-l0op #1): an empty
// query produces zero BM25 matches, and the JSON envelope must still carry an
// empty array `"results":[]`, never `"results":null`. A null result key forces
// every consuming script to special-case nil before ranging, and is inconsistent
// with the no-match case for a non-empty query.
func TestNewSearchCmdEmptyQueryJSONShape(t *testing.T) {
	cmd := newSearchCmd()
	cmd.SetArgs([]string{"", "--format", "json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s := out.String()
	if strings.Contains(s, `"results":null`) || strings.Contains(s, `"results": null`) {
		t.Errorf("output=%q; results must be [] not null", s)
	}
	if !strings.Contains(s, `"results":[]`) && !strings.Contains(s, `"results": []`) {
		t.Errorf("output=%q; want empty results array", s)
	}
}

// TestNewSearchCmdTableBranchHappyPath pins the renderSearchTable arm:
// an explicit --format=table on a real (embedded) catalog must produce
// the OP_ID/RISK/AUTH/SUMMARY column header so operators see human-
// readable output, NOT a JSON blob.
func TestNewSearchCmdTableBranchHappyPath(t *testing.T) {
	cmd := newSearchCmd()
	cmd.SetArgs([]string{"gmail", "--format", "table"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s := out.String()
	// Either the header line (results present) or the "no results" line
	// (zero matches) is acceptable — both prove the table branch ran.
	if !strings.Contains(s, "OP_ID") && !strings.Contains(s, "no results") {
		t.Errorf("output=%q; want table header or 'no results'", s)
	}
	if strings.HasPrefix(strings.TrimSpace(s), "{") {
		t.Errorf("output looks like JSON; want table:\n%s", s)
	}
}
