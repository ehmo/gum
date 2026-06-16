package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/jcs"
)

const (
	opResourceTemplate     = "gum://op/{id}"
	variantResourceTemplate = "gum://variant/{id}"
	schemaResourceTemplate = "gum://schema/{ref}"
	pluginResourceTemplate = "gum://plugin/{name}"

	opURIPrefix     = "gum://op/"
	variantURIPrefix = "gum://variant/"
	schemaURIPrefix = "gum://schema/"
	pluginURIPrefix = "gum://plugin/"

	mimeApplicationJSON   = "application/json"
	mimeApplicationSchema = "application/schema+json"
)

// registerResourceTemplates wires the four §13 parameterized resource
// templates whose handlers are owned by this file. The two pre-existing
// templates (gum://results/{hash} via registerResultsResource and
// gum://help/{topic} via registerHelpResources) are registered separately;
// together the six templates satisfy spec §13 line 3168's registration
// invariant.
func (s *Server) registerResourceTemplates() {
	s.sdkSrv.AddResourceTemplate(
		&sdkmcp.ResourceTemplate{
			Name:        "gum_op",
			Title:       "GUM operation record",
			Description: "Untruncated op record with all variants and full schema refs (spec §13 line 3154). JCS-canonical JSON.",
			URITemplate: opResourceTemplate,
			MIMEType:    mimeApplicationJSON,
		},
		s.handleOpRead,
	)
	s.sdkSrv.AddResourceTemplate(
		&sdkmcp.ResourceTemplate{
			Name:        "gum_variant",
			Title:       "GUM variant record",
			Description: "Resolved variant record for exact TOON reconstruction (spec §13 line 3155). JCS-canonical JSON.",
			URITemplate: variantResourceTemplate,
			MIMEType:    mimeApplicationJSON,
		},
		s.handleVariantRead,
	)
	s.sdkSrv.AddResourceTemplate(
		&sdkmcp.ResourceTemplate{
			Name:        "gum_schema",
			Title:       "GUM JSON Schema document",
			Description: "Full JSON Schema 2020-12 body served by the embedded gen/schemas/ store or the profile-local plugin-schemas/ copy (spec §13 line 3156). Refs absent from the active snapshot, owned by inactive/quarantined plugins, or violating the §8.2 safe served-ref grammar return RESOURCE_NOT_FOUND.",
			URITemplate: schemaResourceTemplate,
			MIMEType:    mimeApplicationSchema,
		},
		s.handleSchemaRead,
	)
	s.sdkSrv.AddResourceTemplate(
		&sdkmcp.ResourceTemplate{
			Name:        "gum_plugin",
			Title:       "GUM plugin metadata",
			Description: "Per-plugin metadata assembled from plugin-state.json + plugins.lock for the active profile (spec §13 lines 3158-3164).",
			URITemplate: pluginResourceTemplate,
			MIMEType:    mimeApplicationJSON,
		},
		s.handlePluginRead,
	)
}

// handleOpRead resolves gum://op/{id} against the active catalog snapshot
// and, on a snapshot miss, against the profile's plugin-catalog.json
// inventory. Spec §13 line 3179 branches the inventory hit by the owning
// plugin's status: active → full record; installed_pending_restart /
// needs_configuration → status-only schema; quarantined → VARIANT_QUARANTINED.
// Misses on both surfaces return RESOURCE_NOT_FOUND.
func (s *Server) handleOpRead(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	uri := req.Params.URI
	id, ok := parseTemplateParam(uri, opURIPrefix)
	if !ok {
		return nil, resourceTemplateNotFound(uri, "op_id grammar rejected before lookup")
	}
	if op := findOp(s.snapshot, id); op != nil {
		body, err := jcs.Marshal(op)
		if err != nil {
			return nil, resourceTemplateNotFound(uri, "jcs canonicalisation failed")
		}
		return jsonResourceResult(uri, body), nil
	}
	if resp, rpcErr, ok := s.inactivePluginOpResponse(uri, id); ok {
		if rpcErr != nil {
			return nil, rpcErr
		}
		if resp != nil {
			return resp, nil
		}
	}
	return nil, resourceTemplateNotFound(uri, "op_id "+id+" not in active catalog snapshot or plugin inventory")
}

// handleVariantRead resolves gum://variant/{id} by linear-scanning every op's
// variants in the active snapshot, then falling back to the plugin inventory.
// Spec §13 line 3155 branches the inventory hit symmetrically to handleOpRead.
func (s *Server) handleVariantRead(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	uri := req.Params.URI
	id, ok := parseTemplateParam(uri, variantURIPrefix)
	if !ok {
		return nil, resourceTemplateNotFound(uri, "variant_id grammar rejected before lookup")
	}
	if op, variant := findVariant(s.snapshot, id); variant != nil {
		payload := map[string]any{
			"op_id":   op.OpID,
			"variant": variant,
		}
		body, err := jcs.Marshal(payload)
		if err != nil {
			return nil, resourceTemplateNotFound(uri, "jcs canonicalisation failed")
		}
		return jsonResourceResult(uri, body), nil
	}
	if resp, rpcErr, ok := s.inactivePluginVariantResponse(uri, id); ok {
		if rpcErr != nil {
			return nil, rpcErr
		}
		if resp != nil {
			return resp, nil
		}
	}
	return nil, resourceTemplateNotFound(uri, "variant_id "+id+" not in active catalog snapshot or plugin inventory")
}

