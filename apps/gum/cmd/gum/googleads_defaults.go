package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/config"
	"github.com/ehmo/gum/internal/dispatch"
	profilepkg "github.com/ehmo/gum/internal/profile"
)

// Google Ads account defaults (gum-puum). Account ids are not secrets, so they
// live in env vars and the profile config rather than the keychain. Precedence
// follows spec §12.2: explicit arg > GUM_* env > profile config.
const (
	adsService            = "googleads"
	envAdsCustomerID      = "GUM_GOOGLE_ADS_CUSTOMER_ID"
	envAdsLoginCustomerID = "GUM_GOOGLE_ADS_LOGIN_CUSTOMER_ID"
	cfgAdsCustomerID      = "googleads.customer_id"
	cfgAdsLoginCustomerID = "googleads.login_customer_id"
	adsAccountIDDigits    = 10
)

// Keyword Planner targeting defaults (gum-0nn2). An omitted geo target returns
// worldwide volume and an omitted language returns all languages, and neither
// result is marked, so a caller who wants one country has to repeat the pair on
// every call. These defaults hold it once per profile.
const (
	envAdsGeoTargets  = "GUM_GOOGLE_ADS_GEO_TARGET_CONSTANTS"
	envAdsLanguage    = "GUM_GOOGLE_ADS_LANGUAGE"
	cfgAdsGeoTargets  = "googleads.geo_target_constants"
	cfgAdsLanguage    = "googleads.language"
	geoTargetResource = "geoTargetConstants"
	languageResource  = "languageConstants"
)

type adsDefault struct {
	arg    string
	env    string
	cfgKey string
	// normalize converts the stored text into the argument value, and reports
	// false when the text cannot be one. expects names the accepted form in
	// that rejection.
	normalize func(raw string) (any, bool)
	expects   string
}

// adsAccountDefaults also drives the missing-argument hint, so it stays
// separate from the targeting table.
var adsAccountDefaults = []adsDefault{
	{arg: "customerId", env: envAdsCustomerID, cfgKey: cfgAdsCustomerID,
		normalize: normalizeAdsAccount, expects: adsAccountForm},
	{arg: "loginCustomerId", env: envAdsLoginCustomerID, cfgKey: cfgAdsLoginCustomerID,
		normalize: normalizeAdsAccount, expects: adsAccountForm},
}

var adsTargetingDefaults = []adsDefault{
	{arg: "geoTargetConstants", env: envAdsGeoTargets, cfgKey: cfgAdsGeoTargets,
		normalize: normalizeGeoTargets,
		expects:   "a comma-separated list of geo target ids or resource names (2840 or geoTargetConstants/2840)"},
	{arg: "language", env: envAdsLanguage, cfgKey: cfgAdsLanguage,
		normalize: normalizeLanguage,
		expects:   "a language id or resource name (1000 or languageConstants/1000)"},
}

var adsDefaults = append(append([]adsDefault{}, adsAccountDefaults...), adsTargetingDefaults...)

const adsAccountForm = "a 10-digit Google Ads account id (dashes allowed)"

// adsArgDefaulter implements dispatch.ArgDefaulter for Google Ads ops. It
// reads env and config on every call, so `gum config set` takes effect in a
// running MCP server without a restart.
type adsArgDefaulter struct {
	profile string
}

// newArgDefaulter returns the configured-defaults source shared by the
// dispatcher and the CLI wizard.
func newArgDefaulter(profile string) dispatch.ArgDefaulter {
	return adsArgDefaulter{profile: profile}
}

func (d adsArgDefaulter) ArgDefaults(op *catalog.Op, args map[string]any) (map[string]any, error) {
	if op == nil || op.Service != adsService {
		return nil, nil
	}

	var cfg *config.Config
	cfgLoaded := false
	out := map[string]any{}
	for _, def := range adsDefaults {
		if !opDeclaresField(op, def.arg) {
			continue
		}
		if _, supplied := args[def.arg]; supplied {
			continue
		}

		raw := strings.TrimSpace(os.Getenv(def.env))
		source := def.env
		if raw == "" {
			if !cfgLoaded {
				// A load error leaves cfg nil and Get reports nothing set;
				// `gum config` commands surface the error itself.
				cfg, _, _ = config.Load(d.profile)
				cfgLoaded = true
			}
			v, _ := cfg.Get(def.cfgKey)
			raw = strings.TrimSpace(v)
			source = fmt.Sprintf("config key %s (profile %s)", def.cfgKey, d.profileName())
		}
		if raw == "" {
			continue
		}

		value, ok := def.normalize(raw)
		if !ok {
			return nil, fmt.Errorf("%s is %q, which is not %s; it was the default for %s",
				source, raw, def.expects, def.arg)
		}
		out[def.arg] = value
	}
	return out, nil
}

func (d adsArgDefaulter) MissingArgHint(op *catalog.Op, missing []string) string {
	if op == nil || op.Service != adsService {
		return ""
	}

	profileFlag := ""
	if d.profileName() != profilepkg.DefaultName.String() {
		profileFlag = " --profile " + d.profileName()
	}
	var hints []string
	for _, name := range missing {
		for _, def := range adsAccountDefaults {
			if def.arg != name {
				continue
			}
			hints = append(hints, fmt.Sprintf("Pass %s, or set a default: `gum config set%s %s=<%d-digit id>` or export %s.",
				def.arg, profileFlag, def.cfgKey, adsAccountIDDigits, def.env))
		}
	}
	return strings.Join(hints, " ")
}

func (d adsArgDefaulter) profileName() string {
	if d.profile == "" {
		return profilepkg.DefaultName.String()
	}
	return d.profile
}

func opDeclaresField(op *catalog.Op, name string) bool {
	for _, f := range op.RequestFields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// normalizeAdsAccount adapts the account id check to the adsDefault table.
func normalizeAdsAccount(raw string) (any, bool) {
	id, ok := normalizeAdsAccountID(raw)
	if !ok {
		return nil, false
	}
	return id, true
}

// normalizeGeoTargets accepts one or more comma-separated geo targets and
// returns them unchanged; the adapter adds the resource prefix to a bare id.
func normalizeGeoTargets(raw string) (any, bool) {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !validAdsConstant(part, geoTargetResource) {
			return nil, false
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// normalizeLanguage accepts one language id or resource name. Google Ads takes
// a single language per request, so a list is a mistake worth naming.
func normalizeLanguage(raw string) (any, bool) {
	v := strings.TrimSpace(raw)
	if !validAdsConstant(v, languageResource) {
		return nil, false
	}
	return v, true
}

// validAdsConstant reports whether raw is a bare numeric id or that same id
// behind its own resource prefix. A language resource name therefore fails a
// geo check, and the reverse.
func validAdsConstant(raw, resource string) bool {
	id := strings.TrimPrefix(strings.TrimSpace(raw), resource+"/")
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// normalizeAdsAccountID strips dashes and spaces and requires exactly 10
// digits, matching the Google Ads adapter's own account id check.
func normalizeAdsAccountID(raw string) (string, bool) {
	var b strings.Builder
	for _, r := range raw {
		if r == '-' || r == ' ' {
			continue
		}
		if r < '0' || r > '9' {
			return "", false
		}
		b.WriteRune(r)
	}
	if b.Len() != adsAccountIDDigits {
		return "", false
	}
	return b.String(), true
}
