package pluginenv

import (
	_ "embed"
	"strings"
)

// DenylistRaw is the embedded canonical denylist file (internal/pluginenv/denylist.txt).
//
//go:embed denylist.txt
var DenylistRaw []byte

// PluginEnvDenylist is the parsed, sorted, deduplicated list of denied env var
// patterns. Entries ending in "*" are prefix globs; all others are exact names.
var PluginEnvDenylist = parseDenylist(DenylistRaw)

// exactEntries holds all non-glob denylist entries for O(n) exact lookup.
var exactEntries []string

// prefixEntries holds the prefix portion of glob entries (trailing "*" stripped).
var prefixEntries []string

func init() {
	for _, pattern := range PluginEnvDenylist {
		if strings.HasSuffix(pattern, "*") {
			prefixEntries = append(prefixEntries, strings.TrimSuffix(pattern, "*"))
		} else {
			exactEntries = append(exactEntries, pattern)
		}
	}
}

// parseDenylist splits raw denylist bytes into trimmed, non-blank, non-comment lines.
func parseDenylist(b []byte) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// IsDeniedEnv reports whether key matches any denylist entry (exact or prefix glob).
func IsDeniedEnv(key string) bool {
	for _, prefix := range prefixEntries {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	for _, exact := range exactEntries {
		if key == exact {
			return true
		}
	}
	return false
}

// FilterEnvForPlugin removes denied env vars from an os.Environ()-style slice.
func FilterEnvForPlugin(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if i := strings.IndexByte(e, '='); i >= 0 {
			if IsDeniedEnv(e[:i]) {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}
