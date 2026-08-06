package profile_test

import (
	"encoding/json"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// historicalResponse is a one-keyword generateKeywordHistoricalMetrics body,
// trimmed to three months. It is the shape from the gum-bpx0 repro.
const historicalResponse = `{
  "results": [
    {
      "text": "lead in protein powder",
      "keywordMetrics": {
        "avgMonthlySearches": "27100",
        "competition": "LOW",
        "competitionIndex": "8",
        "lowTopOfPageBidMicros": "17833",
        "highTopOfPageBidMicros": "161304",
        "monthlySearchVolumes": [
          {"month": "JULY", "year": "2025", "monthlySearches": "390"},
          {"month": "AUGUST", "year": "2025", "monthlySearches": "320"},
          {"month": "SEPTEMBER", "year": "2025", "monthlySearches": "165000"}
        ]
      },
      "closeVariants": ["lead protein powder"]
    }
  ]
}`

// TestHistoricalProfileKeepsMonthlyVolumes pins the gum-bpx0 repro. The op's
// own summary advertises "per-month search volumes"; the profile used to drop
// them, so `--format json` returned a plausible, valid, incomplete answer. A
// caller who averaged the remaining field got a figure inflated by the one
// spike month only `--format raw` revealed.
func TestHistoricalProfileKeepsMonthlyVolumes(t *testing.T) {
	p, ok := profile.BuiltinLookup("googleads.keyword_historical.v1")
	if !ok {
		t.Fatal("googleads.keyword_historical.v1 not embedded")
	}
	out, err := profile.Apply(p, profile.ApplyInput{Body: []byte(historicalResponse), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var got struct {
		Results []struct {
			Text           string `json:"text"`
			KeywordMetrics struct {
				AvgMonthlySearches   string `json:"avgMonthlySearches"`
				MonthlySearchVolumes []struct {
					Month           string `json:"month"`
					Year            string `json:"year"`
					MonthlySearches string `json:"monthlySearches"`
				} `json:"monthlySearchVolumes"`
			} `json:"keywordMetrics"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal shaped body: %v (%s)", err, out.Body)
	}
	if len(got.Results) != 1 {
		t.Fatalf("results = %d; want 1", len(got.Results))
	}
	vols := got.Results[0].KeywordMetrics.MonthlySearchVolumes
	if len(vols) != 3 {
		t.Fatalf("monthlySearchVolumes = %d entries; want 3 (%s)", len(vols), out.Body)
	}
	if vols[2].MonthlySearches != "165000" {
		t.Errorf("September volume = %q; want 165000 — the spike the average hides", vols[2].MonthlySearches)
	}
}

// TestHistoricalProfileReportsWhatItDrops: closeVariants is still removed, and
// the caller is told so by name.
func TestHistoricalProfileReportsWhatItDrops(t *testing.T) {
	p, _ := profile.BuiltinLookup("googleads.keyword_historical.v1")
	out, err := profile.Apply(p, profile.ApplyInput{Body: []byte(historicalResponse), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertPaths(t, out.DroppedPaths, []string{"results.closeVariants"})

	notice := profile.DroppedPathsNotice(out.DroppedPaths, "--format raw", "")
	if notice == "" {
		t.Fatal("lossy profile produced no notice")
	}
}

// TestGoogleAdsProfilesAreRecoverable holds both Keyword Planner profiles to
// recovery != "none". effectiveTeeMode returns "off" for recovery = "none", so
// those profiles wrote no artifact: the dropped fields were unreachable short
// of re-running the op with --format raw, and re-running costs another upstream
// call against a quota'd API (gum-bpx0).
func TestGoogleAdsProfilesAreRecoverable(t *testing.T) {
	for _, name := range []string{"googleads.keyword_historical.v1", "googleads.keyword_ideas.v1"} {
		p, ok := profile.BuiltinLookup(name)
		if !ok {
			t.Errorf("%s not embedded", name)
			continue
		}
		if len(p.KeepFields) == 0 {
			continue // not lossy; nothing to recover
		}
		if p.Recovery == "" || p.Recovery == "none" {
			t.Errorf("%s: recovery = %q with a keep_fields whitelist; the dropped fields have no recovery path", name, p.Recovery)
		}
	}
}
