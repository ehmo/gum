package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func rawSkillToolArgs(s string) json.RawMessage { return json.RawMessage(s) }

func TestSkillsHandlersDirectBranches(t *testing.T) {
	s := NewServer(noopDispatcher{})

	listErr, err := s.handleSkillsList(context.Background(), &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Arguments: rawSkillToolArgs(`{"extra":true}`)},
	})
	if err != nil {
		t.Fatalf("handleSkillsList: %v", err)
	}
	if !listErr.IsError || !strings.Contains(textOfResult(t, listErr), "INVALID_ARGS") {
		t.Fatalf("listErr=%#v text=%q", listErr, textOfResult(t, listErr))
	}

	cases := []string{
		`{`,
		`{}`,
		`{"name":"core","version":"v1.0.0"}`,
		`{"name":"core","max_bytes":-1}`,
		`{"name":"missing"}`,
		`{"name":"core","version":"9.9.9"}`,
	}
	for _, body := range cases {
		res, err := s.handleSkillsGet(context.Background(), &sdkmcp.CallToolRequest{
			Params: &sdkmcp.CallToolParamsRaw{Arguments: rawSkillToolArgs(body)},
		})
		if err != nil {
			t.Fatalf("handleSkillsGet(%s): %v", body, err)
		}
		if !res.IsError {
			t.Fatalf("handleSkillsGet(%s) IsError=false text=%q", body, textOfResult(t, res))
		}
	}
}

func textOfResult(t *testing.T, res *sdkmcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	text, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0]=%T", res.Content[0])
	}
	return text.Text
}
