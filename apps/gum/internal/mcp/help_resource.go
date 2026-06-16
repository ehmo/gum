package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/embedded"
	"github.com/ehmo/gum/internal/help/topics"
)

const (
	helpTopicsURI      = "gum://help/topics"
	helpTopicTemplate  = "gum://help/{topic}"
	helpTopicURIPrefix = "gum://help/"

	// jsonRPCResourceNotFnd is the JSON-RPC error.code emitted on
	// RESOURCE_NOT_FOUND. Spec §13 line 1427 specifies -32004, but the
	// modelcontextprotocol/go-sdk reserves -32004 for jsonrpc2.ErrServerClosing
	// and translates any -32004 handler reply into a connection-close on the
	// client. We therefore use -32002 (the SDK's CodeResourceNotFound, which
	// also matches the upstream MCP 2025-11-25 wire spec) to keep the
	// application error inside the JSON-RPC error frame. The application-level
	// envelope still carries "error_code": "RESOURCE_NOT_FOUND" so spec §13
	// line 1427's "assert both the JSON-RPC error code and the
	// error.data.error_code" requirement is satisfied at the envelope level.
	// This divergence is tracked in docs/known-divergences.md.
	jsonRPCResourceNotFnd = -32002
)

// helpTopicRow mirrors one row in docs/help-topics.v1.json (embedded as
// HelpTopicsJSON). Re-declared locally so the package boundary remains
// dispatch-free.
type helpTopicRow struct {
	Topic              string `json:"topic"`
	Status             string `json:"status"`
	OneLineDescription string `json:"one_line_description"`
	RedirectTopic      string `json:"redirect_topic"`
}

// helpTopicsManifest is the embedded seed-set wrapper. schema_version is
// reserved for future ABI gates and ignored at v0.1.0.
type helpTopicsManifest struct {
	SchemaVersion int            `json:"schema_version"`
	Topics        []helpTopicRow `json:"topics"`
}

// registerHelpResources wires gum://help/topics (fixed URI, TOON body) and
// gum://help/{topic} (parameterised, markdown body) per spec §13.
func (s *Server) registerHelpResources() {
	// Build-time fail-safe: surface HELP_TOPIC_TOO_LARGE before any client
	// hits a truncated topic. Defensive — the same check runs in tests.
	if err := topics.ValidateSizes(); err != nil {
		panic(fmt.Sprintf("mcp: %v", err))
	}
	s.sdkSrv.AddResource(
		&sdkmcp.Resource{
			Name:        "gum_help_topics",
			Title:       "GUM help topics",
			Description: "TOON-rows enumeration of every help topic served by gum://help/{topic}.",
			URI:         helpTopicsURI,
			MIMEType:    "text/plain",
		},
		s.handleHelpTopicsList,
	)
	s.sdkSrv.AddResourceTemplate(
		&sdkmcp.ResourceTemplate{
			Name:        "gum_help_topic",
			Title:       "GUM help topic",
			Description: "Per-topic markdown help body. Topic identifier is the kebab-case slug listed by gum://help/topics.",
			URITemplate: helpTopicTemplate,
			MIMEType:    "text/markdown",
		},
		s.handleHelpTopicRead,
	)
}

// handleHelpTopicsList is the resources/read handler for the fixed
// gum://help/topics URI. It serialises the embedded manifest as a TOON
// document sorted lexicographically by topic identifier.
func (s *Server) handleHelpTopicsList(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	manifest, err := loadHelpManifest()
	if err != nil {
		return nil, resourceNotFoundError(req.Params.URI, "help topics manifest unavailable")
	}
	body := renderHelpTopicsTOON(manifest.Topics)
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{URI: req.Params.URI, MIMEType: "text/plain", Text: body},
		},
	}, nil
}

