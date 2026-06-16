// Command coverage-floor measures per-package line coverage for the
// dispatch kernel and output pipeline and exits non-zero when any
// package falls below its declared threshold (FloorPercent, or a
// ratcheted minimum from internal/coverage.Ratchets).
//
// Spec source of truth: bead gum-b22o.5. The single canonical floor
// is internal/coverage.FloorPercent. Per-package ratchets capture
// current-state minimums for packages with follow-up beads tracking
// the lift to FloorPercent.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ehmo/gum/internal/coverage"
)

func main() {
	workDir := flag.String("workdir", ".", "module directory containing go.mod for the `go test` invocation")
	flag.Parse()

	readings, err := coverage.Measure(coverage.MeasureOptions{WorkDir: *workDir})
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage-floor: measure: %v\n", err)
		os.Exit(2)
	}

	violations := coverage.Check(readings)

	fmt.Printf("%-60s %-9s %-9s\n", "PACKAGE", "COVERAGE", "FLOOR")
	fmt.Println(strings.Repeat("-", 80))
	for _, r := range readings {
		if !r.HasTests {
			fmt.Printf("%-60s %-9s %-9s\n", r.Package, "n/a", "n/a (no tests)")
			continue
		}
		need := coverage.Threshold(r.Package)
		mark := "ok"
		if r.Percent < need {
			mark = "FAIL"
		}
		fmt.Printf("%-60s %7.2f%%  %5.1f%%  %s\n", r.Package, r.Percent, need, mark)
	}
	fmt.Println()

	// Warn-only: surface packages whose coverage has climbed far enough
	// above their retention baseline to justify tightening the ratchet.
	// This never affects the exit code.
	if opps := coverage.Opportunities(readings); len(opps) > 0 {
		fmt.Println("RATCHET_OPPORTUNITY: coverage now exceeds the baseline by " +
			fmt.Sprintf("%.1f%%+; consider raising these ratchet Mins in internal/coverage.Ratchets:", coverage.RatchetOpportunityMargin))
		for _, o := range opps {
			fmt.Printf("  %s: %.2f%% (baseline %.1f%%)\n", o.Reading.Package, o.Reading.Percent, o.Min)
		}
		fmt.Println()
	}

	if len(violations) > 0 {
		fmt.Fprintln(os.Stderr, "coverage-floor: per-package floor violations:")
		fmt.Fprint(os.Stderr, coverage.FormatViolations(violations))
		fmt.Fprintf(os.Stderr, "\nFloor: %.1f%% (internal/coverage.FloorPercent); ratchets track follow-up beads.\n",
			coverage.FloorPercent)
		os.Exit(1)
	}
}
