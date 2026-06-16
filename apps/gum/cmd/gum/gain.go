package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ehmo/gum/internal/output/gain"
	"github.com/spf13/cobra"
)

// newGainCmd implements `gum gain [--by-op] [--fixture-replay] [--format=json|toon]
// [--since=<RFC3339>] [--until=<RFC3339>]`. Time-range filtering reads each
// entry's ts field (auto-stamped on Append); legacy entries without ts
// always pass through so historical evidence is never silently dropped.
func newGainCmd() *cobra.Command {
	var (
		byOp          bool
		fixtureReplay bool
		format        string
		sinceStr      string
		untilStr      string
	)
	cmd := &cobra.Command{
		Use:   "gain",
		Short: "Show cumulative gain-ledger stats",
		Long:  "Print cumulative gain (token-savings) stats from the local ledger, or replay a fixture set with --fixture-replay.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if fixtureReplay {
				dir := defaultFixtureReplayDir()
				result, err := gain.RunFixtureReplay(dir, format)
				if err != nil {
					return fmt.Errorf("fixture replay: %w", err)
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			since, err := parseGainTime("--since", sinceStr)
			if err != nil {
				return err
			}
			until, err := parseGainTime("--until", untilStr)
			if err != nil {
				return err
			}

			ledger, err := gain.NewLedger("")
			if err != nil {
				return fmt.Errorf("open ledger: %w", err)
			}
			defer func() { _ = ledger.Close() }()
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			if byOp {
				return enc.Encode(ledger.StatsByOp(since, until))
			}
			if since.IsZero() && until.IsZero() {
				return enc.Encode(ledger.Stats())
			}
			return enc.Encode(ledger.StatsBetween(since, until))
		},
	}
	cmd.Flags().BoolVar(&byOp, "by-op", false, "Aggregate gain by op_id")
	cmd.Flags().BoolVar(&fixtureReplay, "fixture-replay", false, "Replay fixtures from testdata/fixtures/gain-replay")
	cmd.Flags().StringVar(&format, "format", "toon", "Output format for --fixture-replay only (json|toon); ignored otherwise")
	cmd.Flags().StringVar(&sinceStr, "since", "", "Filter ledger entries with ts >= since (RFC3339 UTC, spec §12.3)")
	cmd.Flags().StringVar(&untilStr, "until", "", "Filter ledger entries with ts <= until (RFC3339 UTC)")
	return cmd
}

// parseGainTime accepts an RFC3339 timestamp (nano-precision tolerated)
// from the named flag. An empty value yields the zero time, which
// StatsBetween treats as an open-ended bound.
func parseGainTime(flagName, raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: invalid RFC3339 timestamp %q: %w", flagName, raw, err)
	}
	return t.UTC(), nil
}

// defaultFixtureReplayDir returns the testdata/fixtures/gain-replay directory
// relative to this source file when available.
func defaultFixtureReplayDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
		candidate := filepath.Join(repoRoot, "testdata", "fixtures", "gain-replay")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join("testdata", "fixtures", "gain-replay")
}
