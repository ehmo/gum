package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/cli/callargs"
	"github.com/ehmo/gum/internal/config"
	"github.com/ehmo/gum/internal/dispatch"
)

// gum-puum: Google Ads ops take customerId and loginCustomerId from
// GUM_GOOGLE_ADS_* env vars or the profile config when the caller omits them.

const historicalMetricsOp = "googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics"

// isolateAdsDefaults points config at an empty temp dir and clears both env
// vars, so a developer's own shell settings never leak into a test.
func isolateAdsDefaults(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv(envAdsCustomerID, "")
	t.Setenv(envAdsLoginCustomerID, "")
	t.Setenv(envAdsGeoTargets, "")
	t.Setenv(envAdsLanguage, "")
}

func saveAdsConfig(t *testing.T, profile string, values map[string]string) {
	t.Helper()
	if err := config.Save(profile, &config.Config{Values: values}); err != nil {
		t.Fatalf("save config: %v", err)
	}
}

func adsTestOp() *catalog.Op {
	return &catalog.Op{
		OpID:    "googleads.test",
		Service: adsService,
		RequestFields: []catalog.RequestField{
			{Name: "customerId", Location: catalog.RequestFieldPath, Required: true},
			{Name: "loginCustomerId", Location: catalog.RequestFieldArg},
		},
	}
}

func TestAdsDefaultsFromEnv(t *testing.T) {
	isolateAdsDefaults(t)
	t.Setenv(envAdsCustomerID, "123-456-7890")
	t.Setenv(envAdsLoginCustomerID, "111 111 1111")

	got, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTestOp(), map[string]any{})
	if err != nil {
		t.Fatalf("ArgDefaults: %v", err)
	}
	if got["customerId"] != "1234567890" {
		t.Errorf("customerId = %#v; want the env value with dashes stripped", got["customerId"])
	}
	if got["loginCustomerId"] != "1111111111" {
		t.Errorf("loginCustomerId = %#v; want the env value with spaces stripped", got["loginCustomerId"])
	}
}

func TestAdsDefaultsFromConfig(t *testing.T) {
	isolateAdsDefaults(t)
	saveAdsConfig(t, "team-ads", map[string]string{
		cfgAdsCustomerID:      "1234567890",
		cfgAdsLoginCustomerID: "111-111-1111",
	})

	got, err := adsArgDefaulter{profile: "team-ads"}.ArgDefaults(adsTestOp(), map[string]any{})
	if err != nil {
		t.Fatalf("ArgDefaults: %v", err)
	}
	if got["customerId"] != "1234567890" || got["loginCustomerId"] != "1111111111" {
		t.Errorf("defaults = %#v; want both ids from the team-ads config", got)
	}

	other, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTestOp(), map[string]any{})
	if err != nil {
		t.Fatalf("ArgDefaults (default profile): %v", err)
	}
	if len(other) != 0 {
		t.Errorf("default profile defaults = %#v; config must stay per profile", other)
	}
}

func TestAdsDefaultsEnvBeatsConfig(t *testing.T) {
	isolateAdsDefaults(t)
	saveAdsConfig(t, "default", map[string]string{cfgAdsCustomerID: "1234567890"})
	t.Setenv(envAdsCustomerID, "9876543210")

	got, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTestOp(), map[string]any{})
	if err != nil {
		t.Fatalf("ArgDefaults: %v", err)
	}
	if got["customerId"] != "9876543210" {
		t.Errorf("customerId = %#v; the env var must rank above the profile config", got["customerId"])
	}
}

// TestAdsDefaultsSkipsCallerArgs also covers a malformed env value for an arg
// the caller supplied: the default is unused, so it must not fail the call.
func TestAdsDefaultsSkipsCallerArgs(t *testing.T) {
	isolateAdsDefaults(t)
	t.Setenv(envAdsCustomerID, "not-an-id")
	t.Setenv(envAdsLoginCustomerID, "1111111111")

	got, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTestOp(), map[string]any{
		"customerId":      "9876543210",
		"loginCustomerId": "",
	})
	if err != nil {
		t.Fatalf("ArgDefaults: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("defaults = %#v; caller-supplied args need no default", got)
	}
}

