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

type adsAccountDefault struct {
	arg    string
	env    string
	cfgKey string
}

var adsAccountDefaults = []adsAccountDefault{
	{arg: "customerId", env: envAdsCustomerID, cfgKey: cfgAdsCustomerID},
	{arg: "loginCustomerId", env: envAdsLoginCustomerID, cfgKey: cfgAdsLoginCustomerID},
}

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
	for _, def := range adsAccountDefaults {
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

		id, ok := normalizeAdsAccountID(raw)
		if !ok {
			return nil, fmt.Errorf("%s is %q, which is not a %d-digit Google Ads account id (dashes allowed); it was the default for %s",
				source, raw, adsAccountIDDigits, def.arg)
		}
		out[def.arg] = id
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
