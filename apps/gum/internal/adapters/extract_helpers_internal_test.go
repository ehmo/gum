package adapters

import (
	"testing"
	"time"
)

// TestExtractIntArg covers every numeric branch (int/int64/float64) plus
// the missing-key and wrong-type fall-throughs. The function exists to
// paper over the JSON-numbers-are-float64 footgun, so each branch must
// resolve cleanly to a Go int.
func TestExtractIntArg(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		key  string
		want int
	}{
		{"missing_key_zero", map[string]any{}, "n", 0},
		{"int_native", map[string]any{"n": 7}, "n", 7},
		{"int64_native", map[string]any{"n": int64(8)}, "n", 8},
		{"float64_from_json", map[string]any{"n": float64(9)}, "n", 9},
		{"wrong_type_string", map[string]any{"n": "10"}, "n", 0},
		{"wrong_type_bool", map[string]any{"n": true}, "n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractIntArg(tc.args, tc.key); got != tc.want {
				t.Errorf("got %d; want %d", got, tc.want)
			}
		})
	}
}

// TestExtractRateLimitedPause covers all five branches: nil result,
// missing error, non-RATE_LIMITED code, RATE_LIMITED with each numeric
// retry_after_ms type, and the zero/missing-retry_after fallback to the
// 60s default. This pins the gum-parallel pacing contract — drifting any
// of these would silently turn a quota hint into a hang.
func TestExtractRateLimitedPause(t *testing.T) {
	cases := []struct {
		name   string
		result map[string]any
		want   time.Duration
	}{
		{"nil_result_zero", nil, 0},
		{"no_error_zero", map[string]any{}, 0},
		{"non_rate_limited_zero", map[string]any{"error": map[string]any{"error_code": "TIMEOUT"}}, 0},
		{
			"rate_limited_int64_honored",
			map[string]any{"error": map[string]any{"error_code": "RATE_LIMITED", "retry_after_ms": int64(250)}},
			250 * time.Millisecond,
		},
		{
			"rate_limited_int_honored",
			map[string]any{"error": map[string]any{"error_code": "RATE_LIMITED", "retry_after_ms": int(125)}},
			125 * time.Millisecond,
		},
		{
			"rate_limited_float64_honored",
			map[string]any{"error": map[string]any{"error_code": "RATE_LIMITED", "retry_after_ms": float64(500)}},
			500 * time.Millisecond,
		},
		{
			"rate_limited_zero_falls_back_to_default",
			map[string]any{"error": map[string]any{"error_code": "RATE_LIMITED", "retry_after_ms": int64(0)}},
			parallel429DefaultRetryAfter,
		},
		{
			"rate_limited_missing_retry_falls_back_to_default",
			map[string]any{"error": map[string]any{"error_code": "RATE_LIMITED"}},
			parallel429DefaultRetryAfter,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractRateLimitedPause(tc.result); got != tc.want {
				t.Errorf("got %v; want %v", got, tc.want)
			}
		})
	}
}
