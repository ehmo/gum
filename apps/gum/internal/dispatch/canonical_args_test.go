// gum-y1n acceptance: spec §10.0 Rule 4 — opt-in UTC normalization of
// RFC 3339 date-time args before JCS serialization. With normalization off,
// equivalent instants in different representations produce DIFFERENT cache
// keys (the documented Rule 3 verbatim behaviour). With normalization on,
// they collapse to the SAME key — restoring cache hit rate in Calendar /
// Gmail / Drive sessions where LLM-generated date variants are the primary
// cache driver.
//
// Coverage:
//   - normalizeDateTimeString unit cases for the spec's documented examples
//     (sub-second truncation, offset folding, malformed pass-through).
//   - normalizeArgsForJCS deep-walks nested maps and arrays.
//   - End-to-end dispatch cache-hit rate: TestDiffOnlyModeEtagReplay (the
//     bead's named acceptance) covers two equivalent-instant Dispatches and
//     asserts the adapter is called once with NormalizeDatetimes=true but
//     twice without it. It lives in http_etag_test.go, next to the §10.2
//     claims that share the name.

package dispatch

import (
	"context"
	"testing"
)

// TestNormalizeDateTimeStringSpecExamples pins the three documented examples
// from spec §10.0 Rule 4 plus the pass-through guarantees for non-datetime
// strings.
func TestNormalizeDateTimeStringSpecExamples(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "sub-second truncated to second precision",
			in:   "2026-05-19T14:30:00.999Z",
			want: "2026-05-19T14:30:00Z",
			ok:   true,
		},
		{
			name: "non-UTC offset folded to UTC",
			in:   "2026-05-19T20:00:00+05:30",
			want: "2026-05-19T14:30:00Z",
			ok:   true,
		},
		{
			name: "already UTC second-precision is idempotent",
			in:   "2026-05-19T14:30:00Z",
			want: "2026-05-19T14:30:00Z",
			ok:   true,
		},
		{
			name: "bare date passes through unchanged (spec §10.0 Rule 4 date branch)",
			in:   "2026-05-19",
			want: "2026-05-19",
			ok:   false,
		},
		{
			name: "malformed datetime passes through unchanged",
			in:   "2026-05-19T25:99:99Z",
			want: "2026-05-19T25:99:99Z",
			ok:   false,
		},
		{
			name: "arbitrary text untouched",
			in:   "hello",
			want: "hello",
			ok:   false,
		},
		{
			name: "empty string untouched",
			in:   "",
			want: "",
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeDateTimeString(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Errorf("normalizeDateTimeString(%q) = (%q, %v); want (%q, %v)",
					tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestNormalizeArgsForJCSDeepWalk asserts the normalizer walks into nested
// maps and arrays so date args buried inside e.g. {"event":{"start":"..."}}
// or {"events":[{"start":"..."}]} collapse on cache lookup just like
// top-level args.
func TestNormalizeArgsForJCSDeepWalk(t *testing.T) {
	in := map[string]any{
		"top":  "2026-05-19T14:30:00.500Z",
		"keep": "hello",
		"event": map[string]any{
			"start": "2026-05-19T20:00:00+05:30",
			"end":   "ignored-string",
		},
		"events": []any{
			map[string]any{"at": "2026-05-19T14:30:00.001Z"},
			"plain-string",
		},
	}
	got := normalizeArgsForJCS(in)
	if got["top"] != "2026-05-19T14:30:00Z" {
		t.Errorf("top = %q; want UTC-normalized", got["top"])
	}
	if got["keep"] != "hello" {
		t.Errorf("keep mutated: got %q", got["keep"])
	}
	ev := got["event"].(map[string]any)
	if ev["start"] != "2026-05-19T14:30:00Z" {
		t.Errorf("event.start = %q; want offset folded to UTC", ev["start"])
	}
	if ev["end"] != "ignored-string" {
		t.Errorf("event.end mutated: got %q", ev["end"])
	}
	arr := got["events"].([]any)
	if arr[0].(map[string]any)["at"] != "2026-05-19T14:30:00Z" {
		t.Errorf("events[0].at = %v; want UTC-normalized", arr[0])
	}
	if arr[1] != "plain-string" {
		t.Errorf("events[1] mutated: got %v", arr[1])
	}

	// Original map must be untouched (defense-in-depth: caller may reuse
	// inv.Args after canonicalization).
	if in["top"] != "2026-05-19T14:30:00.500Z" {
		t.Errorf("normalizeArgsForJCS mutated input: in[\"top\"] = %q", in["top"])
	}
}

// TestNormalizeArgsForJCSEmptyAndNilSafe — defensive: zero-length and nil maps
// must not panic and must round-trip unchanged so the canonical args of an
// empty invocation still produces the well-known "{}" hash.
func TestNormalizeArgsForJCSEmptyAndNilSafe(t *testing.T) {
	if got := normalizeArgsForJCS(nil); len(got) != 0 {
		t.Errorf("nil map normalized to non-empty: %v", got)
	}
	empty := map[string]any{}
	got := normalizeArgsForJCS(empty)
	if len(got) != 0 {
		t.Errorf("empty map normalized to non-empty: %v", got)
	}
}

// TestNormalizeValueForJCSScalarDefaultArm pins canonical_args.go:57-58
// — the `default: return v` arm of normalizeValueForJCS. Non-string,
// non-map, non-array values (numbers, bools, nil) are not datetime
// candidates and must pass through verbatim. The deep-walk test only
// exercises strings/maps/arrays, leaving the scalar default uncovered.
func TestNormalizeValueForJCSScalarDefaultArm(t *testing.T) {
	in := map[string]any{
		"count":   42,
		"ratio":   3.14,
		"enabled": true,
		"missing": nil,
	}
	got := normalizeArgsForJCS(in)
	if got["count"] != 42 {
		t.Errorf("count = %v; want 42 passed through", got["count"])
	}
	if got["ratio"] != 3.14 {
		t.Errorf("ratio = %v; want 3.14 passed through", got["ratio"])
	}
	if got["enabled"] != true {
		t.Errorf("enabled = %v; want true passed through", got["enabled"])
	}
	if v, ok := got["missing"]; !ok || v != nil {
		t.Errorf("missing = %v (present=%v); want nil passed through", v, ok)
	}
}

// AdapterFunc adapts a closure to dispatch.Adapter for inline test stubs.
type AdapterFunc func(context.Context, *Invocation, *ResolvedVariant, *Credentials) (*Response, error)

// Execute satisfies dispatch.Adapter.
func (f AdapterFunc) Execute(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) (*Response, error) {
	return f(ctx, inv, rv, creds)
}
