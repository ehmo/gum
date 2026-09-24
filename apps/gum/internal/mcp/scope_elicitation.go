// Package-internal MCP elicitation for the spec §13 managed-scope re-consent
// flow (docs/test-matrix.md).
//
// A scoped operation refused with SCOPE_MISSING is a dead end for an agent:
// only a human at a Google consent screen can widen the grant. When the client
// declares the elicitation capability, gum answers that refusal with ONE form
// instead: the approval object the kernel minted, showing the op, the exact
// scopes, the profile, the account the profile is bound to, and the request
// hash. An accepted, approved form runs the consent; anything else leaves the
// original SCOPE_MISSING envelope in place.
//
// The kernel owns the decision (internal/dispatch/scope_upgrade.go). This file
// owns only the wire mechanics, per spec §14: build the form, read the reply,
// hand both to the kernel.
//
// Two SDK facts shape the code. An InputRequests result sent to a client that
// cannot elicit fails the whole tool call, so the capability is checked first
// and the flow silently stays off when it is absent. And the SDK re-invokes
// this handler with the reply attached, so the refusal is recomputed from live
// state on the retry: the approval is checked against a freshly derived
// request hash, not against anything the client could choose.

package mcp

import (
	"context"
	"errors"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/dispatch"
)

// scopeUpgradeIDPrefix namespaces gum's re-consent input request. The request
// hash is appended, so a reply minted for one call is never read back as the
// reply to another: a different call asks under a different key.
const scopeUpgradeIDPrefix = "gum.scope_upgrade."

// scopeUpgradeInputRequestID is the InputRequests key for one approval.
func scopeUpgradeInputRequestID(requestHash string) string {
	return scopeUpgradeIDPrefix + requestHash
}

// scopeUpgradeResult implements the §13 flow for a failed dispatch. handled is
// false for every case that is not a live re-consent, and the caller then
// returns the original failure unchanged: a non-SCOPE_MISSING error, a
// dispatcher with no consent wired, a client that cannot elicit, and every
// refusal outcome including decline, cancel and a binding mismatch.
func (s *Server) scopeUpgradeResult(ctx context.Context, req *sdkmcp.CallToolRequest, inv *dispatch.Invocation, dispatchErr error) (*sdkmcp.CallToolResult, bool) {
	if req == nil || req.Session == nil || inv == nil {
		return nil, false
	}
	var se *dispatch.StructuredError
	if !errors.As(dispatchErr, &se) || se.ErrCode != dispatch.ErrCodeScopeMissing {
		return nil, false
	}
	upgrader, ok := s.disp.(dispatch.ScopeUpgrader)
	if !ok {
		return nil, false
	}
	upReq, ok := upgrader.ScopeUpgradeFor(inv, se)
	if !ok {
		return nil, false
	}

	id := scopeUpgradeInputRequestID(upReq.RequestHash)
	if res := scopeUpgradeReplyFor(req, id); res != nil {
		outcome := upgrader.ApplyScopeUpgrade(ctx, upReq, scopeUpgradeReply(res))
		if outcome == nil {
			return nil, false
		}
		// §13: report the grant and stop. The caller re-issues the original
		// operation itself; gum never replays it on the operator's behalf.
		return jsonResult(outcome), true
	}

	if !clientSupportsFormElicitation(req) {
		return nil, false
	}
	if req.Params != nil && len(req.Params.InputResponses) > 0 {
		// A retry that carried other input responses but not ours. Asking
		// again would spin the SDK retry loop, so take it as a refusal.
		return nil, false
	}
	return &sdkmcp.CallToolResult{
		InputRequests: sdkmcp.InputRequestMap{id: scopeUpgradeElicitParams(upReq)},
	}, true
}

// clientSupportsFormElicitation reports whether this request's client declared
// form elicitation. The rule mirrors ServerSession.Elicit: no elicitation
// capability at all means no, and a client that declared URL elicitation only
// means no. Both capabilities absent inside a declared elicitation block means
// form, which is the SDK's backward-compatible reading.
func clientSupportsFormElicitation(req *sdkmcp.CallToolRequest) bool {
	caps := req.ClientCapabilities()
	if caps == nil || caps.Elicitation == nil {
		return false
	}
	return caps.Elicitation.Form != nil || caps.Elicitation.URL == nil
}

