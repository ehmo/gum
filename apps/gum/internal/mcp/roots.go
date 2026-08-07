// Package-internal MCP roots negotiation for §9.2 project-local profile
// resolution. Spec contract:
//
//   - obtain roots/list once per session and cache file:// URIs
//   - 1 file root → that root is the project root (gumRoot optional)
//   - >1 file roots → request MUST carry _meta.gumRoot matching one
//   - 0 roots / no client support → project-local lookup disabled
//   - non-file roots are filtered out; non-file _meta.gumRoot is rejected
//
// MCP 2026-07-28 (SEP-2322) forbids a server from sending roots/list while it
// serves a request: ServerSession.ListRoots now fails outright on any session
// negotiated at that revision. gum therefore asks the only way the protocol
// still allows — a tools/call result carrying an InputRequests map. The SDK
// fulfils it (client-side for 2026-07-28 clients, server-side for older ones),
// then re-invokes the tool handler with the reply in
// CallToolParamsRaw.InputResponses, so one code path serves both revisions.
// The cost is one extra round trip on the first tool call of a session.
//
// The PROJECT_ROOT_REQUIRED error code lives in internal/dispatch/errors.go;
// the envelope shape mirrors spec §1421 with reason / negotiated_roots /
// supplied_root fields for operator-friendly diagnosis.
//
// SEP-2577 deprecates the roots feature as a whole at revision 2026-07-28, so
// the go-sdk marks every roots symbol deprecated. gum keeps roots for v1: spec
// §9.2 binds project-local profile lookup to them, and the SEP guarantees at
// least twelve more months of function. Both suppressions below say so, and
// both are needed: CI runs staticcheck directly, which reads only
// //lint:file-ignore, and golangci-lint reads only //nolint. They go away with
// this file when §9.2 moves to an explicit project-path argument (gum-9uff).

//lint:file-ignore SA1019 SEP-2577 deprecates roots; spec §9.2 still binds project-local profile lookup to it (exit tracked in gum-9uff).

package mcp

import (
	"net/url"
	"strings"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// rootsInputRequestID is the key gum assigns to its roots/list input request
// and reads back out of InputResponses. Namespaced so it cannot collide with
// an input request raised elsewhere in the same call.
const rootsInputRequestID = "gum.roots"

// rootsCache caches the negotiated file:// roots for one MCP session. The
// cache is filled from the first roots/list reply the session delivers;
// subsequent requests reuse the cached list.
type rootsCache struct {
	mu     sync.Mutex
	bySess map[*sdkmcp.ServerSession][]string
}

// rootsInputRequest is the InputRequests map gum returns when it still needs
// the client's roots. Spec §9.2 wants the list once per session; the SDK
// retry loop delivers the reply as an InputResponses entry on the same call.
func rootsInputRequest() sdkmcp.InputRequestMap {
	return sdkmcp.InputRequestMap{rootsInputRequestID: &sdkmcp.ListRootsParams{}} //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it
}

// rootsForRequest returns the negotiated file roots for req's session.
// needRoots is true when the roots are not known yet and the caller must
// answer with rootsInputRequest() so the client can supply them. Non-file
// URIs are filtered out per spec §9.2; a nil session or a client without the
// roots capability yields an empty list (project-local lookup disabled).
func (rc *rootsCache) rootsForRequest(req *sdkmcp.CallToolRequest) (roots []string, needRoots bool) {
	if req == nil || req.Session == nil {
		return nil, false
	}
	if cached, ok := rc.lookup(req.Session); ok {
		return cached, false
	}
	if res := rootsFromInputResponses(req); res != nil {
		roots = fileRoots(res)
		rc.storeSessionRoots(req.Session, roots)
		return roots, false
	}
	// Capabilities come from the request, not the session: MCP 2026-07-28
	// (SEP-2575) carries them in each request's `_meta`, and the SDK accessor
	// falls back to the session's InitializeParams for older clients. A
	// request without the capability is not cached, because under 2026-07-28
	// it says nothing about the next request on the same session.
	caps := req.ClientCapabilities()
	if caps == nil || caps.RootsV2 == nil { //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it
		return nil, false
	}
	if req.Params != nil && len(req.Params.InputResponses) > 0 {
		// A retry that carried other responses but not ours. Asking again
		// would spin the SDK retry loop, so treat it as "no roots".
		return nil, false
	}
	return nil, true
}

// rootsFromInputResponses extracts the roots/list reply gum asked for, or nil
// when this request carries none.
func rootsFromInputResponses(req *sdkmcp.CallToolRequest) *sdkmcp.ListRootsResult { //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it
	if req.Params == nil {
		return nil
	}
	res, ok := req.Params.InputResponses[rootsInputRequestID].(*sdkmcp.ListRootsResult) //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it
	if !ok {
		return nil
	}
	return res
}

// fileRoots keeps only the file:// URIs from a roots/list reply. Spec §9.2
// allows no other scheme for project-local resolution in v0.1.0.
func fileRoots(res *sdkmcp.ListRootsResult) []string { //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it
	var roots []string
	for _, r := range res.Roots {
		if r != nil && isFileURI(r.URI) {
			roots = append(roots, r.URI)
		}
	}
	return roots
}

// lookup returns a copy of the cached roots for session, if any.
func (rc *rootsCache) lookup(session *sdkmcp.ServerSession) ([]string, bool) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	roots, ok := rc.bySess[session]
	if !ok {
		return nil, false
	}
	return append([]string(nil), roots...), true
}

