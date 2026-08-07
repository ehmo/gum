// gum-d7t: acceptance test for spec §9.2 — MCP roots binding +
// project-local profile resolution.
//
// Spec §9.2 lines 2048-2052: "In MCP mode, project-local lookup MUST NOT
// rely on process $PWD. With a single file root, GUM uses that root. With
// multiple file roots, the request MUST provide _meta.gumRoot equal to one
// of the negotiated root URIs. If _meta.gumRoot is absent, non-file, or
// not in the negotiated root set, GUM fails project-local profile lookup
// with PROJECT_ROOT_REQUIRED before applying project-local overrides."
//
// Both tests here drive real client tool calls rather than poking the
// resolver directly. Under MCP 2026-07-28 (SEP-2322) the server cannot send
// roots/list mid-request; it returns an InputRequests map and the SDK
// fulfils it and retries the handler. Only a real call exercises that round
// trip, so the assertions read the profile the dispatcher actually received.

// The roots feature is deprecated as of MCP revision 2026-07-28 (SEP-2577);
// gum keeps it while spec §9.2 binds project-local lookup to it. The SDK's
// SA1019 roots reports are suppressed twice below because the two linters
// read different directives: CI's bare staticcheck reads //lint:file-ignore,
// golangci-lint reads the per-line //nolint. See internal/mcp/roots.go.

//lint:file-ignore SA1019 SEP-2577 deprecates roots; spec §9.2 still binds project-local profile lookup to it (exit tracked in gum-9uff).

package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/dispatch"
	gummcp "github.com/ehmo/gum/internal/mcp"
	"github.com/ehmo/gum/internal/output/profile"
)

// rootsOpID is a catalog op whose default variant carries an output_profile,
// so dispatchToolCall actually resolves a profile for it. rootsProfileName is
// that variant's output_profile, i.e. the .toml basename the resolver looks
// for under <root>/.gum/profiles/ and $XDG_CONFIG_HOME/gum/profiles/.
const (
	rootsOpID        = "flights.search"
	rootsProfileName = "flights.search.v1"
)

// recordingDispatcher captures the Invocation the MCP layer built, so a test
// can assert which profile §9.2 resolution picked. It answers with an empty
// body: the tests read the recording, not the response.
type recordingDispatcher struct {
	mu   sync.Mutex
	invs []*dispatch.Invocation
}

func (d *recordingDispatcher) Dispatch(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.invs = append(d.invs, inv)
	return &dispatch.ShapedResponse{Body: []byte("{}")}, nil
}

// last returns the most recent Invocation, failing the test when dispatch was
// never reached (the usual symptom of a §9.2 rejection or an MRTR stall).
func (d *recordingDispatcher) last(t *testing.T) *dispatch.Invocation {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.invs) == 0 {
		t.Fatal("dispatcher never invoked; the call did not survive §9.2 resolution")
	}
	return d.invs[len(d.invs)-1]
}

