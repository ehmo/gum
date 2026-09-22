package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// minProtocolVersion is the oldest MCP protocol version gum negotiates.
//
// MCP removed JSON-RPC batching in 2025-06-18. The pinned go-sdk transport
// still decodes a legacy batch frame on a session that negotiated an older
// version, and it dispatches every element with no limit on element count,
// decoded body size, or per-item params size, so a single frame can drive
// unbounded work. From 2025-06-18 on, the same transport refuses the frame
// before any handler runs. Spec §13.2 pins the gum contract to MCP
// 2025-11-25, so nothing below this floor was ever in contract.
const minProtocolVersion = "2025-06-18"

// rejectLegacyProtocol refuses an initialize whose protocolVersion predates
// minProtocolVersion. Protocol versions are ISO dates, so a lexical compare
// is a date compare.
func rejectLegacyProtocol(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
	return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
		if method != "initialize" {
			return next(ctx, method, req)
		}

		version := initializeProtocolVersion(req)
		if version != "" && version < minProtocolVersion {
			return nil, legacyProtocolError(version)
		}

		return next(ctx, method, req)
	}
}

// initializeProtocolVersion reads protocolVersion out of the initialize
// params. The SDK decodes those params into an unexported type, so this goes
// through JSON rather than a type assertion.
func initializeProtocolVersion(req sdkmcp.Request) string {
	raw, err := json.Marshal(req.GetParams())
	if err != nil {
		return ""
	}

	var probe struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}

	return probe.ProtocolVersion
}

// legacyProtocolError is returned bare, never wrapped. jsonrpc2.toWireError
// forwards a *jsonrpc.Error verbatim but rebuilds a wrapped one, keeping only
// its code and dropping Data, which would strip error_code off the wire.
func legacyProtocolError(version string) *jsonrpc.Error {
	data, _ := json.Marshal(map[string]any{
		"error_code":           string(dispatch.ErrCodeUnsupportedCapability),
		"min_protocol_version": minProtocolVersion,
		"user_message": fmt.Sprintf(
			"gum speaks MCP %s and later; this client offered %s. Upgrade the client.",
			minProtocolVersion, version),
	})
	return &jsonrpc.Error{
		Code: jsonrpc.CodeInvalidParams,
		Message: fmt.Sprintf("unsupported MCP protocol version %s; gum requires %s or later",
			version, minProtocolVersion),
		Data: data,
	}
}
