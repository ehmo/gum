package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ehmo/gum/internal/output/tee"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// resultsResourceTemplate is the public RFC 6570 URI template for
// gum://results/{hash}; advertised in resources/templates/list (spec §13).
const resultsResourceTemplate = "gum://results/{hash}"

// resultsScheme + resultsHost form the literal prefix accepted at runtime.
// We deliberately reject schemes or hosts that do not match exactly so a
// stray `https://results/...` cannot collide with the template.
const resultsURIPrefix = "gum://results/"

// resultsScanMaxDaysDefault is the v0.1.0 default reverse-lookup window:
// 24 hours rounded up to a 2-day scan to cover the UTC-day boundary the
// artifact path uses.
const resultsScanMaxDaysDefault = 2

// jsonRPCResultArtifactExpired is the spec §13 normative JSON-RPC error
// code for `RESULT_ARTIFACT_EXPIRED`. Distinct from the SDK's -32002 for
// generic resource-not-found so that clients can branch on staleness.
const jsonRPCResultArtifactExpired = -32010

// registerResultsResource attaches the gum://results/{hash} resource
// template handler to the SDK server. Capabilities advertised here are
// merged with the existing Tools capability in NewServerWithCatalog.
func (s *Server) registerResultsResource() {
	s.sdkSrv.AddResourceTemplate(
		&sdkmcp.ResourceTemplate{
			Name:        "gum_result_artifact",
			Title:       "GUM result artifact",
			Description: "Recovery handle for a lossy expression-profile result. URI is gum://results/<hash>. Resolved by directory-scan against the active profile's tee directory. Returns RESULT_ARTIFACT_EXPIRED when the artifact has been pruned (default 24h retention).",
			URITemplate: resultsResourceTemplate,
			MIMEType:    "application/json",
		},
		s.handleResultsResource,
	)
}

// handleResultsResource is the resources/read handler for the
// gum://results/{hash} template. Successful reads return exactly one text
// resource content item containing the decompressed JSON payload (spec §13
// "JSON-valued GUM resources"); failed reads return a JSON-RPC error with
// code -32010 and structured error.data matching the §1423 envelope.
func (s *Server) handleResultsResource(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	uri := req.Params.URI
	hash, ok := parseResultsURI(uri)
	if !ok {
		return nil, expiredArtifactError(uri, "")
	}
	profileDir := s.profileDataDir()
	if profileDir == "" {
		return nil, expiredArtifactError(uri, hash)
	}
	path, ok, err := tee.FindArtifact(profileDir, hash, resultsScanMaxDaysDefault)
	if err != nil || !ok {
		return nil, expiredArtifactError(uri, hash)
	}
	body, err := tee.Read(path)
	if err != nil {
		return nil, expiredArtifactError(uri, hash)
	}
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{
				URI:      uri,
				MIMEType: "application/json",
				Text:     string(body),
			},
		},
	}, nil
}

// parseResultsURI accepts `gum://results/<hash>` and returns the hash.
// Returns ("", false) for any malformed URI: wrong scheme, embedded path
// segments, query/fragment components, or empty hash.
func parseResultsURI(uri string) (string, bool) {
	if !strings.HasPrefix(uri, resultsURIPrefix) {
		return "", false
	}
	hash := strings.TrimPrefix(uri, resultsURIPrefix)
	if hash == "" {
		return "", false
	}
	// Reject embedded slashes / queries / fragments — the spec template is
	// exactly `gum://results/{hash}` with `hash` as a single path component.
	if strings.ContainsAny(hash, "/?#") {
		return "", false
	}
	// Accept only hex hashes (HMAC-SHA-256 output is 64 lowercase hex chars,
	// but allow any positive-length lowercase hex run for forward
	// compatibility — corruption is caught by FindArtifact returning miss).
	for _, c := range hash {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		default:
			return "", false
		}
	}
	return hash, true
}

// expiredArtifactError builds the §1423 RESULT_ARTIFACT_EXPIRED envelope
// wrapped in a JSON-RPC error (code -32010). The envelope carries error_code,
// hash, uri, expires_at (null in v0.1.0 — we don't persist artifact
// metadata), user_message, and suggestion.
func expiredArtifactError(uri, hash string) *jsonrpc.Error {
	envelope := map[string]any{
		"error_code":   "RESULT_ARTIFACT_EXPIRED",
		"hash":         hash,
		"uri":          uri,
		"expires_at":   nil,
		"user_message": "Result artifact expired; re-issue the originating operation to obtain a fresh result handle.",
		"suggestion":   "Re-issue the originating operation to obtain a fresh result handle.",
	}
	data, _ := json.Marshal(envelope)
	return &jsonrpc.Error{
		Code:    jsonRPCResultArtifactExpired,
		Message: "Result artifact expired",
		Data:    data,
	}
}

// profileDataDir returns the absolute path of <data home>/gum/<profile>/,
// honouring XDG_DATA_HOME with a fallback to $HOME/.local/share. Returns
// "" when no home dir is resolvable — the caller maps that to
// RESULT_ARTIFACT_EXPIRED rather than leaking the underlying error.
func (s *Server) profileDataDir() string {
	dir, err := s.profile.DataDir()
	if err != nil {
		return ""
	}
	return dir
}
