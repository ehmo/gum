package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ehmo/gum/internal/output/gain"
	"github.com/spf13/cobra"
)

// newGainCmd implements `gum gain [--session=<ID>] [--history] [--by-op]
// [--exclude-retries] [--fixture-replay] [--format=text|json|csv|toon]
// [--since=<RFC3339>] [--until=<RFC3339>]`. Time-range filtering reads each
// entry's ts field (auto-stamped on Append); legacy entries without ts
// always pass through so historical evidence is never silently dropped.
//
// No mode flag selects the spec §12.3 GainResult summary mode; --session and
// --history select the session and history modes. Each carries the
// mode-specific array the schema requires and omits the other two.
//
// --format is one enum across every path, but the report modes and the
// fixture replay read it differently: for a report it picks the renderer
// (text by default), and for --fixture-replay it picks the shaping format
// under measurement (toon by default, the format §1 validates the headline
// savings claim against). resolveGainFormat owns that split.
func newGainCmd() *cobra.Command {
	var (
		byOp           bool
		fixtureReplay  bool
		format         string
		sinceStr       string
		untilStr       string
		session        string
		history        bool
		excludeRetries bool
	)
	cmd := &cobra.Command{
		Use:   "gain",
		Short: "Show cumulative gain-ledger stats",
		Long:  "Print cumulative gain (token-savings) stats from the local ledger, or replay a fixture set with --fixture-replay.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := resolveGainFormat(format, fixtureReplay)
			if err != nil {
				return err
			}
			if fixtureReplay {
				dir := defaultFixtureReplayDir()
				result, err := gain.RunFixtureReplay(dir, resolved)
				if err != nil {
					return fmt.Errorf("fixture replay: %w", err)
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			if session != "" && history {
				return fmt.Errorf("--session and --history select different GainResult modes; pass one")
			}

			since, err := parseGainTime("--since", sinceStr)
			if err != nil {
				return err
			}
			until, err := parseGainTime("--until", untilStr)
			if err != nil {
				return err
			}

			// Read the ACTIVE profile's ledger. Passing "" read the default
			// profile's file whatever --profile said, so `gum --profile work
			// gain` reported the default profile's numbers.
			name, err := resolveProfileName(cmd)
			if err != nil {
				return err
			}
			path, err := gain.DefaultPath(name)
			if err != nil {
				return fmt.Errorf("open ledger: %w", err)
			}
			ledger, err := gain.NewLedger(path)
			if err != nil {
				return fmt.Errorf("open ledger: %w", err)
			}
			defer func() { _ = ledger.Close() }()
			filter := gain.Filter{
				Since:          since,
				Until:          until,
				Session:        session,
				ExcludeRetries: excludeRetries,
			}
			out := cmd.OutOrStdout()
			if byOp {
				return writeStatsByOp(out, resolved, ledger.StatsByOpFor(filter))
			}
			return writeGainReport(out, resolved, gainModeReport(ledger, filter, session, history, sinceStr))
		},
	}
	cmd.Flags().BoolVar(&byOp, "by-op", false, "Aggregate gain by op_id")
	cmd.Flags().BoolVar(&fixtureReplay, "fixture-replay", false, "Replay fixtures from testdata/fixtures/gain-replay")
	cmd.Flags().StringVar(&format, "format", "", "Report format: text (default), json or csv. With --fixture-replay it selects the shaping format measured, json or toon, and defaults to toon")
	cmd.Flags().StringVar(&sinceStr, "since", "", "Filter ledger entries with ts >= since (RFC3339 UTC)")
	cmd.Flags().StringVar(&untilStr, "until", "", "Filter ledger entries with ts <= until (RFC3339 UTC)")
	cmd.Flags().StringVar(&session, "session", "", "Report one session: per-op rows grouped by op_id, cache status and field-mask status")
	cmd.Flags().BoolVar(&history, "history", false, "Report savings per session grouped by op_family, in ledger order")
	cmd.Flags().BoolVar(&excludeRetries, "exclude-retries", false, "Drop is_retry=true entries from the reported aggregate; the ledger file is unchanged")
	return cmd
}

// gainModeReport builds the §12.3 GainResult for the selected mode. Exactly
// one mode array is attached: the schema treats a present-but-empty array as
// "this mode found nothing" and an absent one as "this mode was not
// selected", so attaching both would match neither oneOf branch.
func gainModeReport(ledger *gain.Ledger, filter gain.Filter, session string, history bool, sinceStr string) gain.Report {
	entries := ledger.Select(filter)
	switch {
	case history:
		report := gain.NewReport("history", gainWindow("history", sinceStr), entries)
		rows := gain.History(entries)
		report.History = &rows
		return report
	case session != "":
		report := gain.NewReport("session", "session:"+session, entries)
		rows := gain.Operations(entries)
		report.Operations = &rows
		return report
	}
	// No mode flag is summary mode, the same envelope gum.gain returns.
	report := gain.NewReport("summary", gainWindow("last-30-sessions", sinceStr), entries)
	rows := gain.Sessions(entries)
	report.Sessions = &rows
	return report
}

// writeGainReport renders one GainResult in the resolved format. json prints
// the schema envelope verbatim; text and csv render the mode array.
func writeGainReport(out io.Writer, format string, report gain.Report) error {
	switch format {
	case gainFormatJSON:
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	case gainFormatCSV:
		return renderGainCSV(out, report)
	default:
		return renderGainText(out, report)
	}
}

// writeStatsByOp renders the --by-op diagnostic in the resolved format.
func writeStatsByOp(out io.Writer, format string, byOp map[string]gain.Stats) error {
	switch format {
	case gainFormatJSON:
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(byOp)
	case gainFormatCSV:
		return renderStatsByOpCSV(out, byOp)
	default:
		return renderStatsByOpText(out, byOp)
	}
}

// gainWindow returns the GainResult window label. A --since bound replaces
// the default label, which would otherwise claim a wider window than the
// numbers cover.
func gainWindow(defaultLabel, sinceStr string) string {
	if sinceStr != "" {
		return "since:" + sinceStr
	}
	return defaultLabel
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
// relative to this source file when available. It is a var so a test can
// point the replay at a private copy; chmod-ing the repository's own fixture
// races every other package that reads the same file under `go test ./...`.
var defaultFixtureReplayDir = func() string {
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
