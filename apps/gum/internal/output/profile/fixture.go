package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ehmo/gum/internal/output/gain"
)

// ProfileFixtureResult is one row in the JSON output of `gum profile test`.
//
// Spec §12.1: `gum profile test --format=json` returns
//   `{"passed": bool, "fixtures": ProfileFixtureResult[], "token_budget": TokenBudgetSummary}`
//
// Failures is the list of expectation strings that did not match; an empty
// list means the fixture passed.
type ProfileFixtureResult struct {
	Name              string   `json:"name"`
	Profile           string   `json:"profile,omitempty"`
	Fixture           string   `json:"fixture"`
	Passed            bool     `json:"passed"`
	ActualFormat      string   `json:"actual_format"`
	ActualTokens      int      `json:"actual_tokens"`
	ActualResultCount int      `json:"actual_result_count,omitempty"`
	ActualOmittedCount int     `json:"actual_omitted_count,omitempty"`
	ActualLossy       bool     `json:"actual_lossy"`
	Failures          []string `json:"failures,omitempty"`
}

// TokenBudgetSummary aggregates token-ceiling assertions across fixtures.
//
// MaxTokensObserved is the highest cl100k_base token count seen across all
// fixtures; CeilingViolations counts fixtures where ActualTokens exceeded
// ExpectMaxTokens.
type TokenBudgetSummary struct {
	TotalFixtures     int `json:"total_fixtures"`
	TotalTokens       int `json:"total_tokens"`
	MaxTokensObserved int `json:"max_tokens_observed"`
	CeilingViolations int `json:"ceiling_violations"`
}

// FixtureRunResult is the top-level JSON envelope for `gum profile test --format=json`.
type FixtureRunResult struct {
	Passed      bool                   `json:"passed"`
	Fixtures    []ProfileFixtureResult `json:"fixtures"`
	TokenBudget TokenBudgetSummary     `json:"token_budget"`
}

// RunFixtures executes every [[tests]] fixture in p against the expression
// pipeline and returns a FixtureRunResult. baseDir is the directory used to
// resolve relative fixture paths (typically the directory containing the
// profile file).
//
// Token counts use cl100k_base via gain.MeasureTokensCl100k to mirror the
// release-gate counting in TestGainFixtureReplay.
func RunFixtures(p *Profile, baseDir string) (FixtureRunResult, error) {
	if p == nil {
		return FixtureRunResult{Passed: true}, nil
	}

	results := make([]ProfileFixtureResult, 0, len(p.Tests))
	allPassed := true
	budget := TokenBudgetSummary{TotalFixtures: len(p.Tests)}

	for _, t := range p.Tests {
		r, err := runOneFixture(p, t, baseDir)
		if err != nil {
			return FixtureRunResult{}, fmt.Errorf("fixture %q: %w", t.Name, err)
		}
		results = append(results, r)
		if !r.Passed {
			allPassed = false
		}
		budget.TotalTokens += r.ActualTokens
		if r.ActualTokens > budget.MaxTokensObserved {
			budget.MaxTokensObserved = r.ActualTokens
		}
		if t.ExpectMaxTokens > 0 && r.ActualTokens > t.ExpectMaxTokens {
			budget.CeilingViolations++
		}
	}

	return FixtureRunResult{
		Passed:      allPassed,
		Fixtures:    results,
		TokenBudget: budget,
	}, nil
}

