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
	"io"
	"os"
	"time"

	"github.com/ehmo/gum/internal/testmatrix"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run holds the whole command so tests can drive it against a fixture
// matrix without spawning a subprocess. The returned int is the process
// exit code: 0 clean, 1 a failed group, 2 a usage or parse failure.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("test-matrix", flag.ContinueOnError)
	fs.SetOutput(stderr)
	matrixPath := fs.String("matrix", "../../docs/test-matrix.md", "path to test-matrix.md (relative to -workdir)")
	workDir := fs.String("workdir", ".", "module directory containing go.mod for the `go test` invocation")
	deferredPath := fs.String("deferred", "", "newline-delimited list of expected tests outside this release scope; empty disables exceptions")
	timeout := fs.Duration("timeout", 15*time.Minute, "overall timeout for the matrix sweep")
	listOnly := fs.Bool("list", false, "print the parsed group/test plan and exit (no go test invocations)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	groups, err := testmatrix.ParseFile(*matrixPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "test-matrix: parse %s: %v\n", *matrixPath, err)
		return 2
	}
	if len(groups) == 0 {
		_, _ = fmt.Fprintf(stderr, "test-matrix: no groups parsed from %s\n", *matrixPath)
		return 2
	}

	if *listOnly {
		for _, g := range groups {
			_, _ = fmt.Fprintf(stdout, "Group %s — %s (%d tests)\n", g.Letter, g.Description, len(g.Tests))
			for _, name := range g.Tests {
				_, _ = fmt.Fprintf(stdout, "  %s\n", name)
			}
		}
		return 0
	}

	deferred := map[string]bool{}
	if *deferredPath != "" {
		deferred, err = testmatrix.ParseDeferredFile(*deferredPath)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "test-matrix: exceptions %s: %v\n", *deferredPath, err)
			return 2
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	runner := &testmatrix.Runner{WorkDir: *workDir, DeferredTests: deferred}
	results := runner.RunAll(ctx, groups)

	summary := testmatrix.Summarize(results)
	if err := summary.WriteTable(stdout); err != nil {
		_, _ = fmt.Fprintf(stderr, "test-matrix: write summary: %v\n", err)
		return 2
	}

	if summary.AnyFailed() {
		_, _ = fmt.Fprintln(stderr, "test-matrix: one or more groups failed")
		return 1
	}
	return 0
}
