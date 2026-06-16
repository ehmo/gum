package dispatch

import "time"

// canonicalArgs returns args optionally with RFC 3339 date-time string values
// UTC-normalized per spec §10.0 Rule 4, gated by the dispatcher's
// normalizeDatetimes flag (DispatcherConfig.NormalizeDatetimes, surfaced as
// `cache.normalize_datetimes` profile config). When the flag is off the args
// are returned verbatim — Rule 3 (verbatim strings) applies. When on, every
// string value parseable as RFC 3339 is converted to UTC with second
// precision before the args reach JCS serialization, so semantically-equal
// datetime variants collapse to the same cache key / args_hash / tee hash.
//
// The original args map is never mutated; on normalization a shallow copy is
// produced and nested maps/arrays are deep-walked.
func (d *dispatcher) canonicalArgs(args map[string]any) map[string]any {
	if !d.normalizeDatetimes {
		return args
	}
	return normalizeArgsForJCS(args)
}

// normalizeArgsForJCS deep-walks args and returns a copy where every string
// value parseable as RFC 3339 date-time is replaced with its UTC, second-
// precision serialization. Date-only strings (YYYY-MM-DD) and full ISO
// datetimes truncated to dates are not currently distinguished from upstream
// schema annotations; the v0.1.0 implementation normalizes any string that
// successfully parses as RFC 3339 and leaves all other values unchanged. This
// satisfies the cache-hit-rate intent of §10.0 Rule 4 without requiring a
// per-arg schema lookup in the catalog.
func normalizeArgsForJCS(args map[string]any) map[string]any {
	if len(args) == 0 {
		return args
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = normalizeValueForJCS(v)
	}
	return out
}

func normalizeValueForJCS(v any) any {
	switch x := v.(type) {
	case string:
		if normalized, ok := normalizeDateTimeString(x); ok {
			return normalized
		}
		return x
	case map[string]any:
		return normalizeArgsForJCS(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeValueForJCS(item)
		}
		return out
	default:
		return v
	}
}

// normalizeDateTimeString reports whether s parses as RFC 3339 date-time and,
// if so, returns its UTC second-precision serialization. Sub-second components
// are truncated (not rounded) per spec §10.0 Rule 4:
//
//	2026-05-19T14:30:00.999Z   -> 2026-05-19T14:30:00Z
//	2026-05-19T20:00:00+05:30  -> 2026-05-19T14:30:00Z
//
// Strings that do not parse as RFC 3339 — including bare dates like
// "2026-05-19", arbitrary text, and malformed datetimes — pass through
// unchanged (ok=false), matching the spec's "malformed values pass through"
// guarantee. Bare dates are intentionally NOT promoted to datetimes here:
// the spec's date branch says "pass through unchanged".
func normalizeDateTimeString(s string) (string, bool) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC().Format("2006-01-02T15:04:05Z"), true
	}
	return s, false
}