func TestAdsDefaultsRejectsMalformed(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		cfg     string
		wantSrc string
	}{
		{name: "env letters", env: "abc", wantSrc: envAdsCustomerID},
		{name: "env too short", env: "12345", wantSrc: envAdsCustomerID},
		{name: "config too long", cfg: "12345678901", wantSrc: cfgAdsCustomerID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateAdsDefaults(t)
			if tc.cfg != "" {
				saveAdsConfig(t, "default", map[string]string{cfgAdsCustomerID: tc.cfg})
			}
			t.Setenv(envAdsCustomerID, tc.env)

			_, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTestOp(), map[string]any{})
			if err == nil {
				t.Fatal("ArgDefaults succeeded; a malformed default must fail")
			}
			if !strings.Contains(err.Error(), tc.wantSrc) {
				t.Errorf("error %q does not name its source %s", err, tc.wantSrc)
			}
		})
	}
}

func TestAdsDefaultsIgnoresOtherOps(t *testing.T) {
	isolateAdsDefaults(t)
	t.Setenv(envAdsCustomerID, "1234567890")

	admin := &catalog.Op{
		OpID:          "adminreports.activities.list",
		Service:       "adminreports",
		RequestFields: []catalog.RequestField{{Name: "customerId", Location: catalog.RequestFieldQuery}},
	}
	got, err := adsArgDefaulter{profile: "default"}.ArgDefaults(admin, map[string]any{})
	if err != nil || len(got) != 0 {
		t.Errorf("ArgDefaults(admin op) = %#v, %v; want no defaults outside Google Ads", got, err)
	}

	bare := &catalog.Op{OpID: "googleads.bare", Service: adsService}
	got, err = adsArgDefaulter{profile: "default"}.ArgDefaults(bare, map[string]any{})
	if err != nil || len(got) != 0 {
		t.Errorf("ArgDefaults(op without customerId) = %#v, %v; want no defaults for undeclared fields", got, err)
	}
}

func TestAdsMissingArgHint(t *testing.T) {
	hint := adsArgDefaulter{profile: "default"}.MissingArgHint(adsTestOp(), []string{"customerId"})
	for _, want := range []string{"gum config set " + cfgAdsCustomerID + "=", envAdsCustomerID} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint %q; want it to contain %q", hint, want)
		}
	}
	if strings.Contains(hint, "--profile") {
		t.Errorf("hint %q names --profile for the default profile", hint)
	}

	if unset := (adsArgDefaulter{}).MissingArgHint(adsTestOp(), []string{"customerId"}); unset != hint {
		t.Errorf("hint with no profile = %q; want the default-profile hint %q", unset, hint)
	}

	named := adsArgDefaulter{profile: "team-ads"}.MissingArgHint(adsTestOp(), []string{"customerId"})
	if !strings.Contains(named, "--profile team-ads") {
		t.Errorf("hint %q; want --profile team-ads for a named profile", named)
	}

	admin := &catalog.Op{OpID: "admin.x", Service: "admin"}
	if got := (adsArgDefaulter{profile: "default"}).MissingArgHint(admin, []string{"customerId"}); got != "" {
		t.Errorf("hint for a non-Ads op = %q; want none", got)
	}
	if got := (adsArgDefaulter{profile: "default"}).MissingArgHint(adsTestOp(), []string{"keywords"}); got != "" {
		t.Errorf("hint for an unrelated missing arg = %q; want none", got)
	}
}