// callRootsOp issues the Tier A read call the roots tests share.
func callRootsOp(ctx context.Context, t *testing.T, cs *sdkmcp.ClientSession) {
	t.Helper()
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := cs.CallTool(callCtx, &sdkmcp.CallToolParams{
		Name:      "gum.read",
		Arguments: map[string]any{"op_id": rootsOpID, "args": map[string]any{}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool returned IsError: %s", textContent(t, res))
	}
}

// TestProfileResolutionFromMCPRoots — bead-named acceptance for gum-d7t.
//
// Stages a tempdir that contains both a project-local and a user-global
// profile of the same name. Boots a real MCP server, connects a client that
// advertises a single file:// root pointing at the project, and asserts that
// the profile handed to the dispatcher is the project-local one.
//
// This nails spec §9.2: "Project-local: .gum/profiles/<profile-name>.toml
// in the nearest ancestor directory containing .gum/ ... User-global: ..."
// and the MCP-roots binding rule that the root URI must come from
// roots/list, not from $PWD.
func TestProfileResolutionFromMCPRoots(t *testing.T) {
	tmp := t.TempDir()

	// Project-local profile: <tmp>/project/.gum/profiles/<name>.toml
	projectDir := filepath.Join(tmp, "project")
	projectProfDir := filepath.Join(projectDir, ".gum", "profiles")
	if err := os.MkdirAll(projectProfDir, 0o755); err != nil {
		t.Fatalf("mkdir project profiles: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(projectProfDir, rootsProfileName+".toml"),
		[]byte("sort_by = \"from-project\"\n"),
		0o644,
	); err != nil {
		t.Fatalf("write project profile: %v", err)
	}

	// User-global profile: $XDG_CONFIG_HOME/gum/profiles/<name>.toml
	xdgConfig := filepath.Join(tmp, "xdg-config")
	userProfDir := filepath.Join(xdgConfig, "gum", "profiles")
	if err := os.MkdirAll(userProfDir, 0o755); err != nil {
		t.Fatalf("mkdir user profiles: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(userProfDir, rootsProfileName+".toml"),
		[]byte("sort_by = \"from-user-global\"\n"),
		0o644,
	); err != nil {
		t.Fatalf("write user profile: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)

	disp := &recordingDispatcher{}
	srv := gummcp.NewServer(disp)
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(
		&sdkmcp.Implementation{Name: "gum-d7t-client", Version: "0.0.1"},
		&sdkmcp.ClientOptions{Capabilities: &sdkmcp.ClientCapabilities{RootsV2: &sdkmcp.RootCapabilities{}}}, //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it
	)
	client.AddRoots(&sdkmcp.Root{URI: "file://" + projectDir, Name: "project"}) //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it

	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	// Single-root session ⇒ no _meta.gumRoot needed.
	callRootsOp(ctx, t, cs)

	got := disp.last(t).OutputProfile
	if got == nil {
		t.Fatal("OutputProfile=nil; want the project-local profile resolved from the MCP root")
	}
	if got.SortBy != "from-project" {
		t.Errorf("sort_by=%q; want \"from-project\" (project-local must shadow user-global)", got.SortBy)
	}

	// Sanity check: with no root path the same name resolves to user-global,
	// proving the precedence assertion above actually tests precedence.
	pUser, srcUser, err := profile.ResolveProfile("", rootsProfileName, nil)
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

// TestRootsCacheStickyAcrossRootsChange verifies the spec §9.2 cache
// invariant ("call roots/list once per session") by behaviour: after the
// first call caches the root, changing the client's roots MUST NOT change
// what later calls in the same session resolve. Both directories hold a
// profile of the same name with different contents, so a cache refresh would
// show up as a different sort_by.
func TestRootsCacheStickyAcrossRootsChange(t *testing.T) {
	tmp := t.TempDir()
	initialDir := writeProjectProfile(t, filepath.Join(tmp, "initial"), "from-initial")
	replacementDir := writeProjectProfile(t, filepath.Join(tmp, "replacement"), "from-replacement")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg-config"))

	disp := &recordingDispatcher{}
	srv := gummcp.NewServer(disp)
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(
		&sdkmcp.Implementation{Name: "gum-d7t-cache-client", Version: "0.0.1"},
		&sdkmcp.ClientOptions{Capabilities: &sdkmcp.ClientCapabilities{
			RootsV2: &sdkmcp.RootCapabilities{ListChanged: true}, //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it
		}},
	)
	initialURI := "file://" + initialDir
	client.AddRoots(&sdkmcp.Root{URI: initialURI}) //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it

	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	callRootsOp(ctx, t, cs)
	first := disp.last(t).OutputProfile
	if first == nil || first.SortBy != "from-initial" {
		t.Fatalf("first call profile=%+v; want sort_by=from-initial", first)
	}

	// Mutate the client roots after the cache is warm. Give the SDK a moment
	// to deliver the list_changed notification.
	client.RemoveRoots(initialURI)                                 //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it
	client.AddRoots(&sdkmcp.Root{URI: "file://" + replacementDir}) //nolint:staticcheck // SEP-2577 deprecates roots; spec §9.2 still binds project-local lookup to it
	time.Sleep(50 * time.Millisecond)

	callRootsOp(ctx, t, cs)
	second := disp.last(t).OutputProfile
	if second == nil || second.SortBy != "from-initial" {
		t.Errorf("second call profile=%+v; want sort_by=from-initial (cache must hold across client-side roots changes — spec §9.2 \"once per session\")", second)
	}
}

// writeProjectProfile stages <dir>/.gum/profiles/<rootsProfileName>.toml with
// the given sort_by marker and returns dir.
func writeProjectProfile(t *testing.T, dir, sortBy string) string {
	t.Helper()
	profDir := filepath.Join(dir, ".gum", "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", profDir, err)
	}
	if err := os.WriteFile(
		filepath.Join(profDir, rootsProfileName+".toml"),
		[]byte("sort_by = \""+sortBy+"\"\n"),
		0o644,
	); err != nil {
		t.Fatalf("write %s profile: %v", dir, err)
	}
	return dir
}
