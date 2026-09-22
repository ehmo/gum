package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/ehmo/gum/internal/output/gain"
)

// The `gum gain --format` enum (spec §12.3). text, json and csv render the
// report modes; toon names the shaping format a --fixture-replay measures and
// is rejected on every other path.
const (
	gainFormatText = "text"
	gainFormatJSON = "json"
	gainFormatCSV  = "csv"
	gainFormatTOON = "toon"
)

// tokenizerDisclosure is the §2 provider-skew annotation. Every human-readable
// gain surface carries it, because the numbers are cl100k_base wire tokens and
// a provider's billing tokenizer counts differently.
const tokenizerDisclosure = "Token counts are a cl100k_base estimate; provider billing may differ."

// resolveGainFormat maps the raw --format value onto the format the selected
// mode will render in. An unset flag resolves to the mode's default: text for
// the report modes, toon for --fixture-replay, which measures the shaping
// format the §1 headline savings claim is validated against.
func resolveGainFormat(raw string, fixtureReplay bool) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if fixtureReplay {
		switch raw {
		case "":
			return gainFormatTOON, nil
		case gainFormatJSON, gainFormatTOON:
			return raw, nil
		}
		return "", cliArgInvalid(fmt.Sprintf(
			"--fixture-replay measures a shaping format, so --format must be json or toon, not %q", raw))
	}

	switch raw {
	case "":
		return gainFormatText, nil
	case gainFormatText, gainFormatJSON, gainFormatCSV:
		return raw, nil
	case gainFormatTOON:
		return "", cliArgInvalid(
			"--format=toon names a fixture shaping format and is valid only with --fixture-replay")
	}
	return "", cliArgInvalid(fmt.Sprintf("unknown --format %q: want text|json|csv", raw))
}

// renderGainText writes one GainResult as a totals block plus the table for
// whichever mode array the report carries.
func renderGainText(w io.Writer, r gain.Report) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "mode      %s\n", r.Mode)
	fmt.Fprintf(&sb, "window    %s\n", r.Window)
	fmt.Fprintf(&sb, "tokenizer %s\n\n", r.Tokenizer)
	fmt.Fprintf(&sb, "baseline  %d\n", r.BaselineTokens)
	fmt.Fprintf(&sb, "actual    %d\n", r.ActualTokens)
	fmt.Fprintf(&sb, "saved     %d (%s)\n", r.SavingsTokens, gainPctLabel(r.SavingsPct))
	fmt.Fprintf(&sb, "end-to-end savings      %s\n", gainPctLabel(r.EndToEndSavings))
	fmt.Fprintf(&sb, "per-op shaping savings  %s\n", gainPctLabel(r.PerOpShapingSavings))
	fmt.Fprintf(&sb, "batch envelope overhead %d\n\n", r.BatchEnvelopeOverhead)

	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	switch {
	case r.Sessions != nil:
		fmt.Fprintln(tw, "SESSION\tCALLS\tBASELINE\tACTUAL\tSAVINGS%\tOP_FAMILIES")
		for _, row := range *r.Sessions {
			fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%s\t%s\n",
				row.Session, row.Calls, row.BaselineTokens, row.ActualTokens,
				gainPct(row.SavingsPct), strings.Join(row.OpFamilies, ","))
		}
	case r.Operations != nil:
		fmt.Fprintln(tw, "OP_ID\tOP_FAMILY\tCALLS\tBASELINE\tACTUAL\tCACHE\tFIELD_MASK")
		for _, row := range *r.Operations {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
				row.OpID, row.OpFamily, row.Calls, row.BaselineTokens,
				row.ActualTokens, row.CacheStatus, row.FieldMaskStatus)
		}
	case r.History != nil:
		fmt.Fprintln(tw, "SESSION\tOP_FAMILY\tBASELINE\tACTUAL\tSAVINGS%")
		for _, row := range *r.History {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\n",
				row.Session, row.OpFamily, row.BaselineTokens, row.ActualTokens,
				gainPct(row.SavingsPct))
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(&sb, "\n%s\n", tokenizerDisclosure)
	_, err := io.WriteString(w, sb.String())
	return err
}

