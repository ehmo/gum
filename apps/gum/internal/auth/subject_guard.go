package auth

// subject_guard.go enforces the spec §7 credential-resolution rule at login
// time: a profile's active credential is used "only if its
// auth_subject_fingerprint matches the selected profile's expected subject
// when one is recorded". Both interactive flows put an account chooser in
// front of the operator, so the account that comes back is not necessarily
// the one the profile is already bound to, and adopting a new one silently
// orphans every cache entry, tee artifact and gain-ledger row keyed on the
// old fingerprint (§10.0.1). The dispatch kernel raises the same code when a
// credential resolved outside a login does not match (bead gum-q0kd).

import "fmt"

// SubjectMismatchCode is the stable error code both the login guard and the
// dispatch-path guard carry.
const SubjectMismatchCode = "AUTH_SUBJECT_MISMATCH"

// checkSubject reports whether got may replace want for strategy. An empty
// want means the profile has recorded no expectation, so any account is
// adopted; allowChange is the operator's deliberate `--switch-account`.
func checkSubject(strategy, want, got string, allowChange bool) error {
	if want == "" || allowChange || got == want {
		return nil
	}
	return &AuthError{
		Code:         SubjectMismatchCode,
		Strategy:     strategy,
		SetupCommand: "gum login --switch-account",
		HumanRemediation: fmt.Sprintf(
			"consent returned subject %s but this profile is bound to %s; nothing was stored. Re-run with --switch-account to rebind the profile to the new account, or run `gum logout` first.",
			got, want),
		UserMessage: "That is a different Google account than this profile uses. Re-run with --switch-account to switch accounts.",
	}
}
