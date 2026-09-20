package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	keyringlib "github.com/zalando/go-keyring"
)

// TestMCPStdioRejectsAnInvalidProfile pins the guard between the dispatcher
// build and the transport: a profile name gum cannot parse must fail before
// the server takes over stdio, otherwise the failure is invisible to the host.
func TestMCPStdioRejectsAnInvalidProfile(t *testing.T) {
	keyringlib.MockInit()
	isolatedHome(t)

	// --profile is a root persistent flag, so the run has to go through the
	// root command for the bad name to reach the server.
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"mcp", "--stdio", "--profile", "bad/name"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("want an invalid-profile error, got nil")
	}
	if !strings.Contains(err.Error(), "bad/name") {
		t.Errorf("err=%q does not name the rejected profile", err)
	}
}

// TestMCPStdioStopsWithItsContext pins the shutdown path. The signal context
// wraps the caller's, so a cancelled parent has to bring the server down
// instead of holding stdio open forever.
func TestMCPStdioStopsWithItsContext(t *testing.T) {
	keyringlib.MockInit()
	isolatedHome(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- runMCPStdio(ctx, "default") }()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("runMCPStdio did not return after its context was cancelled")
	}
}
