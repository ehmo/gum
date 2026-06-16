// Package dispatch_test — logging/setLevel tolerance test (spec.md §13.1).
package dispatch_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/dispatch"
	gummcp "github.com/ehmo/gum/internal/mcp"
)

// noopDispatcher satisfies dispatch.Dispatcher with a no-op implementation.
type noopDispatcher struct{}

func (noopDispatcher) Dispatch(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	panic("not implemented in noopDispatcher")
}

// TestLoggingSetLevelTolerant verifies that the server responds to
// logging/setLevel with success (not method-not-found) per spec.md §13.1.
//
// v1.6.0 go-sdk exposes ClientSession.SetLoggingLevel which sends the standard
// logging/setLevel JSON-RPC request and treats a 2xx empty result as success.
// A method-not-found server response would surface here as a non-nil error.
func TestLoggingSetLevelTolerant(t *testing.T) {
	defer goleak.VerifyNone(t)

	srv := gummcp.NewServer(noopDispatcher{})
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- srv.Run(ctx, srvTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "setlevel-test", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	// Pre-check: server is alive and tools/list works.
	if _, err := cs.ListTools(ctx, &sdkmcp.ListToolsParams{}); err != nil {
		t.Fatalf("ListTools (pre-setLevel): %v", err)
	}

	// The contract: send logging/setLevel, expect success (no method-not-found).
	if err := cs.SetLoggingLevel(ctx, &sdkmcp.SetLoggingLevelParams{Level: "info"}); err != nil {
		t.Fatalf("logging/setLevel returned error (expected tolerant {} success per spec §13.1): %v", err)
	}

	// Post-check: server still healthy after the tolerant handling.
	if _, err := cs.ListTools(ctx, &sdkmcp.ListToolsParams{}); err != nil {
		t.Fatalf("ListTools (post-setLevel): %v", err)
	}

	cancel()
	select {
	case runErr := <-serverDone:
		if runErr != nil && !isCleanShutdownError(runErr) {
			t.Errorf("server.Run returned unexpected error after setLevel test: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Error("server did not stop within 2s")
	}
}

func isCleanShutdownError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, token := range []string{"eof", "closed", "cancel", "reset", "broken pipe"} {
		if strings.Contains(msg, token) {
			return true
		}
	}
	return false
}
