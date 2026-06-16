// Command gum is the Google Universal MCP CLI.
//
// All subcommands share the same dispatch.Dispatcher kernel as the MCP server
// (spec.md §14): presentation layers stay thin, internal/dispatch owns the
// invocation lifecycle.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// version is set at build time via -ldflags='-X main.version=v0.1.0'.
var version = "dev"

func main() {
	// Spec §14.1 rule 3: install a JSON handler on slog.Default before any
	// goroutine spawns. Level and format are overridden by --log-level /
	// --log-format flags during root command execution (see root.go).
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: false,
	})))
	root := newRootCmd()
	// Two-pass flag setup: a `gum call <op_id>` invocation gets typed --flags
	// derived from that op's RequestFields registered before cobra parses, so
	// `--site-url`, `--start-date`, etc. are real flags (with --help, completion,
	// and typo detection). The op_id is a positional, so this peeks os.Args.
	registerDynamicCallFlags(root, os.Args[1:])
	// Signal-aware root context so Ctrl-C / SIGTERM cancels the in-flight
	// command (cmd.Context() propagates the deadline into dispatch, adapters,
	// and the MCP server) instead of being a hard kill mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := root.ExecuteContext(ctx); err != nil {
		// Spec §14.1 permits direct stderr for the top-level fatal error path
		// in cmd/gum (not in the prohibited internal/* scan set). Using slog
		// here would route the fatal exit through the JSON handler installed
		// above, which is also acceptable; the direct write is kept so the
		// final error stays a single human-readable line.
		//
		// A dispatch error already written to stderr as a full JSON envelope
		// (errRendered) is not re-printed — otherwise stderr would carry both
		// the envelope and a redundant terse line.
		var rendered errRendered
		if !errors.As(err, &rendered) {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}