// handleHelpTopicRead is the resources/read handler for the parameterised
// gum://help/{topic} URI. Active topics return their embedded markdown body;
// deprecated topics return the §7 JSON-valued redirect shape; unknown topics
// produce the canonical RESOURCE_NOT_FOUND envelope (spec §13 line 1425).
func (s *Server) handleHelpTopicRead(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	uri := req.Params.URI
	name, ok := parseHelpTopicURI(uri)
	if !ok {
		return nil, resourceNotFoundError(uri, "")
	}
	manifest, err := loadHelpManifest()
	if err != nil {
		return nil, resourceNotFoundError(uri, name)
	}
	row, found := findTopicRow(manifest.Topics, name)
	if !found {
		return nil, resourceNotFoundError(uri, name)
	}
	if row.Status == "deprecated" {
		payload, _ := json.Marshal(map[string]any{
			"status":   "deprecated",
			"redirect": row.RedirectTopic,
		})
		return &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{
				{URI: uri, MIMEType: "application/json", Text: string(payload)},
			},
		}, nil
	}
	body, ok := topics.Read(name)
	if !ok {
		// Manifest says the topic exists but no markdown ships. The build-time
		// test catches this drift; at runtime we still return a clear error.
		return nil, resourceNotFoundError(uri, name)
	}
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{URI: uri, MIMEType: "text/markdown", Text: string(body)},
		},
	}, nil
}

func loadHelpManifest() (*helpTopicsManifest, error) {
	var m helpTopicsManifest
	if err := json.Unmarshal(embedded.HelpTopicsJSON, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func findTopicRow(rows []helpTopicRow, name string) (helpTopicRow, bool) {
	for _, r := range rows {
		if r.Topic == name {
			return r, true
		}
	}
	return helpTopicRow{}, false
}

// parseHelpTopicURI accepts `gum://help/<topic>` and returns the topic slug.
// Rejects gum://help/topics (reserved for the list resource), embedded
// slashes/queries/fragments, and non-kebab-lowercase identifiers.
func parseHelpTopicURI(uri string) (string, bool) {
	if !strings.HasPrefix(uri, helpTopicURIPrefix) {
		return "", false
	}
	tail := strings.TrimPrefix(uri, helpTopicURIPrefix)
	if tail == "" || tail == "topics" {
		return "", false
	}
	if strings.ContainsAny(tail, "/?#") {
		return "", false
	}
	for _, c := range tail {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return "", false
		}
	}
	return tail, true
}

// renderHelpTopicsTOON serialises the manifest as a TOON document. The
// header carries the synthetic op/variant identifiers chosen so the body
// passes the in-tree TOON validator without re-using any real op_id.
func renderHelpTopicsTOON(rows []helpTopicRow) string {
	sorted := append([]helpTopicRow(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Topic < sorted[j].Topic })

	var b strings.Builder
	b.WriteString("op: gum.help.topics\n")
	b.WriteString("variant: gum.help.topics.v1\n")
	b.WriteString("format_version: 1\n")
	b.WriteString("fields: topic,status,one_line_description,redirect_topic\n")
	fmt.Fprintf(&b, "count: %d\n", len(sorted))
	b.WriteString("\n")
	for _, r := range sorted {
		b.WriteString(csvField(r.Topic))
		b.WriteByte(',')
		b.WriteString(csvField(r.Status))
		b.WriteByte(',')
		b.WriteString(csvField(r.OneLineDescription))
		b.WriteByte(',')
		b.WriteString(csvField(r.RedirectTopic))
		b.WriteByte('\n')
	}
	return b.String()
}

// csvField applies the spec §9.0 TOON quoting rule: bare unless the value
// contains a comma, double quote, or newline; otherwise wrapped in double
// quotes with internal `"` escaped as `""`.
func csvField(s string) string {
	if s == "" {
		return ""
	}
	if !strings.ContainsAny(s, ",\"\n") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// resourceNotFoundError builds the spec §13 line 1425 RESOURCE_NOT_FOUND
// envelope wrapped in JSON-RPC (-32004). The envelope is the same for both
// gum://help/topics-list failure and gum://help/<unknown> not-found.
func resourceNotFoundError(uri, topic string) *jsonrpc.Error {
	envelope := map[string]any{
		"error_code":   "RESOURCE_NOT_FOUND",
		"uri":          uri,
		"user_message": fmt.Sprintf("Resource not found: %s.", uri),
		"suggestion":   "Call resources/read on gum://help/topics for valid help topics when the URI starts with gum://help/.",
	}
	if topic != "" {
		envelope["topic"] = topic
	}
	data, _ := json.Marshal(envelope)
	return &jsonrpc.Error{
		Code:    jsonRPCResourceNotFnd,
		Message: "Resource not found",
		Data:    data,
	}
}
