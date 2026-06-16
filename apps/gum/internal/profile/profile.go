// Package profile centralizes GUM profile-name validation and filesystem paths.
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// DefaultName is the implicit profile used when no explicit profile is set.
	DefaultName Name = "default"
	// EnvVar is the environment variable used when --profile is not supplied.
	EnvVar = "GUM_PROFILE"
)

var validNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Name is a validated profile name safe to use as a single path segment.
type Name string

// Parse validates raw and returns a normalized Name. Empty maps to DefaultName.
func Parse(raw string) (Name, error) {
	if raw == "" {
		return DefaultName, nil
	}
	if strings.TrimSpace(raw) != raw {
		return "", invalid(raw)
	}
	if raw == "." || raw == ".." || strings.Contains(raw, "..") {
		return "", invalid(raw)
	}
	if strings.ContainsAny(raw, `/\`) {
		return "", invalid(raw)
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", invalid(raw)
		}
	}
	if !validNameRe.MatchString(raw) {
		return "", invalid(raw)
	}
	return Name(raw), nil
}

// Resolve chooses the active profile using CLI-over-env precedence.
func Resolve(flagValue string, flagChanged bool) (Name, error) {
	if flagChanged {
		return Parse(flagValue)
	}
	if env := os.Getenv(EnvVar); env != "" {
		return Parse(env)
	}
	return Parse(flagValue)
}

func invalid(raw string) error {
	return fmt.Errorf("profile: invalid profile name %q (use letters, numbers, '.', '_' or '-'; path separators, whitespace, control characters, and '..' are not allowed)", raw)
}

func (n Name) String() string {
	if n == "" {
		return string(DefaultName)
	}
	return string(n)
}

// ConfigPath returns <XDG_CONFIG_HOME>/gum/<profile>/config.toml.
func (n Name) ConfigPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("config: resolve home: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "gum", n.String(), "config.toml"), nil
}

// DataDir returns <XDG_DATA_HOME>/gum/<profile>.
func (n Name) DataDir() (string, error) {
	base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "gum", n.String()), nil
}

// CacheDir returns <XDG_CACHE_HOME>/gum/<profile>.
func (n Name) CacheDir() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "gum", n.String()), nil
}

// NotifyPath returns <XDG_CACHE_HOME>/gum/<profile>/notify.json.
func (n Name) NotifyPath() (string, error) {
	dir, err := n.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "notify.json"), nil
}
