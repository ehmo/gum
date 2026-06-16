// Package config implements per-profile gum config file load/save (spec §12.2).
//
// The config file lives at <XDG_CONFIG_HOME>/gum/<profile>/config.toml (or
// $HOME/.config/gum/<profile>/config.toml if XDG_CONFIG_HOME is unset), with
// mode 600. Keys are flat dotted strings; values are stored verbatim as
// strings. Unknown keys are preserved with a structured warning rather than
// a fatal error (spec §12.2 line 2474).
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	profilepkg "github.com/ehmo/gum/internal/profile"
)

const CurrentSchemaVersion = 1

// Known top-level prefixes. Keys NOT rooted under one of these emit a
// Warning on Load but are preserved.
var knownPrefixes = []string{
	"output.",
	"audit.",
	"cache.",
	"gain.",
	"code.",
	"validation.",
	"meta_tools.",
	"notify.",
}

// Warning is a structured non-fatal diagnostic emitted during Load.
type Warning struct {
	Event       string
	Key         string
	Profile     string
	UserMessage string
}

// ErrSchemaUnsupported is returned when the config file's schema version
// exceeds CurrentSchemaVersion (CONFIG_SCHEMA_UNSUPPORTED).
type ErrSchemaUnsupported struct {
	Profile string
	Version int
}

func (e *ErrSchemaUnsupported) Error() string {
	return fmt.Sprintf("CONFIG_SCHEMA_UNSUPPORTED: profile '%s' config_schema_version=%d is not supported by this gum binary", e.Profile, e.Version)
}

// Config holds the parsed per-profile configuration.
type Config struct {
	SchemaVersion int
	Values        map[string]string
}

// Get returns the value for key, and whether it was present.
func (c *Config) Get(key string) (string, bool) {
	if c == nil || c.Values == nil {
		return "", false
	}
	v, ok := c.Values[key]
	return v, ok
}

// Set stores key=value in the config.
func (c *Config) Set(key, value string) {
	if c.Values == nil {
		c.Values = map[string]string{}
	}
	c.Values[key] = value
}

// Unset removes key from the config and reports whether it was present.
func (c *Config) Unset(key string) bool {
	if c == nil || c.Values == nil {
		return false
	}
	if _, ok := c.Values[key]; !ok {
		return false
	}
	delete(c.Values, key)
	return true
}

// Keys returns all keys in sorted order.
func (c *Config) Keys() []string {
	out := make([]string, 0, len(c.Values))
	for k := range c.Values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Path returns the on-disk path for profile's config.toml.
// Honors XDG_CONFIG_HOME first; falls back to $HOME/.config.
func Path(profile string) (string, error) {
	name, err := profilepkg.Parse(profile)
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return name.ConfigPath()
}

// Load reads, parses, and validates the per-profile config file.
// A missing file is not an error — it returns an empty Config with SchemaVersion 0.
func Load(profile string) (*Config, []Warning, error) {
	p, err := Path(profile)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{SchemaVersion: 0, Values: map[string]string{}}, nil, nil
		}
		return nil, nil, fmt.Errorf("config: read %s: %w", p, err)
	}
	return parse(profile, string(data))
}

func parse(profile, src string) (*Config, []Warning, error) {
	c := &Config{SchemaVersion: 0, Values: map[string]string{}}
	var warnings []Warning
	sc := bufio.NewScanner(strings.NewReader(src))
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			return nil, nil, fmt.Errorf("config: line %d: expected key = value, got %q", lineNum, line)
		}
		key := strings.TrimSpace(line[:idx])
		rawVal := strings.TrimSpace(line[idx+1:])
		if key == "" {
			return nil, nil, fmt.Errorf("config: line %d: empty key", lineNum)
		}

		// Handle config_schema_version specially — parse as integer.
		if key == "config_schema_version" {
			var v int
			if _, err := fmt.Sscanf(rawVal, "%d", &v); err != nil {
				return nil, nil, fmt.Errorf("config: line %d: config_schema_version must be an integer, got %q", lineNum, rawVal)
			}
			if v > CurrentSchemaVersion {
				return nil, nil, &ErrSchemaUnsupported{Profile: profile, Version: v}
			}
			c.SchemaVersion = v
			continue
		}

		// Strip surrounding quotes from the value if present.
		val, err := unquoteValue(rawVal)
		if err != nil {
			return nil, nil, fmt.Errorf("config: line %d: %w", lineNum, err)
		}

		c.Values[key] = val
		if !isKnownKey(key) {
			warnings = append(warnings, Warning{
				Event:       "unknown_config_key",
				Key:         key,
				Profile:     profile,
				UserMessage: fmt.Sprintf("key '%s' is not recognized by this version of gum; it will be ignored.", key),
			})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("config: scan: %w", err)
	}
	return c, warnings, nil
}

func unquoteValue(raw string) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("empty value")
	}
	if strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"") {
		if len(raw) < 2 {
			return "", fmt.Errorf("unterminated quoted value: %q", raw)
		}
		// Reverse Save's escaping (it writes values double-quoted with interior
		// double-quotes escaped as \"). Without this, a value containing a " is
		// silently mangled on every save/load round trip.
		return strings.ReplaceAll(raw[1:len(raw)-1], `\"`, `"`), nil
	}
	if strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") {
		if len(raw) < 2 {
			return "", fmt.Errorf("unterminated quoted value: %q", raw)
		}
		// TOML literal (single-quoted) string: no escaping.
		return raw[1 : len(raw)-1], nil
	}
	// Unquoted scalar — bool / number / bareword.
	return raw, nil
}

func isKnownKey(key string) bool {
	for _, p := range knownPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// Save atomically writes c back to disk. Migrates SchemaVersion 0 → CurrentSchemaVersion.
func Save(profile string, c *Config) error {
	if c == nil {
		return fmt.Errorf("config: Save: nil config")
	}
	p, err := Path(profile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("config: mkdir parent: %w", err)
	}

	if c.SchemaVersion == 0 {
		c.SchemaVersion = CurrentSchemaVersion
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "config_schema_version = %d\n", c.SchemaVersion)
	for _, k := range c.Keys() {
		// Quote the value. Values containing double-quotes have them escaped.
		v := strings.ReplaceAll(c.Values[k], `"`, `\"`)
		fmt.Fprintf(&buf, "%s = \"%s\"\n", k, v)
	}

	// Atomic write: write to <path>.tmp, then rename. Mode 600.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(buf.String()), 0o600); err != nil {
		return fmt.Errorf("config: write tmp: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("config: chmod tmp: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("config: rename: %w", err)
	}
	return nil
}
