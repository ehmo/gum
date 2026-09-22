package main

// Profile-recorded credential subjects (bead gum-q0kd).
//
// Spec §7 admits a profile's active credential "only if its
// auth_subject_fingerprint matches the selected profile's expected subject
// when one is recorded". This file is where the expectation is recorded and
// read back: `gum login` stores the fingerprint the consent returned, and the
// dispatcher is handed the whole per-strategy map at construction so step 5
// can refuse anything else.

import (
	"strings"

	"github.com/ehmo/gum/internal/config"
)

// expectedSubjectPrefix roots the per-strategy config keys, e.g.
// auth.expected_subject.byo_oauth. The strategy is part of the key because
// the fingerprint namespace is per strategy: the same Google account hashes
// differently under byo_oauth than under gum_oauth.
const expectedSubjectPrefix = "auth.expected_subject."

// byoStrategy is the auth_strategy name both login paths authorize under, and
// so the key suffix their expectation is recorded at.
const byoStrategy = "byo_oauth"

// expectedAuthSubjects reads the profile's recorded subjects as the
// auth_strategy -> fingerprint map the dispatcher expects. An unreadable or
// absent config yields nil, which disables the check rather than locking the
// operator out.
func expectedAuthSubjects(profile string) map[string]string {
	cfg, _, err := config.Load(profile)
	if err != nil || cfg == nil {
		return nil
	}
	out := map[string]string{}
	for _, key := range cfg.Keys() {
		strategy, found := strings.CutPrefix(key, expectedSubjectPrefix)
		if !found || strategy == "" {
			continue
		}
		if value, ok := cfg.Get(key); ok && value != "" {
			out[strategy] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// expectedAuthSubject returns the fingerprint recorded for one strategy, or ""
// when the profile has none.
func expectedAuthSubject(profile, strategy string) string {
	return expectedAuthSubjects(profile)[strategy]
}

// recordExpectedSubject binds the profile to fingerprint for strategy. It runs
// after a successful login, which is the one moment the operator has proved
// which account the profile should use. A no-op for an empty fingerprint: a
// legacy grant with no OIDC subject still derives one from the refresh token,
// so the empty case only happens when the strategy has no subject at all.
func recordExpectedSubject(profile, strategy, fingerprint string) error {
	if fingerprint == "" || strategy == "" {
		return nil
	}
	cfg, _, err := config.Load(profile)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	if current, ok := cfg.Get(expectedSubjectPrefix + strategy); ok && current == fingerprint {
		return nil
	}
	cfg.Set(expectedSubjectPrefix+strategy, fingerprint)
	return config.Save(profile, cfg)
}