// TestAdsDefaultsSkipWizardPrompt keeps the TTY wizard from asking for an id
// the configured default already supplies.
func TestAdsDefaultsSkipWizardPrompt(t *testing.T) {
	isolateAdsDefaults(t)
	fields := lookupRequestFields(historicalMetricsOp)

	all, err := unconfiguredFields(historicalMetricsOp, "default", map[string]any{}, fields)
	if err != nil {
		t.Fatalf("unconfiguredFields: %v", err)
	}
	if !hasField(all, "customerId") {
		t.Fatal("customerId dropped with no default configured; the wizard must still prompt for it")
	}

	t.Setenv(envAdsCustomerID, "1234567890")
	got, err := unconfiguredFields(historicalMetricsOp, "default", map[string]any{}, fields)
	if err != nil {
		t.Fatalf("unconfiguredFields: %v", err)
	}
	if hasField(got, "customerId") {
		t.Error("customerId still prompted although a default is configured")
	}
	if len(got) != len(fields)-1 {
		t.Errorf("unconfiguredFields kept %d of %d fields; want only customerId dropped", len(got), len(fields))
	}

	t.Setenv(envAdsCustomerID, "bad")
	_, err = unconfiguredFields(historicalMetricsOp, "default", map[string]any{}, fields)
	var cerr *callargs.Error
	if !errors.As(err, &cerr) || cerr.Code != "CLI_ARG_INVALID" || !strings.Contains(err.Error(), envAdsCustomerID) {
		t.Errorf("unconfiguredFields error = %v; want a CLI arg error naming %s", err, envAdsCustomerID)
	}
}

// TestAdsWizardWithoutCatalog keeps every field when the catalog is missing:
// with no op record there is no way to tell which fields take a default.
func TestAdsWizardWithoutCatalog(t *testing.T) {
	isolateAdsDefaults(t)
	t.Setenv(envAdsCustomerID, "1234567890")
	fields := lookupRequestFields(historicalMetricsOp)

	setCatalogBlob(t, nil)

	if op := lookupCatalogOp(historicalMetricsOp); op != nil {
		t.Fatalf("lookupCatalogOp with no catalog = %s; want nil", op.OpID)
	}
	got, err := unconfiguredFields(historicalMetricsOp, "default", map[string]any{}, fields)
	if err != nil {
		t.Fatalf("unconfiguredFields: %v", err)
	}
	if len(got) != len(fields) {
		t.Errorf("unconfiguredFields kept %d of %d fields; want all of them", len(got), len(fields))
	}
}

func hasField(fields []catalog.RequestField, name string) bool {
	for _, f := range fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// TestAdsDefaultsThroughDispatcher runs the production dispatcher on the
// embedded catalog. With loadProfileScopes=false the scope gate stops the call
// right after arg validation, so no credentials or network are touched.
func TestAdsDefaultsThroughDispatcher(t *testing.T) {
	isolateAdsDefaults(t)
	dispatchOp := func() *dispatch.StructuredError {
		disp, closer := newDefaultDispatcherWithCloserAndScopeLoading("default", false, false, nil)
		defer func() { _ = closer() }()
		_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
			OpID:   historicalMetricsOp,
			Args:   map[string]any{"keywords": []any{"gum"}},
			Caller: dispatch.CallerCLI,
		})
		var serr *dispatch.StructuredError
		if !errors.As(err, &serr) {
			t.Fatalf("Dispatch error = %v; want a StructuredError", err)
		}
		return serr
	}

	serr := dispatchOp()
	if serr.ErrCode != dispatch.ErrCodeInvalidArgs {
		t.Fatalf("no default: ErrCode = %s; want %s", serr.ErrCode, dispatch.ErrCodeInvalidArgs)
	}
	if hint, _ := serr.Detail["hint"].(string); !strings.Contains(hint, cfgAdsCustomerID) {
		t.Errorf("no default: hint = %q; want it to name %s", hint, cfgAdsCustomerID)
	}

	saveAdsConfig(t, "default", map[string]string{cfgAdsCustomerID: "1234567890"})
	serr = dispatchOp()
	if serr.ErrCode == dispatch.ErrCodeInvalidArgs {
		t.Fatalf("configured default: still INVALID_ARGS (%s); the default was not applied", serr.Message)
	}
	if serr.ErrCode != dispatch.ErrCodeScopeMissing {
		t.Errorf("configured default: ErrCode = %s; want %s from the gate after arg validation", serr.ErrCode, dispatch.ErrCodeScopeMissing)
	}
}
