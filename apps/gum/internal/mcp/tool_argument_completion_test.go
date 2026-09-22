// Closed-enum tool-argument completions (gum-qcbh).
//
// Spec §13 lists `gum.code.language` and `gum.read|write|destructive.format`
// as completable, and requires `TestMCPCompletions` to prove three cases:
// prefix "r" on language returns ["risor"], prefix "s" returns [], and
// prefix "t" on format returns ["toon"].
//
// MCP carries no tool reference type. CompleteReference accepts "ref/prompt"
// and "ref/resource" and rejects everything else at unmarshal, across every
// protocol revision the pinned SDK implements (2024-11-05 through 2026-07-28);
// gum itself negotiates 2025-06-18 and later, per the §13.1 protocol floor.
// A tool argument therefore reaches the client through the registered
// inputSchema `enum`, which is where an MCP client reads its suggestions
// from. These tests assert the server publishes those enums over the wire,
// and run the client-side prefix filter the spec cases describe.

package mcp_test

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"
)

// toolArgEnum returns the enum published for one tool argument, in wire order.
func toolArgEnum(t *testing.T, ctx context.Context, cs *sdkmcp.ClientSession, tool, arg string) []string {
	t.Helper()
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tl := range listed.Tools {
		if tl.Name != tool {
			continue
		}
		raw, err := json.Marshal(tl.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s inputSchema: %v", tool, err)
		}
		var schema struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal %s inputSchema: %v; raw=%s", tool, err, raw)
		}
		prop, ok := schema.Properties[arg]
		if !ok {
			t.Fatalf("%s inputSchema has no %q property; raw=%s", tool, arg, raw)
		}
		if len(prop.Enum) == 0 {
			t.Fatalf("%s.%s publishes no enum; clients have nothing to complete from", tool, arg)
		}
		return prop.Enum
	}
	t.Fatalf("tools/list has no %q", tool)
	return nil
}

// completeFromEnum is the client-side half: filter the published enum by the
// typed prefix, case-insensitively, and sort. Spec §13 caps completions at 50
// entries; a closed enum is far below that, so no cap applies here.
func completeFromEnum(values []string, prefix string) []string {
	out := []string{}
	for _, v := range values {
		if strings.HasPrefix(strings.ToLower(v), strings.ToLower(prefix)) {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// assertSpecClosedEnumCompletions runs the three §13 cases plus the full
// enum sets the spec fixes. TestMCPCompletions calls it, because §13 names
// that test as the carrier of these assertions.
func assertSpecClosedEnumCompletions(t *testing.T, ctx context.Context, cs *sdkmcp.ClientSession) {
	t.Helper()

	lang := toolArgEnum(t, ctx, cs, "gum.code", "language")
	if !equalStringSlices(lang, []string{"risor"}) {
		t.Errorf("gum.code.language enum = %v; want [risor] (§4.3 closed v0.1 set)", lang)
	}
	// §4.3: the reserved names MUST NOT appear in the v0.1.0 input schema.
	for _, deferred := range []string{"starlark", "yaegi", "js", "python"} {
		for _, v := range lang {
			if v == deferred {
				t.Errorf("gum.code.language offers %q; reserved until its sandbox ships", deferred)
			}
		}
	}
	if got := completeFromEnum(lang, "r"); !equalStringSlices(got, []string{"risor"}) {
		t.Errorf("complete(gum.code.language, %q) = %v; want [risor]", "r", got)
	}
	if got := completeFromEnum(lang, "s"); len(got) != 0 {
		t.Errorf("complete(gum.code.language, %q) = %v; want [] in v0.1.0", "s", got)
	}

	wantFormat := []string{"toon", "csv", "json", "markdown"}
	for _, tool := range []string{"gum.read", "gum.write", "gum.destructive"} {
		format := toolArgEnum(t, ctx, cs, tool, "format")
		if !equalStringSlices(format, wantFormat) {
			t.Errorf("%s.format enum = %v; want %v", tool, format, wantFormat)
		}
		if got := completeFromEnum(format, "t"); !equalStringSlices(got, []string{"toon"}) {
			t.Errorf("complete(%s.format, %q) = %v; want [toon]", tool, "t", got)
		}
	}
}

// TestCompleteHasNoToolReferenceType pins why the closed-enum assertions run
// against the inputSchema instead of completion/complete: the protocol has no
// tool reference. When a future SDK adds one, this test fails and the tool
// arguments must be routed through handleComplete.
func TestCompleteHasNoToolReferenceType(t *testing.T) {
	defer goleak.VerifyNone(t)

	ref := &sdkmcp.CompleteReference{Type: "ref/tool", Name: "gum.code"}
	if _, err := json.Marshal(ref); err == nil {
		t.Error("CompleteReference marshalled a ref/tool; the SDK now carries tool completions, so wire them in handleComplete")
	}
	var decoded sdkmcp.CompleteReference
	if err := json.Unmarshal([]byte(`{"type":"ref/tool","name":"gum.code"}`), &decoded); err == nil {
		t.Error("CompleteReference accepted a ref/tool over the wire; route tool arguments in handleComplete")
	}
}
