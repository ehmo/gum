package auth_test

import (
	"errors"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/catalog"
)

// catchPanic calls fn and returns any panic value as an error string, or ("", false).
// This lets tests assert on panicking stub behaviour without killing the test binary.
func catchPanic(fn func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg = "panic: not implemented"
			panicked = true
		}
	}()
	fn()
	return "", false
}

// TestAuthStrategyClosedEnum verifies that every unknown strategy string surfaces
// as ErrUnknownStrategy (G3.1 — first half).
func TestAuthStrategyClosedEnum(t *testing.T) {
	defer goleak.VerifyNone(t)

	unknownValues := []catalog.AuthStrategy{
		"totally_unknown",
		"",
		"GUM_OAUTH", // wrong case
		"BYO_OAUTH", // wrong case
	}

	for _, bad := range unknownValues {
		bad := bad
		v := &catalog.Variant{AuthStrategy: bad}
		var gotErr error
		var gotStrat auth.Strategy

		msg, panicked := catchPanic(func() {
			gotStrat, gotErr = auth.Resolve(t.Context(), v)
			_ = gotStrat
		})
		if panicked {
			t.Errorf("Resolve(%q): panicked (%s); green team must return ErrUnknownStrategy without panicking", bad, msg)
			continue
		}
		if gotErr == nil {
			t.Errorf("Resolve(%q): expected ErrUnknownStrategy, got nil", bad)
			continue
		}
		if !errors.Is(gotErr, auth.ErrUnknownStrategy) {
			t.Errorf("Resolve(%q): expected errors.Is(err, ErrUnknownStrategy), got: %v", bad, gotErr)
		}
	}
}

// TestAuthStrategyImplementedSet asserts (G3.1 — second half):
//   - byo_oauth and adc: Acquire must NOT return AUTH_STRATEGY_NOT_IMPLEMENTED.
//   - The other 6 strategies: Acquire must return AUTH_STRATEGY_NOT_IMPLEMENTED.
func TestAuthStrategyImplementedSet(t *testing.T) {
	defer goleak.VerifyNone(t)

	implementedCases := []struct {
		name  string
		strat auth.Strategy
	}{
		{"byo_oauth", auth.StrategyBYOOAuth},
		{"adc", auth.StrategyADC},
		{"api_key", auth.StrategyAPIKey},
		{"service_account_key", auth.StrategyServiceAccountKey},
		// gum_oauth is implemented via NewGumOAuth() (manifest-gated);
		// Acquire returns AUTH_ACQUIRE_REQUIRES_INSTANCE rather than
		// AUTH_STRATEGY_NOT_IMPLEMENTED.
		{"gum_oauth", auth.StrategyGUMOAuth},
	}

	for _, tc := range implementedCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var gotErr error
			msg, panicked := catchPanic(func() {
				_, gotErr = auth.Acquire(t.Context(), tc.strat, []string{"https://www.googleapis.com/auth/gmail.readonly"})
			})
			if panicked {
				t.Errorf("strategy %q: panicked (%s); green team must return a real error or credentials", tc.name, msg)
				return
			}
			if errors.Is(gotErr, auth.ErrAuthStrategyNotImplemented) {
				t.Errorf("strategy %q must not return AUTH_STRATEGY_NOT_IMPLEMENTED (it is implemented)", tc.name)
			}
		})
	}

	stubbedCases := []struct {
		name  string
		strat auth.Strategy
	}{
		{"workload_identity", auth.StrategyWorkloadIdentity},
		{"impersonation", auth.StrategyImpersonation},
		{"none", auth.StrategyNone},
	}

	for _, tc := range stubbedCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var gotErr error
			msg, panicked := catchPanic(func() {
				_, gotErr = auth.Acquire(t.Context(), tc.strat, nil)
			})
			if panicked {
				t.Errorf("strategy %q: panicked (%s); green team must return AUTH_STRATEGY_NOT_IMPLEMENTED without panicking", tc.name, msg)
				return
			}
			if gotErr == nil {
				t.Errorf("strategy %q: expected AUTH_STRATEGY_NOT_IMPLEMENTED, got nil", tc.name)
				return
			}
			if !errors.Is(gotErr, auth.ErrAuthStrategyNotImplemented) {
				t.Errorf("strategy %q: expected errors.Is(err, AUTH_STRATEGY_NOT_IMPLEMENTED), got: %v", tc.name, gotErr)
			}
		})
	}
}

// TestStrategyStringCanonical verifies that Strategy.String() returns the
// canonical catalog name for each of the 8 known strategies.
func TestStrategyStringCanonical(t *testing.T) {
	defer goleak.VerifyNone(t)

	cases := []struct {
		strat auth.Strategy
		want  string
	}{
		{auth.StrategyGUMOAuth, "gum_oauth"},
		{auth.StrategyBYOOAuth, "byo_oauth"},
		{auth.StrategyADC, "adc"},
		{auth.StrategyAPIKey, "api_key"},
		{auth.StrategyServiceAccountKey, "service_account_key"},
		{auth.StrategyWorkloadIdentity, "workload_identity"},
		{auth.StrategyImpersonation, "impersonation"},
		{auth.StrategyNone, "none"},
	}

	for _, tc := range cases {
		var got string
		msg, panicked := catchPanic(func() {
			got = tc.strat.String()
		})
		if panicked {
			t.Errorf("Strategy(%d).String(): panicked (%s); green team must implement String()", tc.strat, msg)
			continue
		}
		if got != tc.want {
			t.Errorf("Strategy(%d).String() = %q, want %q", tc.strat, got, tc.want)
		}
	}
}

// TestStrategyStringRemainingValues completes the matrix from
// TestStrategyStringCanonical by covering compound, plugin_managed,
// and the default-unknown fallback for sentinel values that aren't in
// the closed enum. These are non-zero-overhead but matter for log
// readability when a corrupted manifest surfaces an invalid Strategy.
func TestStrategyStringRemainingValues(t *testing.T) {
	cases := []struct {
		strat auth.Strategy
		want  string
	}{
		{auth.StrategyCompound, "compound"},
		{auth.StrategyPluginManaged, "plugin_managed"},
		{auth.Strategy(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.strat.String(); got != tc.want {
			t.Errorf("Strategy(%d).String()=%q; want %q", tc.strat, got, tc.want)
		}
	}
}