// scopeUpgradeReplyFor returns the elicitation reply this request carries for
// id, or nil when it carries none.
func scopeUpgradeReplyFor(req *sdkmcp.CallToolRequest, id string) *sdkmcp.ElicitResult {
	if req.Params == nil || len(req.Params.InputResponses) == 0 {
		return nil
	}
	res, ok := req.Params.InputResponses[id].(*sdkmcp.ElicitResult)
	if !ok {
		return nil
	}
	return res
}

// scopeUpgradeReply converts an elicitation result into the kernel's reply.
// Every binding the client echoed is carried through for comparison; a field
// the client dropped arrives empty and is not compared.
func scopeUpgradeReply(res *sdkmcp.ElicitResult) dispatch.ScopeUpgradeReply {
	reply := dispatch.ScopeUpgradeReply{Action: res.Action}
	if res.Action != dispatch.ScopeUpgradeAccept {
		return reply
	}
	approve, _ := res.Content["approve"].(bool)
	reply.Approved = approve
	reply.OpID = contentString(res.Content, "op_id")
	reply.VariantID = contentString(res.Content, "variant_id")
	reply.Profile = contentString(res.Content, "profile")
	reply.RequestHash = contentString(res.Content, "request_hash")
	if scopes := contentString(res.Content, "required_scopes"); scopes != "" {
		reply.RequiredScopes = strings.Fields(scopes)
	}
	return reply
}

// contentString reads a string field out of an elicitation form's content.
func contentString(content map[string]any, key string) string {
	s, _ := content[key].(string)
	return s
}

// scopeUpgradeElicitParams builds the §13 approval form.
//
// Only `approve` is required. The bindings are read-only echoes carrying
// `default`, and a required field with a default would still be rejected when
// the client omits it: the SDK validates the reply BEFORE it applies defaults.
// So they are optional, and the kernel compares only what came back.
func scopeUpgradeElicitParams(req dispatch.ScopeUpgradeRequest) *sdkmcp.ElicitParams {
	scopes := strings.Join(req.RequiredScopes, " ")
	properties := map[string]any{
		"approve": map[string]any{
			"type":        "boolean",
			"title":       "Grant these scopes",
			"description": "Approve to open a Google consent screen for exactly the scopes listed above. Declining changes nothing.",
		},
		"op_id":           bindingProperty("Operation", req.OpID),
		"variant_id":      bindingProperty("Variant", req.VariantID),
		"profile":         bindingProperty("Profile", req.Profile),
		"required_scopes": bindingProperty("Scopes requested", scopes),
		"request_hash":    bindingProperty("Request hash", req.RequestHash),
	}
	if req.ExpectedSubject != "" {
		properties["expected_subject"] = bindingProperty("Expected account", req.ExpectedSubject)
	}
	return &sdkmcp.ElicitParams{
		Mode:    "form",
		Message: scopeUpgradeMessage(req, scopes),
		RequestedSchema: map[string]any{
			"type":       "object",
			"properties": properties,
			"required":   []string{"approve"},
		},
	}
}

// bindingProperty renders one read-only binding: a string field whose default
// is the value being approved, so a client that shows the form shows the
// binding and a client that submits a bare `approve` still round-trips it.
func bindingProperty(title, value string) map[string]any {
	return map[string]any{
		"type":        "string",
		"title":       title,
		"default":     value,
		"description": "Bound to this approval. Do not change.",
	}
}

// scopeUpgradeMessage states every binding in the prompt itself. A client that
// renders only the message, and not the schema, still shows the operator what
// the approval covers.
func scopeUpgradeMessage(req dispatch.ScopeUpgradeRequest, scopes string) string {
	var b strings.Builder
	b.WriteString("gum needs wider Google access to run ")
	b.WriteString(req.OpID)
	b.WriteString(".\n\nOperation: ")
	b.WriteString(req.OpID)
	b.WriteString("\nVariant: ")
	b.WriteString(req.VariantID)
	b.WriteString("\nProfile: ")
	b.WriteString(req.Profile)
	b.WriteString("\nScopes requested: ")
	b.WriteString(scopes)
	if req.ExpectedSubject != "" {
		b.WriteString("\nExpected account: ")
		b.WriteString(req.ExpectedSubject)
	}
	b.WriteString("\nRequest hash: ")
	b.WriteString(req.RequestHash)
	b.WriteString("\n\nApproving opens a Google consent screen for exactly these scopes. ")
	b.WriteString("The grant is refused if the consent comes back short of them, or on a different account. ")
	b.WriteString("The operation is not re-run for you: re-issue it after the grant is recorded.")
	return b.String()
}
