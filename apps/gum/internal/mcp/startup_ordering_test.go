package mcp_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	gummcp "github.com/ehmo/gum/internal/mcp"
)

// TestMCPStartupOrdering asserts the spec §4.1 line 383 invariant: every
// Tier A tool (9 meta + 18 convenience = 27) is registered before Run, and
// the connected client sees zero spurious tools/list_changed notifications
// during the initialize handshake. The SDK auto-emits list_changed when
// AddTool is called after the server starts accepting sessions; this test
// would catch a regression that moves Tier A registrations into a post-Run
// hot path.
func TestMCPStartupOrdering(t *testing.T) {
	defer goleak.VerifyNone(t)

	srv := gummcp.NewServer(stubDispatcher{})

	// All registrations must already be in place before Run.
	if got := len(srv.AllToolNames()); got != 27 {
		t.Fatalf("AllToolNames after NewServer = %d; want 27 (9 meta + 18 convenience)", got)
	}
	if got := len(srv.MetaToolNames()); got != 9 {
		t.Errorf("MetaToolNames = %d; want 9", got)
	}
	if got := len(srv.ConvenienceToolNames()); got != 18 {
		t.Errorf("ConvenienceToolNames = %d; want 18", got)
	}

	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx, srvTransport) }()

	var notifyCount atomic.Int64
	client := sdkmcp.NewClient(
		&sdkmcp.Implementation{Name: "startup-ordering-test", Version: "0.0.1"},
		&sdkmcp.ClientOptions{
			ToolListChangedHandler: func(_ context.Context, _ *sdkmcp.ToolListChangedRequest) {
				notifyCount.Add(1)
			},
		},
	)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}

	// Drain any pending debounced notifications. The SDK's notification
	// debounce window is ~1s in tests without synctest; sleeping 1.2s
	// gives a real-world margin without making the test flaky.
	time.Sleep(1200 * time.Millisecond)

	if got := notifyCount.Load(); got != 0 {
		t.Errorf("tools/list_changed notifications during/after initialize = %d; want 0 (spec §4.1 line 383)", got)
	}

	// tools/list MUST return exactly 27 tools (spec §4.1: "tools/list MUST
	// therefore remain exactly 27 tools even when active plugins are installed").
	listRes, err := cs.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if got := len(listRes.Tools); got != 29 {
		t.Errorf("ListTools = %d tools; want 29 (27 Tier A + 2 skills helpers)", got)
	}

	_ = cs.Close()
	cancel()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Server.Run did not return within 2s after cancel")
	}
}