func (rc *rootsCache) storeSessionRoots(session *sdkmcp.ServerSession, roots []string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.bySess == nil {
		rc.bySess = make(map[*sdkmcp.ServerSession][]string)
	}
	rc.bySess[session] = append([]string(nil), roots...)
}

// projectRootError carries the PROJECT_ROOT_REQUIRED envelope payload.
// reason values are closed at the call sites in resolveProjectRoot.
type projectRootError struct {
	Reason          string   // "missing_gumroot_in_multi_root_session" | "gumroot_not_file_uri" | "gumroot_not_in_negotiated_set"
	NegotiatedRoots []string // every file root from the cached roots/list reply
	SuppliedRoot    string   // the _meta.gumRoot value as supplied (may be empty)
}

// resolveProjectRoot applies the §9.2 multi-root selection rule. Returns the
// chosen root URI (string form) when project-local lookup should fire, or
// ("", err) when the request must be failed with PROJECT_ROOT_REQUIRED.
// Returns ("", nil) when project-local lookup is disabled (zero roots).
func resolveProjectRoot(roots []string, metaGumRoot string) (string, *projectRootError) {
	switch len(roots) {
	case 0:
		return "", nil
	case 1:
		if metaGumRoot == "" {
			return roots[0], nil
		}
		if !isFileURI(metaGumRoot) {
			return "", &projectRootError{
				Reason:          "gumroot_not_file_uri",
				NegotiatedRoots: roots,
				SuppliedRoot:    metaGumRoot,
			}
		}
		if !sliceContainsString(roots, metaGumRoot) {
			return "", &projectRootError{
				Reason:          "gumroot_not_in_negotiated_set",
				NegotiatedRoots: roots,
				SuppliedRoot:    metaGumRoot,
			}
		}
		return metaGumRoot, nil
	default:
		if metaGumRoot == "" {
			return "", &projectRootError{
				Reason:          "missing_gumroot_in_multi_root_session",
				NegotiatedRoots: roots,
			}
		}
		if !isFileURI(metaGumRoot) {
			return "", &projectRootError{
				Reason:          "gumroot_not_file_uri",
				NegotiatedRoots: roots,
				SuppliedRoot:    metaGumRoot,
			}
		}
		if !sliceContainsString(roots, metaGumRoot) {
			return "", &projectRootError{
				Reason:          "gumroot_not_in_negotiated_set",
				NegotiatedRoots: roots,
				SuppliedRoot:    metaGumRoot,
			}
		}
		return metaGumRoot, nil
	}
}

// projectRootRequiredEnvelope builds the spec §1421 PROJECT_ROOT_REQUIRED
// envelope from a projectRootError. Returned as a map so callers can marshal
// it inside an MCP tool error or RPC error.data field.
func projectRootRequiredEnvelope(e *projectRootError) map[string]any {
	return map[string]any{
		"error_code":       "PROJECT_ROOT_REQUIRED",
		"reason":           e.Reason,
		"negotiated_roots": e.NegotiatedRoots,
		"supplied_root":    e.SuppliedRoot,
		"user_message":     "Project-local profile resolution requires _meta.gumRoot in multi-root MCP sessions.",
	}
}

// ResolveProjectRootForRequest reads the session's negotiated roots list and
// applies the §9.2 resolution algorithm to pick the project root that profile
// lookup should use for this request.
//
// Returns:
//   - (path, false, nil)  when project-local lookup should fire against path
//   - ("", false, nil)    when project-local lookup is disabled (no roots /
//     no client support); caller falls back to user-global
//   - ("", true, nil)     when the roots are not known yet; the caller must
//     answer with rootsInputRequest() and let the SDK retry the call
//   - ("", false, projErr) when the request must fail with
//     PROJECT_ROOT_REQUIRED
//
// metaGumRoot is the request's `_meta.gumRoot` value (empty when absent).
func (s *Server) ResolveProjectRootForRequest(req *sdkmcp.CallToolRequest, metaGumRoot string) (string, bool, *projectRootError) {
	roots, needRoots := s.roots.rootsForRequest(req)
	if needRoots {
		return "", true, nil
	}
	rootURI, projErr := resolveProjectRoot(roots, metaGumRoot)
	if projErr != nil {
		return "", false, projErr
	}
	if rootURI == "" {
		return "", false, nil
	}
	return rootURIToPath(rootURI), false, nil
}

// rootURIToPath converts a file:// URI to an absolute local filesystem path.
// Honours the URI encoding rules so paths with spaces or unicode survive the
// round-trip. Returns "" for any non-file or malformed URI.
func rootURIToPath(uri string) string {
	if !isFileURI(uri) {
		return ""
	}
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return u.Path
}

// isFileURI reports whether s starts with the file:// scheme. Spec §9.2
// allows only file:// for project-local resolution in v0.1.0.
func isFileURI(s string) bool {
	return strings.HasPrefix(s, "file://")
}

// sliceContainsString returns true when needle equals any haystack entry.
// Linear scan is fine here: roots lists in practice are tiny (1-3 entries).
func sliceContainsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
