// Package coverage implements the per-package line-coverage gate for the
// tracked Go surface (./internal/... and ./cmd/gum/...). It serves two
// roles:
//
//   - FloorPercent is the absolute minimum any *un-listed* (e.g. brand new)
//     gated package must meet. Bead gum-b22o.5.
//   - Ratchets is the per-package retention baseline table (bead gum-5wkg):
//     every tracked package is pinned at floor(current − ~1% jitter
//     headroom) so a localized regression in any single package fails CI
//     even while others stay green. A baseline Min may sit *above* or
//     *below* FloorPercent — it captures the level actually achieved, not
//     an aspiration.
//
// A ratchet only goes up. When a package's measured coverage exceeds its
// baseline Min by RatchetOpportunityMargin, cmd/coverage-floor prints a
// non-failing RATCHET_OPPORTUNITY hint so the baseline can be tightened.
// Lowering a Min (or dropping its Bead reference) without a matching
// docs/test-matrix.md update is a regression.
package coverage

import (
	"fmt"
	"sort"
	"strings"
)

// FloorPercent is the absolute line-coverage floor for any gated package
// that does NOT carry a Ratchet baseline (e.g. a newly added package).
// New packages MUST land at or above this number until they earn a
// tighter retention baseline in Ratchets.
const FloorPercent = 85.0

// RatchetOpportunityMargin is how far a package's measured coverage must
// exceed its baseline Min before cmd/coverage-floor suggests tightening
// the ratchet. Day-one baselines are floor(current − 1%), so the initial
// margin is always < 2.0 and no spurious hint fires until coverage
// genuinely climbs.
const RatchetOpportunityMargin = 2.0

// Ratchet declares the per-package retention baseline. Min is the minimum
// acceptable line coverage in percent; readings strictly below Min fail
// the gate. Bead is the reference that owns the baseline (and any future
// tightening). Unlike the original below-floor-exception model, Min may
// be above or below FloorPercent — it records the level actually held.
type Ratchet struct {
	Package string
	Min     float64
	Bead    string
}

