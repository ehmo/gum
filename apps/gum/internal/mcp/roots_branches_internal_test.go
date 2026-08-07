// Branch coverage for rootsForRequest. The roots feature is deprecated as of
// MCP revision 2026-07-28 (SEP-2577); gum keeps it while spec §9.2 binds
// project-local lookup to it. CI's bare staticcheck reads //lint:file-ignore,
// golangci-lint reads the per-line //nolint, so both appear.

//lint:file-ignore SA1019 SEP-2577 deprecates roots; spec §9.2 still binds project-local profile lookup to it (exit tracked in gum-9uff).

package mcp

import (
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// rootsReq builds the minimal CallToolRequest that rootsForRequest reads:
// a session to key the cache on, plus params carrying the client's roots
// capability and any InputResponses. Capabilities travel in `_meta` under
// MCP 2026-07-28 (SEP-2575), which is what ClientCapabilities() reads first.
func rootsReq(session *sdkmcp.ServerSession, withRootsCap bool, responses sdkmcp.InputResponseMap) *sdkmcp.CallToolRequest {
	params := &sdkmcp.CallToolParamsRaw{InputResponses: responses}
	if withRootsCap {
		params.Meta = sdkmcp.Meta{
			sdkmcp.MetaKeyClientCapabilities: map[string]any{"roots": map[string]any{}},
		}
	}
	return &sdkmcp.CallToolRequest{Session: session, Params: params}
}

// TestRootsForRequestNilSessionReturnsNil pins the
// `session == nil → return nil` arm. A request that arrives before a session
// is attached must yield an empty list (so callers fall back to user-global)
// rather than panic or ask the client for roots.
func TestRootsForRequestNilSessionReturnsNil(t *testing.T) {
	t.Parallel()
	rc := &rootsCache{}
	roots, needRoots := rc.rootsForRequest(nil)
	if roots != nil || needRoots {
		t.Errorf("roots=%v needRoots=%v; want nil false", roots, needRoots)
	}
}

// TestRootsForRequestWithoutCapabilityIsNotCached pins the caps-less arm.
// MCP 2026-07-28 carries client capabilities per request, so a request that
// advertises no roots capability must neither ask for roots nor poison the
// session cache for a later request that does advertise one.
func TestRootsForRequestWithoutCapabilityIsNotCached(t *testing.T) {
	t.Parallel()
	rc := &rootsCache{}
	session := new(sdkmcp.ServerSession)

	roots, needRoots := rc.rootsForRequest(rootsReq(session, false, nil))
	if roots != nil || needRoots {
		t.Errorf("roots=%v needRoots=%v; want nil false", roots, needRoots)
	}
	if _, cached := rc.bySess[session]; cached {
		t.Error("session cached after a request without roots capability; want no entry")
	}
}

// TestRootsForRequestAsksOnceThenUsesTheReply walks the SEP-2322 round trip:
// the first call reports needRoots, the reply is filtered to file:// URIs and
// cached, and a later call on the same session neither asks again nor needs
// the capability to be re-advertised.
func TestRootsForRequestAsksOnceThenUsesTheReply(t *testing.T) {
	t.Parallel()
	rc := &rootsCache{}
	session := new(sdkmcp.ServerSession)

	if roots, needRoots := rc.rootsForRequest(rootsReq(session, true, nil)); !needRoots || roots != nil {
		t.Fatalf("first call roots=%v needRoots=%v; want nil true", roots, needRoots)
	}

	reply := sdkmcp.InputResponseMap{
		rootsInputRequestID: &sdkmcp.ListRootsResult{Roots: []*sdkmcp.Root{ //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it
			{URI: "file:///tmp/keep"},
			{URI: "https://example.com/drop"},
			nil,
		}},
	}
	roots, needRoots := rc.rootsForRequest(rootsReq(session, true, reply))
	if needRoots {
		t.Fatal("needRoots=true after the reply arrived; want false")
	}
	if len(roots) != 1 || roots[0] != "file:///tmp/keep" {
		t.Fatalf("roots=%v; want [file:///tmp/keep] (non-file URIs filtered per §9.2)", roots)
	}

	cached, needRoots := rc.rootsForRequest(rootsReq(session, false, nil))
	if needRoots {
		t.Error("needRoots=true on a cached session; want false (roots/list is once per session)")
	}
	if len(cached) != 1 || cached[0] != "file:///tmp/keep" {
		t.Errorf("cached roots=%v; want [file:///tmp/keep]", cached)
	}
}

// TestRootsForRequestUnrelatedResponsesDoNotLoop pins the guard against the
// SDK retry loop: a retry that carries somebody else's InputResponses but not
// gum's must resolve to "no roots" instead of asking again.
func TestRootsForRequestUnrelatedResponsesDoNotLoop(t *testing.T) {
	t.Parallel()
	rc := &rootsCache{}
	other := sdkmcp.InputResponseMap{"other.request": &sdkmcp.ElicitResult{Action: "decline"}}

	roots, needRoots := rc.rootsForRequest(rootsReq(new(sdkmcp.ServerSession), true, other))
	if roots != nil || needRoots {
		t.Errorf("roots=%v needRoots=%v; want nil false", roots, needRoots)
	}
}

func TestRootsCacheStoresRootsPerSession(t *testing.T) {
	t.Parallel()
	rc := &rootsCache{}
	sessionA := new(sdkmcp.ServerSession)
	sessionB := new(sdkmcp.ServerSession)

	rc.storeSessionRoots(sessionA, []string{"file:///a"})
	rc.storeSessionRoots(sessionB, []string{"file:///b"})

	// No capability advertised: the per-session cache hit precedes the check.
	gotA, _ := rc.rootsForRequest(rootsReq(sessionA, false, nil))
	gotB, _ := rc.rootsForRequest(rootsReq(sessionB, false, nil))
	if len(gotA) != 1 || gotA[0] != "file:///a" {
		t.Fatalf("session A roots=%v; want file:///a", gotA)
	}
	if len(gotB) != 1 || gotB[0] != "file:///b" {
		t.Fatalf("session B roots=%v; want file:///b", gotB)
	}
}

// TestResolveProjectRootSingleRootNonFileGumRootRejected pins the
// single-root branch's `!isFileURI(metaGumRoot) → gumroot_not_file_uri`
// arm (roots.go:84-90). The multi-root variant is already tested;
// this is the missing single-root mirror.
func TestResolveProjectRootSingleRootNonFileGumRootRejected(t *testing.T) {
	t.Parallel()
	_, err := resolveProjectRoot([]string{"file:///tmp/a"}, "https://example.com/proj")
	if err == nil {
		t.Fatal("err=nil; want gumroot_not_file_uri")
	}
	if err.Reason != "gumroot_not_file_uri" {
		t.Errorf("reason=%q; want gumroot_not_file_uri", err.Reason)
	}
}

// TestResolveProjectRootForRequestEmptyRootsReturnsEmpty pins
// ResolveProjectRootForRequest's `rootURI == "" → return "", false, nil` arm.
// When the session has no roots, project-local is simply disabled — the
// caller must fall back to user-global without asking the client anything.
func TestResolveProjectRootForRequestEmptyRootsReturnsEmpty(t *testing.T) {
	t.Parallel()
	s := &Server{}
	path, needRoots, projErr := s.ResolveProjectRootForRequest(nil, "")
	if projErr != nil {
		t.Fatalf("projErr=%+v; want nil", projErr)
	}
	if needRoots {
		t.Error("needRoots=true for a request with no session; want false")
	}
	if path != "" {
		t.Errorf("path=%q; want empty", path)
	}
}

// TestResolveProjectRootForRequestReportsNeedRoots pins the SEP-2322 arm: a
// first request from a roots-capable client resolves to needRoots so the
// handler answers with an InputRequests map instead of a result.
func TestResolveProjectRootForRequestReportsNeedRoots(t *testing.T) {
	t.Parallel()
	s := &Server{}
	path, needRoots, projErr := s.ResolveProjectRootForRequest(rootsReq(new(sdkmcp.ServerSession), true, nil), "")
	if projErr != nil {
		t.Fatalf("projErr=%+v; want nil", projErr)
	}
	if !needRoots {
		t.Error("needRoots=false on the first call of a roots-capable session; want true")
	}
	if path != "" {
		t.Errorf("path=%q; want empty while roots are still unknown", path)
	}
}

// TestRootURIToPathMalformedURIReturnsEmpty pins rootURIToPath's
// `url.Parse err → return ""` arm (roots.go:168-170). A file:// URI
// with an invalid host portion passes isFileURI but trips url.Parse.
func TestRootURIToPathMalformedURIReturnsEmpty(t *testing.T) {
	t.Parallel()
	badURI := string([]byte{'f', 'i', 'l', 'e', ':', '/', '/', '['})
	if got := rootURIToPath(badURI); got != "" {
		t.Errorf("got=%q; want empty", got)
	}
}
