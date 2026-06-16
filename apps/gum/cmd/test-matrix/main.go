// Command test-matrix runs every group from docs/test-matrix.md as a
// release-gate sweep and prints a per-group PASS/FAIL summary.
//
// Usage:
//
//	test-matrix [-matrix=docs/test-matrix.md] [-workdir=.] [-deferred=<path>] [-timeout=15m]
//
// The process exits non-zero when any group fails or when an expected
// test from the matrix did not actually run. Spec: bead gum-b22o.1;
// matrix source of truth: docs/test-matrix.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ehmo/gum/internal/testmatrix"
)

func main() {
	matrixPath := flag.String("matrix", "../../docs/test-matrix.md", "path to test-matrix.md (relative to -workdir)")
	workDir := flag.String("workdir", ".", "module directory containing go.mod for the `go test` invocation")
	deferredPath := flag.String("deferred", "", "newline-delimited list of expected tests outside this release scope; empty disables exceptions")
	timeout := flag.Duration("timeout", 15*time.Minute, "overall timeout for the matrix sweep")
	listOnly := flag.Bool("list", false, "print the parsed group/test plan and exit (no go test invocations)")
	flag.Parse()

	groups, err := testmatrix.ParseFile(*matrixPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "test-matrix: parse %s: %v\n", *matrixPath, err)
		os.Exit(2)
	}
	if len(groups) == 0 {
		fmt.Fprintf(os.Stderr, "test-matrix: no groups parsed from %s\n", *matrixPath)
		os.Exit(2)
	}

	if *listOnly {
		for _, g := range groups {
			fmt.Printf("Group %s — %s (%d tests)\n", g.Letter, g.Description, len(g.Tests))
			for _, name := range g.Tests {
				fmt.Printf("  %s\n", name)
			}
		}
		return
	}

	deferred := map[string]bool{}
	if *deferredPath != "" {
		deferred, err = testmatrix.ParseDeferredFile(*deferredPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "test-matrix: exceptions %s: %v\n", *deferredPath, err)
			os.Exit(2)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	runner := &testmatrix.Runner{WorkDir: *workDir, DeferredTests: deferred}
	results := runner.RunAll(ctx, groups)

	summary := testmatrix.Summarize(results)
	if err := summary.WriteTable(os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "test-matrix: write summary: %v\n", err)
		os.Exit(2)
	}

	if summary.AnyFailed() {
		fmt.Fprintln(os.Stderr, "test-matrix: one or more groups failed")
		os.Exit(1)
	}
}