// Ratchets pins every tracked package at floor(current − ~1% jitter
// headroom). gum-5wkg established the original retention sweep
// (2026-05-26); gum-8ilq refreshed stale baselines after the hardened audit
// remediation sweep (2026-06-03). The trailing comment on each line is the
// measured coverage that produced the Min. Lowering a Min or dropping its
// Bead without a docs/test-matrix.md update is a regression; raising a Min
// when coverage improves is encouraged.
var Ratchets = []Ratchet{
	{Package: "github.com/ehmo/gum/internal/fsatomic", Min: 58.0, Bead: "gum-x5vw"},           // 59.3% — residual is defensive I/O-error branches (chmod/sync/close/write failures) not portably triggerable
	{Package: "github.com/ehmo/gum/internal/adapters/googleads", Min: 85.0, Bead: "gum-x5vw"}, // 86.7%
	{Package: "github.com/ehmo/gum/internal/help/topics", Min: 79.0, Bead: "gum-5wkg"},        // 80.0%
	{Package: "github.com/ehmo/gum/internal/plugins/registry", Min: 82.0, Bead: "gum-5wkg"},   // 83.5%
	{Package: "github.com/ehmo/gum/internal/catalog", Min: 84.0, Bead: "gum-ql6c"},            // 84.6% — release rehearsal recalibration after catalog breadth growth
	{Package: "github.com/ehmo/gum/cmd/gum", Min: 86.0, Bead: "gum-ejek"},                     // 86.9% on Linux Go 1.26.4 public CI; macOS/local may report slightly higher
	{Package: "github.com/ehmo/gum/internal/initpkg", Min: 88.0, Bead: "gum-5wkg"},            // 89.1%
	{Package: "github.com/ehmo/gum/internal/profile", Min: 90.0, Bead: "gum-8ilq"},            // 91.7%
	{Package: "github.com/ehmo/gum/internal/testutil/golden", Min: 91.0, Bead: "gum-5wkg"},    // 92.1%
	{Package: "github.com/ehmo/gum/internal/adapters/genai", Min: 91.0, Bead: "gum-5wkg"},     // 92.3%
	{Package: "github.com/ehmo/gum/internal/auditlog", Min: 91.0, Bead: "gum-5wkg"},           // 92.3%
	{Package: "github.com/ehmo/gum/internal/auth", Min: 92.0, Bead: "gum-8ilq"},               // 93.1%
	{Package: "github.com/ehmo/gum/internal/coverage", Min: 91.0, Bead: "gum-ql6c"},           // 91.5% — release rehearsal recalibration
	{Package: "github.com/ehmo/gum/internal/sandbox/risor", Min: 91.0, Bead: "gum-ql6c"},      // 91.8% — release rehearsal recalibration after gum.code hardening
	{Package: "github.com/ehmo/gum/internal/output/gain", Min: 91.0, Bead: "gum-ql6c"},        // 91.2% — release rehearsal recalibration
	{Package: "github.com/ehmo/gum/internal/testmatrix", Min: 92.0, Bead: "gum-5wkg"},         // 93.4%
	{Package: "github.com/ehmo/gum/internal/adapters", Min: 90.0, Bead: "gum-ql6c"},           // 90.3% — release rehearsal recalibration after gum.code dispatch wiring
	{Package: "github.com/ehmo/gum/internal/sanitize", Min: 93.0, Bead: "gum-5wkg"},           // 94.1%
	{Package: "github.com/ehmo/gum/internal/dispatch", Min: 93.0, Bead: "gum-8ilq"},           // 94.4%
	{Package: "github.com/ehmo/gum/internal/cache", Min: 92.0, Bead: "gum-ql6c"},              // 92.3% — release rehearsal recalibration
	{Package: "github.com/ehmo/gum/internal/adapters/maps", Min: 93.0, Bead: "gum-5wkg"},      // 94.6%
	{Package: "github.com/ehmo/gum/internal/notify", Min: 94.0, Bead: "gum-8ilq"},             // 95.7%
	{Package: "github.com/ehmo/gum/internal/plugins", Min: 94.0, Bead: "gum-5wkg"},            // 95.0%
	{Package: "github.com/ehmo/gum/internal/embed", Min: 95.0, Bead: "gum-5wkg"},              // 96.2%
	{Package: "github.com/ehmo/gum/internal/lro", Min: 95.0, Bead: "gum-5wkg"},                // 96.3%
	{Package: "github.com/ehmo/gum/internal/output/profile", Min: 95.0, Bead: "gum-8ilq"},     // 96.2%
	{Package: "github.com/ehmo/gum/internal/output/tee", Min: 94.0, Bead: "gum-ql6c"},         // 94.6% — release rehearsal recalibration
	{Package: "github.com/ehmo/gum/internal/bench", Min: 96.0, Bead: "gum-5wkg"},              // 97.3%
	{Package: "github.com/ehmo/gum/internal/lro/routing", Min: 96.0, Bead: "gum-5wkg"},        // 97.4%
	{Package: "github.com/ehmo/gum/internal/mcp", Min: 96.0, Bead: "gum-5wkg"},                // 97.8%
	{Package: "github.com/ehmo/gum/internal/config", Min: 97.0, Bead: "gum-5wkg"},             // 98.1%
	{Package: "github.com/ehmo/gum/internal/cli/callargs", Min: 97.0, Bead: "gum-8ilq"},       // 98.9%
	{Package: "github.com/ehmo/gum/internal/output/toon", Min: 92.0, Bead: "gum-ql6c"},        // 92.0% — release rehearsal recalibration after typed decoder expansion
	{Package: "github.com/ehmo/gum/internal/output/jcs", Min: 98.0, Bead: "gum-5wkg"},         // 99.1%
	{Package: "github.com/ehmo/gum/internal/adapters/grpc", Min: 99.0, Bead: "gum-5wkg"},      // 100.0%
	{Package: "github.com/ehmo/gum/internal/help", Min: 99.0, Bead: "gum-5wkg"},               // 100.0%
	{Package: "github.com/ehmo/gum/internal/httputil", Min: 99.0, Bead: "gum-5wkg"},           // 100.0%
	{Package: "github.com/ehmo/gum/internal/output/fieldmask", Min: 99.0, Bead: "gum-5wkg"},   // 100.0%
	{Package: "github.com/ehmo/gum/internal/pluginenv", Min: 61.0, Bead: "gum-ejek"},          // 61.1% on Linux Go 1.26.4 public CI; darwin-only backend files make macOS report 100%
}

