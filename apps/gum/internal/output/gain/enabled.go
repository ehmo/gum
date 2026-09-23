package gain

import (
	"os"
	"strconv"

	"github.com/ehmo/gum/internal/config"
	"github.com/ehmo/gum/internal/profile"
)

// EnabledKey is the config key spec §12.3 names as the gain opt-out:
// `gum config set gain.enabled=false` stops all usage logging for one profile.
const EnabledKey = "gain.enabled"

// DisabledEnv short-circuits the config lookup. It exists for one-shot runs
// and for tests that must not touch a user config file.
const DisabledEnv = "GUM_GAIN_DISABLED"

// Enabled reports whether gain accounting runs for this profile.
//
// Both the writer (the dispatch ledger) and the reader (gum gain, gum.gain)
// call this, so one opt-out covers recording and reporting. Spec §12.3 makes
// the config key the documented switch; the env var stays as an override.
//
// A missing key, an unreadable config, or a value that is not a bool leaves
// accounting on. Silently disabling the ledger on a typo would make gum report
// zero savings with no way to tell that from a genuinely idle profile.
func Enabled(name profile.Name) bool {
	if os.Getenv(DisabledEnv) == "1" {
		return false
	}

	cfg, _, err := config.Load(name.String())
	if err != nil || cfg == nil {
		return true
	}

	raw, ok := cfg.Get(EnabledKey)
	if !ok {
		return true
	}

	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return true
	}

	return enabled
}
