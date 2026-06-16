package profile

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrProfileStripNullsUnsafe is the sentinel for spec §7 error code
// PROFILE_STRIP_NULLS_UNSAFE per docs/catalog-abi.md §55 and
// docs/expression-profile-dsl.md validation rule §2.
var ErrProfileStripNullsUnsafe = errors.New("PROFILE_STRIP_NULLS_UNSAFE")

// ValidateStripNullsSafety enforces the strip_nulls safety contract:
//
//   - If p.StripNulls is false, returns nil (nothing to enforce).
//   - If safe contains "*", returns nil (curator-reviewed full elision).
//   - If safe is nil/empty, returns an error wrapping ErrProfileStripNullsUnsafe.
//   - If p.KeepFields is empty (the elision surface is unbounded relative
//     to a non-"*" safe list), returns an error.
//   - If every entry in p.KeepFields is contained in safe, returns nil.
//   - Otherwise returns an error naming the offending field(s).
//
// Every returned error message contains the literal "PROFILE_STRIP_NULLS_UNSAFE"
// (the error wraps ErrProfileStripNullsUnsafe via %w).
func ValidateStripNullsSafety(p *Profile, safe []string) error {
	if p == nil || !p.StripNulls {
		return nil
	}
	if containsStar(safe) {
		return nil
	}
	if len(safe) == 0 {
		return fmt.Errorf("%w: variant declares no null_elision_safe_fields; strip_nulls=true is not allowed", ErrProfileStripNullsUnsafe)
	}
	if len(p.KeepFields) == 0 {
		return fmt.Errorf("%w: profile has strip_nulls=true with unbounded elision surface (no keep_fields) and variant safe set is not \"*\"", ErrProfileStripNullsUnsafe)
	}

	safeSet := make(map[string]struct{}, len(safe))
	for _, s := range safe {
		safeSet[s] = struct{}{}
	}
	var bad []string
	for _, k := range p.KeepFields {
		if _, ok := safeSet[k]; !ok {
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("%w: keep_fields not covered by variant null_elision_safe_fields: %s", ErrProfileStripNullsUnsafe, strings.Join(bad, ", "))
}

func containsStar(safe []string) bool {
	for _, s := range safe {
		if s == "*" {
			return true
		}
	}
	return false
}
