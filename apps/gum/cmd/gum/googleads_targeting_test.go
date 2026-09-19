package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// gum-0nn2: the three keywordPlanIdeas ops return worldwide, all-language data
// when geoTargetConstants and language are omitted, and nothing in the result
// says so. These tests cover the profile defaults that hold the pair.

// adsTargetingOp declares the two targeting fields the account-only adsTestOp
// leaves out.
func adsTargetingOp() *catalog.Op {
	op := adsTestOp()
	op.RequestFields = append(op.RequestFields,
		catalog.RequestField{Name: "geoTargetConstants", Location: catalog.RequestFieldArg},
		catalog.RequestField{Name: "language", Location: catalog.RequestFieldArg},
	)
	return op
}

func TestAdsTargetingFromConfig(t *testing.T) {
	isolateAdsDefaults(t)
	saveAdsConfig(t, "default", map[string]string{
		cfgAdsGeoTargets: "2840",
		cfgAdsLanguage:   "1000",
	})

	got, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTargetingOp(), map[string]any{})
	if err != nil {
		t.Fatalf("ArgDefaults: %v", err)
	}
	if want := []string{"2840"}; !reflect.DeepEqual(got["geoTargetConstants"], want) {
		t.Errorf("geoTargetConstants = %#v; want %#v", got["geoTargetConstants"], want)
	}
	if got["language"] != "1000" {
		t.Errorf("language = %#v; want \"1000\"", got["language"])
	}
}

func TestAdsTargetingMultipleGeoTargets(t *testing.T) {
	isolateAdsDefaults(t)
	saveAdsConfig(t, "default", map[string]string{
		cfgAdsGeoTargets: "2840, geoTargetConstants/2124",
	})

	got, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTargetingOp(), map[string]any{})
	if err != nil {
		t.Fatalf("ArgDefaults: %v", err)
	}
	want := []string{"2840", "geoTargetConstants/2124"}
	if !reflect.DeepEqual(got["geoTargetConstants"], want) {
		t.Errorf("geoTargetConstants = %#v; want %#v", got["geoTargetConstants"], want)
	}
}

func TestAdsTargetingEnvBeatsConfig(t *testing.T) {
	isolateAdsDefaults(t)
	saveAdsConfig(t, "default", map[string]string{
		cfgAdsGeoTargets: "2840",
		cfgAdsLanguage:   "1000",
	})
	t.Setenv(envAdsGeoTargets, "2124")
	t.Setenv(envAdsLanguage, "1001")

	got, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTargetingOp(), map[string]any{})
	if err != nil {
		t.Fatalf("ArgDefaults: %v", err)
	}
	if want := []string{"2124"}; !reflect.DeepEqual(got["geoTargetConstants"], want) {
		t.Errorf("geoTargetConstants = %#v; want the env value %#v", got["geoTargetConstants"], want)
	}
	if got["language"] != "1001" {
		t.Errorf("language = %#v; want the env value \"1001\"", got["language"])
	}
}

// TestAdsTargetingSkipsCallerArgs is acceptance criterion (b): an explicit
// argument wins, and an explicit empty list buys back the worldwide result.
func TestAdsTargetingSkipsCallerArgs(t *testing.T) {
	isolateAdsDefaults(t)
	t.Setenv(envAdsGeoTargets, "2840")
	t.Setenv(envAdsLanguage, "1000")

	args := map[string]any{
		"geoTargetConstants": []any{"2124"},
		"language":           "1001",
	}
	got, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTargetingOp(), args)
	if err != nil {
		t.Fatalf("ArgDefaults: %v", err)
	}
	if _, ok := got["geoTargetConstants"]; ok {
		t.Errorf("geoTargetConstants defaulted over a caller argument: %#v", got)
	}
	if _, ok := got["language"]; ok {
		t.Errorf("language defaulted over a caller argument: %#v", got)
	}

	empty := map[string]any{"geoTargetConstants": []any{}}
	got, err = adsArgDefaulter{profile: "default"}.ArgDefaults(adsTargetingOp(), empty)
	if err != nil {
		t.Fatalf("ArgDefaults: %v", err)
	}
	if _, ok := got["geoTargetConstants"]; ok {
		t.Errorf("an explicit empty list must stay empty, got %#v", got)
	}
}

// TestAdsTargetingUnsetAddsNothing is acceptance criterion (c): with no default
// and no argument, the request is what 1.4.0 sent.
func TestAdsTargetingUnsetAddsNothing(t *testing.T) {
	isolateAdsDefaults(t)

	got, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTargetingOp(), map[string]any{})
	if err != nil {
		t.Fatalf("ArgDefaults: %v", err)
	}
	for _, key := range []string{"geoTargetConstants", "language"} {
		if _, ok := got[key]; ok {
			t.Errorf("%s was defaulted with nothing configured: %#v", key, got)
		}
	}
}

// TestAdsTargetingIgnoresOtherOps: googleads.googleAds.search declares neither
// field, so a configured default must not reach it.
func TestAdsTargetingIgnoresOtherOps(t *testing.T) {
	isolateAdsDefaults(t)
	t.Setenv(envAdsGeoTargets, "2840")
	t.Setenv(envAdsLanguage, "1000")

	got, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTestOp(), map[string]any{})
	if err != nil {
		t.Fatalf("ArgDefaults: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("defaults = %#v; want none for an op without the fields", got)
	}
}

func TestAdsTargetingRejectsMalformed(t *testing.T) {
	cases := []struct {
		name    string
		geo     string
		lang    string
		wantSrc string
		wantArg string
	}{
		{name: "geo letters", geo: "US", wantSrc: envAdsGeoTargets, wantArg: "geoTargetConstants"},
		{name: "geo wrong resource", geo: "languageConstants/1000", wantSrc: envAdsGeoTargets, wantArg: "geoTargetConstants"},
		{name: "geo empty list", geo: ",", wantSrc: envAdsGeoTargets, wantArg: "geoTargetConstants"},
		{name: "language letters", lang: "en", wantSrc: envAdsLanguage, wantArg: "language"},
		{name: "language list", lang: "1000,1001", wantSrc: envAdsLanguage, wantArg: "language"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateAdsDefaults(t)
			t.Setenv(envAdsGeoTargets, tc.geo)
			t.Setenv(envAdsLanguage, tc.lang)

			_, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTargetingOp(), map[string]any{})
			if err == nil {
				t.Fatal("ArgDefaults succeeded; a malformed default must fail")
			}
			if !strings.Contains(err.Error(), tc.wantSrc) {
				t.Errorf("error %q does not name its source %s", err, tc.wantSrc)
			}
			if !strings.Contains(err.Error(), tc.wantArg) {
				t.Errorf("error %q does not name the argument %s", err, tc.wantArg)
			}
		})
	}
}

// TestAdsTargetingMalformedNamesConfigKey: a bad value in the profile config
// must name the config key, not just the env var.
func TestAdsTargetingMalformedNamesConfigKey(t *testing.T) {
	isolateAdsDefaults(t)
	saveAdsConfig(t, "default", map[string]string{cfgAdsGeoTargets: "worldwide"})

	_, err := adsArgDefaulter{profile: "default"}.ArgDefaults(adsTargetingOp(), map[string]any{})
	if err == nil {
		t.Fatal("ArgDefaults succeeded; a malformed config default must fail")
	}
	if !strings.Contains(err.Error(), cfgAdsGeoTargets) {
		t.Errorf("error %q does not name %s", err, cfgAdsGeoTargets)
	}
}