// renderGainCSV writes one CSV row per displayed detail row (spec §12.3). The
// totals block has no CSV form: it is one record of a different shape, and
// emitting it as a second header would break every consumer that reads the
// stream with a standard CSV reader.
func renderGainCSV(w io.Writer, r gain.Report) error {
	cw := csv.NewWriter(w)
	switch {
	case r.Sessions != nil:
		if err := cw.Write([]string{"session", "calls", "baseline_tokens", "actual_tokens", "savings_pct", "op_families"}); err != nil {
			return err
		}
		for _, row := range *r.Sessions {
			if err := cw.Write([]string{
				row.Session, gainItoa(row.Calls), gainItoa(row.BaselineTokens), gainItoa(row.ActualTokens),
				gainPct(row.SavingsPct), strings.Join(row.OpFamilies, ","),
			}); err != nil {
				return err
			}
		}
	case r.Operations != nil:
		if err := cw.Write([]string{"op_id", "op_family", "calls", "baseline_tokens", "actual_tokens", "cache_status", "field_mask_status"}); err != nil {
			return err
		}
		for _, row := range *r.Operations {
			if err := cw.Write([]string{
				row.OpID, row.OpFamily, gainItoa(row.Calls), gainItoa(row.BaselineTokens),
				gainItoa(row.ActualTokens), row.CacheStatus, row.FieldMaskStatus,
			}); err != nil {
				return err
			}
		}
	case r.History != nil:
		if err := cw.Write([]string{"session", "op_family", "baseline_tokens", "actual_tokens", "savings_pct"}); err != nil {
			return err
		}
		for _, row := range *r.History {
			if err := cw.Write([]string{
				row.Session, row.OpFamily, gainItoa(row.BaselineTokens),
				gainItoa(row.ActualTokens), gainPct(row.SavingsPct),
			}); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

// renderStatsByOpText writes the --by-op diagnostic as a table. --by-op is not
// a §12.3 GainResult mode: it reports raw per-op Stats, percentiles included,
// so it has its own columns.
func renderStatsByOpText(w io.Writer, byOp map[string]gain.Stats) error {
	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "OP_ID\tCALLS\tBASELINE\tSAVED\tSAVINGS%\tMEAN\tP50\tP95\tP99")
	for _, opID := range sortedOpIDs(byOp) {
		s := byOp[opID]
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%.1f\t%.1f\t%d\t%d\t%d\n",
			opID, s.TotalCalls, s.TotalTokensIn, s.TotalTokensSaved,
			s.AggregateSavingsPct*100, s.MeanSavingsPerCall, s.P50, s.P95, s.P99)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(&sb, "\n%s\n", tokenizerDisclosure)
	_, err := io.WriteString(w, sb.String())
	return err
}

// renderStatsByOpCSV writes the --by-op diagnostic as one CSV row per op_id.
func renderStatsByOpCSV(w io.Writer, byOp map[string]gain.Stats) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"op_id", "total_calls", "total_tokens_in", "total_tokens_saved",
		"aggregate_savings_pct", "mean_savings_per_call", "p50", "p95", "p99",
	}); err != nil {
		return err
	}
	for _, opID := range sortedOpIDs(byOp) {
		s := byOp[opID]
		if err := cw.Write([]string{
			opID, gainItoa(s.TotalCalls), gainItoa(s.TotalTokensIn), gainItoa(s.TotalTokensSaved),
			strconv.FormatFloat(s.AggregateSavingsPct*100, 'f', 1, 64),
			strconv.FormatFloat(s.MeanSavingsPerCall, 'f', 1, 64),
			gainItoa(s.P50), gainItoa(s.P95), gainItoa(s.P99),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// sortedOpIDs returns the map's keys in op_id order so two runs over one
// ledger print the same rows in the same order.
func sortedOpIDs(byOp map[string]gain.Stats) []string {
	ids := make([]string, 0, len(byOp))
	for opID := range byOp {
		ids = append(ids, opID)
	}
	sort.Strings(ids)
	return ids
}

// gainPct formats a GainResult percentage. A nil pointer means the subset had
// no baseline to divide by, which is not the same claim as zero savings, so it
// prints as "-" rather than "0.0".
func gainPct(pct *float64) string {
	if pct == nil {
		return "-"
	}
	return strconv.FormatFloat(*pct, 'f', 1, 64)
}

// gainPctLabel formats a percentage for the totals block, where there is no
// column header to carry the unit. An unknown percentage reads "n/a" rather
// than a bare dash followed by a percent sign.
func gainPctLabel(pct *float64) string {
	if pct == nil {
		return "n/a"
	}
	return strconv.FormatFloat(*pct, 'f', 1, 64) + "%"
}

// gainItoa formats an int64 column cell.
func gainItoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
