package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestLoadRootsNilSessionReturnsNil pins loadRoots's
// `session == nil → return nil` arm (roots.go:45-47). A request that
// arrives before a session is attached must yield an empty list (so
// callers fall back to user-global) rather than panic.
func TestLoadRootsNilSessionReturnsNil(t *testing.T) {
	t.Parallel()
	rc := &rootsCache{}
	if got := rc.loadRoots(context.Background(), nil); got != nil {
		t.Errorf("got=%v; want nil", got)
	}
}

func TestRootsCacheStoresRootsPerSession(t *testing.T) {
	t.Parallel()
	rc := &rootsCache{}
	sessionA := new(sdkmcp.ServerSession)
	sessionB := new(sdkmcp.ServerSession)

	rc.storeSessionRoots(sessionA, []string{"file:///a"})
	rc.storeSessionRoots(sessionB, []string{"file:///b"})

	gotA := rc.loadRoots(context.Background(), sessionA)
	gotB := rc.loadRoots(context.Background(), sessionB)
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
// ResolveProjectRootForRequest's `rootURI == "" → return "", nil` arm
// (roots.go:154-156). When loadRoots yields no roots, project-local
// is simply disabled — the caller must fall back to user-global.
func TestResolveProjectRootForRequestEmptyRootsReturnsEmpty(t *testing.T) {
	t.Parallel()
	s := &Server{}
	path, projErr := s.ResolveProjectRootForRequest(context.Background(), nil, "")
	if projErr != nil {
		t.Fatalf("projErr=%+v; want nil", projErr)
	}
	if path != "" {
		t.Errorf("path=%q; want empty", path)
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
