// Package profile — regression test for integer precision in the shaping
// pipeline.
//
// Defect: Apply decoded the upstream body with json.Unmarshal into `any`, which
// turns every JSON number into a float64. float64 holds only 53 bits of
// integer, so any identifier wider than 2^53 came out of the pipeline as a
// different number. Google Ads customer ids, YouTube view counts and Drive
// quota bytes all exceed that. The body was silently corrupted: JSON output
// returned 1234567890123456800 for 1234567890123456789, and TOON returned
// 1234567890123456768.
package profile

import (
	"strings"
	"testing"
)

// TestApplyPreservesLargeIntegerPrecision asserts a >2^53 integer survives the
// pipeline byte for byte in both output formats.
func TestApplyPreservesLargeIntegerPrecision(t *testing.T) {
	const body = `{"customerId":1234567890123456789,"views":9007199254740993}`
	for _, format := range []string{"json", "toon"} {
		t.Run(format, func(t *testing.T) {
			out, err := Apply(&Profile{}, ApplyInput{Body: []byte(body), UserFormat: format})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			got := string(out.Body)
			for _, want := range []string{"1234567890123456789", "9007199254740993"} {
				if !strings.Contains(got, want) {
					t.Errorf("output lost %s (float64 round-trip); got: %s", want, got)
				}
			}
			// The digits must be a number, not a quoted string: a client that
			// parses the field as an integer must keep working.
			if strings.Contains(got, `"1234567890123456789"`) {
				t.Errorf("large integer was quoted as a string; got: %s", got)
			}
		})
	}
}

// TestApplyPreservesFractionalNumbers guards the other direction: the precision
// fix must not turn a float into an integer or a string.
func TestApplyPreservesFractionalNumbers(t *testing.T) {
	const body = `{"ratio":1.5,"micros":-0.25,"zero":0}`
	out, err := Apply(&Profile{}, ApplyInput{Body: []byte(body), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := string(out.Body)
	for _, want := range []string{`"ratio":1.5`, `"micros":-0.25`, `"zero":0`} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %s; got: %s", want, got)
		}
	}
}

// TestSortByOrdersLargeIntegersNumerically proves the numeric comparators still
// see numbers after the decode change. A string comparison would sort "1000"
// before "999".
func TestSortByOrdersLargeIntegersNumerically(t *testing.T) {
	const body = `[{"n":999},{"n":1000},{"n":9007199254740993},{"n":2}]`
	out, err := Apply(&Profile{SortBy: "n"}, ApplyInput{Body: []byte(body), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := `[{"n":2},{"n":999},{"n":1000},{"n":9007199254740993}]`
	if got := string(out.Body); got != want {
		t.Errorf("sort_by order\n got: %s\nwant: %s", got, want)
	}
}

// TestDedupeSeparatesLargeIntegersThatCollideAsFloats is the concrete data-loss
// case: 9007199254740993 and 9007199254740992 are the same float64, so dedupe
// dropped one of two distinct rows.
func TestDedupeSeparatesLargeIntegersThatCollideAsFloats(t *testing.T) {
	const body = `[{"id":9007199254740993},{"id":9007199254740992}]`
	out, err := Apply(&Profile{Dedupe: &DedupeSpec{By: []string{"id"}}},
		ApplyInput{Body: []byte(body), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := string(out.Body)
	for _, want := range []string{"9007199254740993", "9007199254740992"} {
		if !strings.Contains(got, want) {
			t.Errorf("dedupe dropped the distinct row %s; got: %s", want, got)
		}
	}
}
