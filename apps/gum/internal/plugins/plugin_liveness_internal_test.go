package plugins

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectedPlugin returns a Plugin handle wired to an in-process MCP server,
// plus that server's session. No subprocess is involved, so the test controls
// exactly when the session ends — a real plugin crash is not reproducible from
// a unit test without shipping a second binary.
func connectedPlugin(t *testing.T) (*Plugin, *sdkmcp.ServerSession) {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "gum", Version: pluginClientVersion}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	return newPlugin("fixture", cs, nil), ss
}

// waitDead polls Alive until it reports false. The watcher runs on its own
// goroutine, so the flip is not synchronous with the peer's close.
func waitDead(t *testing.T, p *Plugin) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !p.Alive() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

// TestPluginAliveFlipsWhenSessionEnds pins the liveness signal gum-q79t needs:
// when the plugin side of the session goes away, the handle must report dead
// without anyone calling Stop. That is the crash case — nothing in the host
// asked for the teardown.
func TestPluginAliveFlipsWhenSessionEnds(t *testing.T) {
	plug, ss := connectedPlugin(t)
	if !plug.Alive() {
		t.Fatal("Alive()=false on a freshly connected plugin")
	}

	if err := ss.Close(); err != nil {
		t.Fatalf("server session close: %v", err)
	}
	if !waitDead(t, plug) {
		t.Error("Alive() stayed true after the plugin side closed the session")
	}
}

// TestPluginAliveFalseAfterStop pins the ordinary teardown: a handle the host
// stopped is never handed back to a caller either.
func TestPluginAliveFalseAfterStop(t *testing.T) {
	plug, ss := connectedPlugin(t)
	defer func() { _ = ss.Close() }()

	if err := plug.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if plug.Alive() {
		t.Error("Alive()=true after Stop")
	}
}
