// resolver.go — §9.2 three-level profile resolution.
package profile

import (
	"errors"
	"os"
	"path/filepath"
)

// ResolutionSource identifies which resolution layer supplied the profile.
type ResolutionSource string

const (
	SourceProjectLocal    ResolutionSource = "project-local"
	SourceUserGlobal      ResolutionSource = "user-global"
	SourceCatalogEmbedded ResolutionSource = "catalog-embedded"
)

// ErrProfileNotFound is returned when no resolution layer supplies the
// requested profile name.
var ErrProfileNotFound = errors.New("profile: not found in any resolution layer")

// CatalogLookup is the catalog-embedded resolution callback.
type CatalogLookup func(name string) (*Profile, bool)

// ResolveProfile resolves name through the §9.2 three-level hierarchy.
//
// rootPath is the absolute path of the active project root (per MCP roots
// binding). Pass "" to disable project-local lookup; the resolver does NOT
// walk $PWD when rootPath is empty.
//
// catalogLookup may be nil; when nil, the catalog-embedded layer never matches.
func ResolveProfile(rootPath, name string, catalogLookup CatalogLookup) (*Profile, ResolutionSource, error) {
	// 1. Project-local — only when an explicit root was provided.
	if rootPath != "" {
		if p, found, err := lookupProjectLocal(rootPath, name); err != nil {
			return nil, "", err
		} else if found {
			return p, SourceProjectLocal, nil
		}
	}

	// 2. User-global — XDG_CONFIG_HOME → $HOME/.config
	if p, found, err := lookupUserGlobal(name); err != nil {
		return nil, "", err
	} else if found {
		return p, SourceUserGlobal, nil
	}

	// 3. Catalog-embedded — caller-supplied callback.
	if catalogLookup != nil {
		if p, ok := catalogLookup(name); ok {
			return p, SourceCatalogEmbedded, nil
		}
	}

	return nil, "", ErrProfileNotFound
}

// lookupProjectLocal walks upward from rootPath until it finds a directory
// containing .gum/profiles/<name>.toml, or until the filesystem root is reached.
func lookupProjectLocal(rootPath, name string) (*Profile, bool, error) {
	dir := filepath.Clean(rootPath)
	for {
		candidate := filepath.Join(dir, ".gum", "profiles", name+".toml")
		data, err := os.ReadFile(candidate)
		if err == nil {
			p, err := Parse(string(data))
			if err != nil {
				return nil, true, err
			}
			return p, true, nil
		} else if !os.IsNotExist(err) {
			return nil, false, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root.
			return nil, false, nil
		}
		dir = parent
	}
}

// lookupUserGlobal resolves the user-global profile path using XDG_CONFIG_HOME
// when set, falling back to $HOME/.config.
func lookupUserGlobal(name string) (*Profile, bool, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			// No home directory available — fall through silently.
			return nil, false, nil
		}
		base = filepath.Join(home, ".config")
	}
	candidate := filepath.Join(base, "gum", "profiles", name+".toml")
	data, err := os.ReadFile(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	p, err := Parse(string(data))
	if err != nil {
		return nil, true, err
	}
	return p, true, nil
}
