// Package-internal MCP roots negotiation for §9.2 project-local profile
// resolution. Spec contract:
//
//   - call roots/list once per session and cache file:// URIs
//   - 1 file root → that root is the project root (gumRoot optional)
//   - >1 file roots → request MUST carry _meta.gumRoot matching one
//   - 0 roots / no client support → project-local lookup disabled
//   - non-file roots are filtered out; non-file _meta.gumRoot is rejected
//
// The PROJECT_ROOT_REQUIRED error code lives in internal/dispatch/errors.go;
// the envelope shape mirrors spec §1421 with reason / negotiated_roots /
// supplied_root fields for operator-friendly diagnosis.

package mcp

import (
	"context"
	"net/url"
	"strings"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// rootsCache caches the negotiated file:// roots for one MCP session. The
// cache is loaded lazily on the first request that needs project-local
// resolution; subsequent requests reuse the cached list.
type rootsCache struct {
	mu       sync.Mutex
	bySess   map[*sdkmcp.ServerSession][]string
	nilRoots []string
	nilSet   bool
}

// loadRoots fetches the session's roots if not already cached. Returns the
// cached file-URI list. Non-file URIs are filtered out per spec §9.2. A nil
// session, a client without roots capability, or a ListRoots RPC failure
// yields an empty list (project-local disabled).
func (rc *rootsCache) loadRoots(ctx context.Context, session *sdkmcp.ServerSession) []string {
	rc.mu.Lock()
	if session == nil {
		if rc.nilSet {
			roots := append([]string(nil), rc.nilRoots...)
			rc.mu.Unlock()
			return roots
		}
		rc.nilSet = true
		rc.mu.Unlock()
		return nil
	}
	if rc.bySess != nil {
		if roots, ok := rc.bySess[session]; ok {
			roots = append([]string(nil), roots...)
			rc.mu.Unlock()
			return roots
		}
	}
	rc.mu.Unlock()

	var roots []string
	params := session.InitializeParams()
	if params == nil || params.Capabilities == nil || params.Capabilities.RootsV2 == nil {
		rc.storeSessionRoots(session, nil)
		return nil
	}
	res, err := session.ListRoots(ctx, &sdkmcp.ListRootsParams{})
	if err != nil || res == nil {
		rc.storeSessionRoots(session, nil)
		return nil
	}
	for _, r := range res.Roots {
		if r != nil && isFileURI(r.URI) {
			roots = append(roots, r.URI)
		}
	}
	rc.storeSessionRoots(session, roots)
	return append([]string(nil), roots...)
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

// ResolveProjectRootForRequest fetches the negotiated roots list (caching
// the first ListRoots reply) and applies the §9.2 resolution algorithm to
// pick the project root that profile lookup should use for this request.
//
// Returns:
//   - (path, nil)        when project-local lookup should fire against path
//   - ("", nil)          when project-local lookup is disabled (no roots /
//                        no client support); caller falls back to user-global
//   - ("", projErr)      when the request must fail with PROJECT_ROOT_REQUIRED
//
// metaGumRoot is the request's `_meta.gumRoot` value (empty when absent).
func (s *Server) ResolveProjectRootForRequest(ctx context.Context, session *sdkmcp.ServerSession, metaGumRoot string) (string, *projectRootError) {
	roots := s.roots.loadRoots(ctx, session)
	rootURI, projErr := resolveProjectRoot(roots, metaGumRoot)
	if projErr != nil {
		return "", projErr
	}
	if rootURI == "" {
		return "", nil
	}
	return rootURIToPath(rootURI), nil
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