// runOneFixture applies p to a single [[tests]] entry and evaluates each
// expectation. Path resolution: t.Fixture is treated as relative to baseDir
// when not absolute.
func runOneFixture(p *Profile, t TestFixture, baseDir string) (ProfileFixtureResult, error) {
	r := ProfileFixtureResult{
		Name:    t.Name,
		Profile: t.Profile,
		Fixture: t.Fixture,
	}
	if t.Fixture == "" {
		r.Failures = append(r.Failures, "fixture path is empty")
		return r, nil
	}
	path := t.Fixture
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		r.Failures = append(r.Failures, fmt.Sprintf("read fixture: %v", err))
		return r, nil
	}

	out, err := Apply(p, ApplyInput{Body: body})
	if err != nil {
		r.Failures = append(r.Failures, fmt.Sprintf("apply profile: %v", err))
		return r, nil
	}
	r.ActualFormat = out.Format

	tokens, err := gain.MeasureTokensCl100k(out.Body)
	if err != nil {
		return r, fmt.Errorf("count tokens: %w", err)
	}
	r.ActualTokens = tokens

	// Parse the shaped body for result-count / omitted-count introspection.
	// Tolerate raw or TOON bodies — those produce zero counts but no error.
	r.ActualResultCount, r.ActualOmittedCount = inspectShape(body, out)
	r.ActualLossy = isLossy(p)

	// Evaluate expectations.
	if t.ExpectFormat != "" && out.Format != t.ExpectFormat {
		r.Failures = append(r.Failures, fmt.Sprintf("expect_format=%q, got %q", t.ExpectFormat, out.Format))
	}
	if t.ExpectMaxTokens > 0 && tokens > t.ExpectMaxTokens {
		r.Failures = append(r.Failures, fmt.Sprintf("expect_max_tokens=%d, got %d tokens", t.ExpectMaxTokens, tokens))
	}
	if t.ExpectLossySet && r.ActualLossy != t.ExpectLossy {
		r.Failures = append(r.Failures, fmt.Sprintf("expect_lossy=%v, got %v", t.ExpectLossy, r.ActualLossy))
	}
	if t.ExpectResultCountSet && r.ActualResultCount != t.ExpectResultCount {
		r.Failures = append(r.Failures, fmt.Sprintf("expect_result_count=%d, got %d", t.ExpectResultCount, r.ActualResultCount))
	}
	if t.ExpectOmittedCountSet && r.ActualOmittedCount != t.ExpectOmittedCount {
		r.Failures = append(r.Failures, fmt.Sprintf("expect_omitted_count=%d, got %d", t.ExpectOmittedCount, r.ActualOmittedCount))
	}
	for _, f := range t.ExpectFields {
		if !bodyContainsField(out.Body, f) {
			r.Failures = append(r.Failures, fmt.Sprintf("expect_fields: %q not present in shaped output", f))
		}
	}

	r.Passed = len(r.Failures) == 0
	return r, nil
}

// isLossy reports whether profile p applies any data-dropping transform. The
// definition mirrors the DSL fields that, by construction, cannot be inverted
// from output alone: projection, keep/drop_fields, strip_nulls, collapse_arrays,
// truncate_strings, dedupe, and any non-zero limit.
func isLossy(p *Profile) bool {
	if p == nil {
		return false
	}
	return len(p.Projection) > 0 ||
		len(p.KeepFields) > 0 ||
		len(p.DropFields) > 0 ||
		p.StripNulls ||
		p.CollapseArrays != nil ||
		p.TruncateStrings != nil ||
		p.Dedupe != nil ||
		p.Limit > 0
}

// inspectShape returns (resultCount, omittedCount) for an applied profile by
// re-parsing the original body as JSON and counting top-level array length or
// `items` array length, then reading any `omitted_count` field. Tolerates
// non-JSON bodies (TOON, raw) by returning zeros.
func inspectShape(srcBody []byte, out ApplyOutput) (int, int) {
	// Re-decode the *source* body to get an accurate result count even when
	// the shaped output is TOON-encoded (and therefore opaque to JSON decode).
	var v any
	if err := json.Unmarshal(srcBody, &v); err != nil {
		return 0, 0
	}
	switch t := v.(type) {
	case []any:
		return len(t), 0
	case map[string]any:
		var rc, oc int
		if arr, ok := t["items"].([]any); ok {
			rc = len(arr)
		} else if arr, ok := t["data"].([]any); ok {
			rc = len(arr)
		} else if arr, ok := t["messages"].([]any); ok {
			rc = len(arr)
		}
		if n, ok := t["omitted_count"].(float64); ok {
			oc = int(n)
		}
		return rc, oc
	}
	return 0, 0
}

// bodyContainsField reports whether the shaped body contains the named field
// somewhere in its structure. Tolerates non-JSON bodies by performing a
// substring search on the field name in quoted form.
func bodyContainsField(body []byte, field string) bool {
	// Substring search is sufficient for both JSON and TOON: TOON encodes
	// field names without quotes but JSON quotes them. Match either form.
	quoted := []byte("\"" + field + "\"")
	if bytesContains(body, quoted) {
		return true
	}
	plain := []byte(field)
	return bytesContains(body, plain)
}

// bytesContains is a stdlib-free contains so the file doesn't need `bytes`
// imported alongside its other deps. (Inlined to keep this file self-contained.)
func bytesContains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
