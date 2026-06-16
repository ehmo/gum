// Package mcp — coverage push for handlers.go pure helpers and small
// dispatch surfaces (handleWrite, handleUnknown, intArg, stringFromMeta,
// profileNameForOp). Each test drives one helper directly without any
// real subprocess or transport — the helpers are pure or thin shims over
// catalog/snapshot data.
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/catalog"
)

// TestIntArg covers every code branch: float64 (default JSON number type),
// int (when args are constructed in-Go), json.Number (when args came from a
// decoder with UseNumber), and the default fallback for missing/wrong types.
func TestIntArg(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		key  string
		def  int
		want int
	}{
		{"float64", map[string]any{"n": float64(5)}, "n", 99, 5},
		{"int", map[string]any{"n": 7}, "n", 99, 7},
		{"json_number", map[string]any{"n": json.Number("11")}, "n", 99, 11},
		{"missing_key_returns_default", map[string]any{}, "n", 42, 42},
		{"wrong_type_returns_default", map[string]any{"n": "abc"}, "n", 42, 42},
		{"bad_json_number_returns_default", map[string]any{"n": json.Number("not-a-num")}, "n", 42, 42},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := intArg(tc.args, tc.key, tc.def); got != tc.want {
				t.Errorf("intArg(%v, %q) = %d; want %d", tc.args, tc.key, got, tc.want)
			}
		})
	}
}

// TestStringFromMeta covers the four guard branches: nil request, nil
// params, nil meta map, missing key — all "" — plus the happy path that
// returns the string value, and the wrong-type branch that returns "".
func TestStringFromMeta(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		if got := stringFromMeta(nil, "k"); got != "" {
			t.Errorf("got %q; want \"\"", got)
		}
	})
	t.Run("nil_params", func(t *testing.T) {
		req := &sdkmcp.CallToolRequest{}
		if got := stringFromMeta(req, "k"); got != "" {
			t.Errorf("got %q; want \"\"", got)
		}
	})
	t.Run("nil_meta_map", func(t *testing.T) {
		req := &sdkmcp.CallToolRequest{Params: &sdkmcp.CallToolParamsRaw{}}
		if got := stringFromMeta(req, "k"); got != "" {
			t.Errorf("got %q; want \"\"", got)
		}
	})
	t.Run("missing_key", func(t *testing.T) {
		req := &sdkmcp.CallToolRequest{Params: &sdkmcp.CallToolParamsRaw{Meta: map[string]any{}}}
		if got := stringFromMeta(req, "k"); got != "" {
			t.Errorf("got %q; want \"\"", got)
		}
	})
	t.Run("happy_path_returns_string", func(t *testing.T) {
		req := &sdkmcp.CallToolRequest{Params: &sdkmcp.CallToolParamsRaw{Meta: map[string]any{"gumRoot": "/abs/path"}}}
		if got := stringFromMeta(req, "gumRoot"); got != "/abs/path" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("wrong_type_returns_empty", func(t *testing.T) {
		req := &sdkmcp.CallToolRequest{Params: &sdkmcp.CallToolParamsRaw{Meta: map[string]any{"gumRoot": 42}}}
		if got := stringFromMeta(req, "gumRoot"); got != "" {
			t.Errorf("got %q; want \"\"", got)
		}
	})
}

// TestProfileNameForOp covers the three return paths: unknown op → "",
// op without a matching default variant → "", and the happy path that
// returns the variant's OutputProfile.
func TestProfileNameForOp(t *testing.T) {
	t.Run("nil_snapshot_returns_empty", func(t *testing.T) {
		s := &Server{}
		if got := s.profileNameForOp("any"); got != "" {
			t.Errorf("got %q; want \"\"", got)
		}
	})

	t.Run("unknown_op_returns_empty", func(t *testing.T) {
		op := minimalReadOp("known.op", catalog.RiskClassRead)
		s := &Server{snapshot: minimalCatalog(op)}
		if got := s.profileNameForOp("unknown.op"); got != "" {
			t.Errorf("got %q; want \"\"", got)
		}
	})

	t.Run("default_variant_missing_returns_empty", func(t *testing.T) {
		// Op whose DefaultVariantID does not match any Variant.VariantID.
		op := catalog.Op{
			OpID:             "bad.default",
			DefaultVariantID: "missing",
			Variants: []catalog.Variant{
				{VariantID: "actual.v1", OutputProfile: "x"},
			},
		}
		s := &Server{snapshot: minimalCatalog(op)}
		if got := s.profileNameForOp("bad.default"); got != "" {
			t.Errorf("got %q; want \"\" (no matching variant)", got)
		}
	})

	t.Run("happy_path_returns_output_profile", func(t *testing.T) {
		op := minimalReadOp("gmail.list", catalog.RiskClassRead)
		op.Variants[0].OutputProfile = "gmail.profile.v1"
		s := &Server{snapshot: minimalCatalog(op)}
		if got := s.profileNameForOp("gmail.list"); got != "gmail.profile.v1" {
			t.Errorf("got %q; want gmail.profile.v1", got)
		}
	})
}

// TestHandleUnknown asserts the catch-all meta-tool returns
// META_TOOL_NOT_IMPLEMENTED carrying the provided name — the safety net
// when a tool is registered but no handler is wired.
func TestHandleUnknown(t *testing.T) {
	s := &Server{}
	h := s.handleUnknown("gum.future_tool")
	res, err := h(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleUnknown returned error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected isError=true")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	if !strings.Contains(tc.Text, "META_TOOL_NOT_IMPLEMENTED") {
		t.Errorf("text=%q; missing META_TOOL_NOT_IMPLEMENTED", tc.Text)
	}
	if !strings.Contains(tc.Text, "gum.future_tool") {
		t.Errorf("text=%q; missing tool name", tc.Text)
	}
}

// TestHandleWriteRiskMismatchForReadOp drives handleWrite end-to-end.
// Calling gum.write with a read-class op must short-circuit to the
// RISK_TOOL_MISMATCH envelope with required_tool="gum.read", proving the
// handler is wired to handleRiskTier with RiskClassWrite as the want
// argument.
func TestHandleWriteRiskMismatchForReadOp(t *testing.T) {
	const opID = "gmail.read.op"
	op := minimalReadOp(opID, catalog.RiskClassRead)
	snap := minimalCatalog(op)
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, snap)

	raw, _ := json.Marshal(map[string]any{"op_id": opID})
	req := &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Name: "gum.write", Arguments: raw},
	}
	res, err := srv.handleWrite(context.Background(), req)
	if err != nil {
		t.Fatalf("handleWrite returned go err: %v", err)
	}
	m := parseErrorResult(t, res)
	if m["error_code"] != "RISK_TOOL_MISMATCH" {
		t.Errorf("error_code=%v; want RISK_TOOL_MISMATCH", m["error_code"])
	}
	if m["required_tool"] != "gum.read" {
		t.Errorf("required_tool=%v; want gum.read", m["required_tool"])
	}
	if len(cd.Calls) != 0 {
		t.Errorf("dispatcher called %d time(s); want 0 on risk mismatch", len(cd.Calls))
	}
}