// jsonResourceResult wraps a JCS-marshalled body in the canonical one-content
// MCP read response. Shared by the snapshot-hit and inactive-plugin paths.
func jsonResourceResult(uri string, body []byte) *sdkmcp.ReadResourceResult {
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{URI: uri, MIMEType: mimeApplicationJSON, Text: string(body)},
		},
	}
}

// handleSchemaRead handles gum://schema/{ref} by delegating to
// resolveSchemaBody in schema_resource.go. The §13 line 3156 resolution
// chain (grammar check → active first-party snapshot → active plugin
// inventory → RESOURCE_NOT_FOUND) lives there; this handler owns only the
// MCP wire shape (one resource-content item with mimeType
// application/schema+json).
func (s *Server) handleSchemaRead(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	uri := req.Params.URI
	rawRef, ok := parseTemplateParam(uri, schemaURIPrefix)
	if !ok {
		return nil, resourceTemplateNotFound(uri, "schema ref grammar rejected before lookup")
	}
	ref := decodeSchemaRef(rawRef)
	body, detail, ok := s.resolveSchemaBody(ref)
	if !ok {
		return nil, resourceTemplateNotFound(uri, detail)
	}
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{URI: uri, MIMEType: mimeApplicationSchema, Text: string(body)},
		},
	}, nil
}

// handlePluginRead resolves gum://plugin/{name} per spec §13 lines 3158-3166.
// Returns the full record assembled by loadPluginResourceRecord — see
// plugin_resource.go for the three-source precedence. Plugins whose
// gum://plugins TOON row is filtered (installed_pending_restart) remain
// addressable here because the operator may know the name via stderr from
// `gum plugin install`.
func (s *Server) handlePluginRead(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	uri := req.Params.URI
	name, ok := parseTemplateParam(uri, pluginURIPrefix)
	if !ok {
		return nil, resourceTemplateNotFound(uri, "plugin name grammar rejected before lookup")
	}
	rec, ok := s.loadPluginResourceRecord(name)
	if !ok {
		return nil, resourceTemplateNotFound(uri, "plugin "+name+" not installed in active profile")
	}
	body, err := jcs.Marshal(rec)
	if err != nil {
		return nil, resourceTemplateNotFound(uri, "jcs canonicalisation failed")
	}
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{URI: uri, MIMEType: mimeApplicationJSON, Text: string(body)},
		},
	}, nil
}

// parseTemplateParam strips prefix from uri and returns the remaining
// identifier when it is non-empty and contains no path or query separators.
// The grammar matches the spec §8.2 safe-served-ref shape closely enough for
// v0.1.0: only printable ASCII (no '/', '?', '#'), at least one byte, length
// capped at 256.
func parseTemplateParam(uri, prefix string) (string, bool) {
	if !strings.HasPrefix(uri, prefix) {
		return "", false
	}
	tail := strings.TrimPrefix(uri, prefix)
	if tail == "" || len(tail) > 256 {
		return "", false
	}
	if strings.ContainsAny(tail, "/?#") {
		return "", false
	}
	return tail, true
}

// findOp returns the matching catalog op or nil. Linear scan is acceptable at
// the v0.1.0 catalog size; an index lands when catalog generation grows past
// ~10k ops.
func findOp(c *catalog.Catalog, id string) *catalog.Op {
	if c == nil {
		return nil
	}
	for i := range c.Ops {
		if c.Ops[i].OpID == id {
			return &c.Ops[i]
		}
	}
	return nil
}

// findVariant returns the (parent op, variant) pair matching id, or nil/nil.
func findVariant(c *catalog.Catalog, id string) (*catalog.Op, *catalog.Variant) {
	if c == nil {
		return nil, nil
	}
	for i := range c.Ops {
		op := &c.Ops[i]
		for j := range op.Variants {
			if op.Variants[j].VariantID == id {
				return op, &op.Variants[j]
			}
		}
	}
	return nil, nil
}

// resourceTemplateNotFound builds the canonical RESOURCE_NOT_FOUND envelope.
// JSON-RPC code -32002 (matching the SDK's CodeResourceNotFound) is used in
// place of the spec-defined -32004; see docs/known-divergences.md for the
// SDK-collision rationale.
func resourceTemplateNotFound(uri, detail string) *jsonrpc.Error {
	envelope := map[string]any{
		"error_code":   "RESOURCE_NOT_FOUND",
		"uri":          uri,
		"user_message": "Resource not found: " + uri + ".",
	}
	if detail != "" {
		envelope["detail"] = detail
	}
	data, _ := json.Marshal(envelope)
	return &jsonrpc.Error{
		Code:    jsonRPCResourceNotFnd,
		Message: "Resource not found",
		Data:    data,
	}
}
