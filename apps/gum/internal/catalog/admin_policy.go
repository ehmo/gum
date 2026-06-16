package catalog

import (
	"fmt"
	"strings"
)

// IsAdminFixtureResource reports whether a user/group/member key is clearly
// owned by a GUM fixture. It accepts deterministic fixture names and emails
// whose local part starts with the fixture marker; it deliberately does not
// trust fixture-looking domains such as user@gum-fixture-example.com.
func IsAdminFixtureResource(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, AdminFixtureMarkerPrefix) {
		return true
	}
	if at := strings.IndexByte(value, '@'); at > 0 {
		return strings.HasPrefix(value[:at], AdminFixtureMarkerPrefix)
	}
	if slash := strings.LastIndexByte(value, '/'); slash >= 0 && slash+1 < len(value) {
		return strings.HasPrefix(value[slash+1:], AdminFixtureMarkerPrefix)
	}
	return false
}

// ValidateAdminFixtureOwnership verifies that every policy-named resource key
// is present in args and carries the fixture marker. It is intentionally small:
// it is a pre-dispatch guard for future Admin write promotions, not a general
// JSON schema validator.
func ValidateAdminFixtureOwnership(args map[string]any, policy *AdminPolicy) error {
	if policy == nil || !policy.FixtureOwnershipRequired {
		return nil
	}
	for _, key := range policy.FixtureResourceKeys {
		raw, ok := adminFixtureArg(args, key)
		if !ok {
			return fmt.Errorf("%s missing: %w", key, ErrAdminFixtureOwnership)
		}
		value, ok := raw.(string)
		if !ok || !IsAdminFixtureResource(value) {
			return fmt.Errorf("%s not fixture-owned: %w", key, ErrAdminFixtureOwnership)
		}
	}
	return nil
}

func adminFixtureArg(args map[string]any, key string) (any, bool) {
	raw, ok := args[key]
	if ok {
		return raw, true
	}
	body, ok := args["body"]
	if !ok {
		return nil, false
	}
	switch b := body.(type) {
	case map[string]any:
		raw, ok = b[key]
		return raw, ok
	case map[string]string:
		raw, ok = b[key]
		return raw, ok
	default:
		return nil, false
	}
}
