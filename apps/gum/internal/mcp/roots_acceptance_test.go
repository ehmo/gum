// gum-d7t: acceptance test for spec §9.2 — MCP roots binding +
// project-local profile resolution.
//
// Spec §9.2 lines 2048-2052: "In MCP mode, project-local lookup MUST NOT
// rely on process $PWD. With a single file root, GUM uses that root. With
// multiple file roots, the request MUST provide _meta.gumRoot equal to one
// of the negotiated root URIs. If _meta.gumRoot is absent, non-file, or
// not in the negotiated root set, GUM fails project-local profile lookup
// with PROJECT_ROOT_REQUIRED before applying project-local overrides."

package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	gummcp "github.com/ehmo/gum/internal/mcp"
	"github.com/ehmo/gum/internal/output/profile"
)

// TestProfileResolutionFromMCPRoots — bead-named acceptance for gum-d7t.
//
// Stages a tempdir that contains both a project-local and a user-global
// profile of the same name. Boots a real MCP server, connects a client that
// advertises a single file:// root pointing at the project, and asserts that
// the server's roots-aware lookup chain picks the project-local profile.
//
// This nails spec §9.2: "Project-local: .gum/profiles/<profile-name>.toml
// in the nearest ancestor directory containing .gum/ ... User-global: ..."
// and the MCP-roots binding rule that the root URI must come from
// roots/list, not from $PWD.
func TestProfileResolutionFromMCPRoots(t *testing.T) {
	tmp := t.TempDir()

	// Project-local profile: <tmp>/project/.gum/profiles/myprof.toml
	projectDir := filepath.Join(tmp, "project")
	projectProfDir := filepath.Join(projectDir, ".gum", "profiles")
	if err := os.MkdirAll(projectProfDir, 0o755); err != nil {
		t.Fatalf("mkdir project profiles: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(projectProfDir, "myprof.toml"),
		[]byte("sort_by = \"from-project\"\n"),
		0o644,
	); err != nil {
		t.Fatalf("write project profile: %v", err)
	}

	// User-global profile: $XDG_CONFIG_HOME/gum/profiles/myprof.toml
	xdgConfig := filepath.Join(tmp, "xdg-config")
	userProfDir := filepath.Join(xdgConfig, "gum", "profiles")
	if err := os.MkdirAll(userProfDir, 0o755); err != nil {
		t.Fatalf("mkdir user profiles: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(userProfDir, "myprof.toml"),
		[]byte("sort_by = \"from-user-global\"\n"),
		0o644,
	); err != nil {
		t.Fatalf("write user profile: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)

	projectURI := "file://" + projectDir

	srv := gummcp.NewServer(stubDispatcher{})
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(
		&sdkmcp.Implementation{Name: "gum-d7t-client", Version: "0.0.1"},
		&sdkmcp.ClientOptions{
			Capabilities: &sdkmcp.ClientCapabilities{
				RootsV2: &sdkmcp.RootCapabilities{},
			},
		},
	)
	client.AddRoots(&sdkmcp.Root{URI: projectURI, Name: "project"})

	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	session := waitForServerSession(t, srv)

	// Single-root session ⇒ no _meta.gumRoot needed.
	gotRoot, projErr := srv.ResolveProjectRootForRequest(ctx, session, "")
	if projErr != nil {
		t.Fatalf("ResolveProjectRootForRequest: %+v", projErr)
	}
	if gotRoot != projectDir {
		t.Errorf("resolved root path=%q; want %q (file:// URI must decode to local path)", gotRoot, projectDir)
	}

	p, src, err := profile.ResolveProfile(gotRoot, "myprof", nil)
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if src != profile.SourceProjectLocal {
		t.Errorf("source=%q; want project-local (spec §9.2 first match wins)", src)
	}
	if p.SortBy != "from-project" {
		t.Errorf("sort_by=%q; want \"from-project\" (project-local must shadow user-global)", p.SortBy)
	}

	// Sanity check: with no root path, the same name resolves to user-global,
	// proving the precedence test actually tests precedence.
	pUser, srcUser, err := profile.ResolveProfile("", "myprof", nil)
	if err != nil {
		t.Fatalf("ResolveProfile(no-root): %v", err)
	}
	if srcUser != profile.SourceUserGlobal {
		t.Errorf("no-root source=%q; want user-global", srcUser)
	}
	if pUser.SortBy != "from-user-global" {
		t.Errorf("no-root sort_by=%q; want \"from-user-global\"", pUser.SortBy)
	}
}

// TestRootsCacheStickyAcrossRootsChange verifies the spec §9.2 cache invariant
// ("call roots/list once per session") by behaviour: after the first
// resolution caches the root, a subsequent roots/list_changed (here forced by
// removing and re-adding a different root on the client side) MUST NOT cause
// the server-side cache to refresh — the cached value is what subsequent
// requests in the same session see.
func TestRootsCacheStickyAcrossRootsChange(t *testing.T) {
	srv := gummcp.NewServer(stubDispatcher{})
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(
		&sdkmcp.Implementation{Name: "gum-d7t-cache-client", Version: "0.0.1"},
		&sdkmcp.ClientOptions{
			Capabilities: &sdkmcp.ClientCapabilities{
				RootsV2: &sdkmcp.RootCapabilities{ListChanged: true},
			},
		},
	)
	const initialURI = "file:///tmp/d7t-initial"
	const replacementURI = "file:///tmp/d7t-replacement"
	client.AddRoots(&sdkmcp.Root{URI: initialURI})

	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	session := waitForServerSession(t, srv)

	first, projErr := srv.ResolveProjectRootForRequest(ctx, session, "")
	if projErr != nil {
		t.Fatalf("first resolve: %+v", projErr)
	}
	const wantInitial = "/tmp/d7t-initial"
	if first != wantInitial {
		t.Fatalf("first resolve path=%q; want %q", first, wantInitial)
	}

	// Mutate the client roots after the cache is warm. Give the SDK a moment
	// to deliver any list_changed notification.
	client.RemoveRoots(initialURI)
	client.AddRoots(&sdkmcp.Root{URI: replacementURI})
	time.Sleep(50 * time.Millisecond)

	second, projErr := srv.ResolveProjectRootForRequest(ctx, session, "")
	if projErr != nil {
		t.Fatalf("second resolve: %+v", projErr)
	}
	if second != first {
		t.Errorf("second resolve path=%q; want %q (cache must hold across client-side roots changes — spec §9.2 \"once per session\")", second, first)
	}
}

// waitForServerSession spins until the server has accepted at least one
// session. The in-memory transport delivers Connect synchronously from the
// client side, but the server's session bookkeeping happens in a goroutine;
// a tiny spin loop avoids a brittle time.Sleep.
func waitForServerSession(t *testing.T, srv *gummcp.Server) *sdkmcp.ServerSession {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for ss := range srv.Sessions() {
			return ss
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no server session became visible within 2s")
	return nil
}