// GatedPackages returns the `go test` patterns whose coverage MUST be
// measured. ./internal/... and ./cmd/gum/... are the full tracked source
// surface; build-time tooling (cmd/gen-*, cmd/measure-tier-a,
// cmd/test-matrix, cmd/coverage-floor) and the generated, gitignored
// gen/dispatch tree are intentionally excluded.
func GatedPackages() []string {
	return []string{
		"./cmd/gum/...",
		"./internal/...",
	}
}

// Threshold returns the required minimum coverage for pkg: the matching
// Ratchet.Min if one exists, otherwise FloorPercent.
func Threshold(pkg string) float64 {
	for _, r := range Ratchets {
		if r.Package == pkg {
			return r.Min
		}
	}
	return FloorPercent
}

// Reading captures the measured line-coverage percentage for one Go
// package, in the same units `go tool cover -func` emits.
type Reading struct {
	Package  string
	Percent  float64
	HasTests bool
}

// Violation is one package whose Reading falls below the package's
// effective threshold (ratcheted or FloorPercent).
type Violation struct {
	Reading   Reading
	Threshold float64
}

// Check evaluates readings against the floor, returning the violations
// sorted by package import path. Packages absent from readings are NOT
// flagged here; the caller is responsible for invoking the right test
// command. Empty-test packages (HasTests=false) are skipped because
// "no test files" is a different problem class than insufficient
// coverage and is enforced by separate gates.
func Check(readings []Reading) []Violation {
	var violations []Violation
	for _, r := range readings {
		if !r.HasTests {
			continue
		}
		need := Threshold(r.Package)
		if r.Percent < need {
			violations = append(violations, Violation{Reading: r, Threshold: need})
		}
	}
	sort.Slice(violations, func(i, j int) bool {
		return violations[i].Reading.Package < violations[j].Reading.Package
	})
	return violations
}

// FormatViolations renders violations for the CI failure message. The
// shape is "<pkg>: <actual>% < <required>% (ratchet: <bead>)" with one
// line per violation; an empty slice returns "".
func FormatViolations(violations []Violation) string {
	if len(violations) == 0 {
		return ""
	}
	var b strings.Builder
	for _, v := range violations {
		fmt.Fprintf(&b, "%s: %.1f%% < %.1f%%",
			v.Reading.Package, v.Reading.Percent, v.Threshold)
		if r, ok := ratchetFor(v.Reading.Package); ok {
			fmt.Fprintf(&b, " (ratchet bead: %s)", r.Bead)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func ratchetFor(pkg string) (Ratchet, bool) {
	for _, r := range Ratchets {
		if r.Package == pkg {
			return r, true
		}
	}
	return Ratchet{}, false
}

// Opportunity is a ratcheted package whose measured coverage now exceeds
// its baseline Min by at least RatchetOpportunityMargin — a candidate for
// tightening the ratchet upward.
type Opportunity struct {
	Reading Reading
	Min     float64
}

// Opportunities returns the ratchet-tightening candidates among readings:
// tested packages whose actual coverage is >= Min + RatchetOpportunityMargin.
// Packages with no ratchet entry (gated only by FloorPercent) and empty-test
// packages are never candidates. Results are sorted by import path. This
// gate never fails the build; cmd/coverage-floor surfaces it as a hint.
func Opportunities(readings []Reading) []Opportunity {
	var out []Opportunity
	for _, r := range readings {
		if !r.HasTests {
			continue
		}
		rat, ok := ratchetFor(r.Package)
		if !ok {
			continue
		}
		if r.Percent >= rat.Min+RatchetOpportunityMargin {
			out = append(out, Opportunity{Reading: r, Min: rat.Min})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Reading.Package < out[j].Reading.Package
	})
	return out
}
