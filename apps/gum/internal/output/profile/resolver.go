// resolver.go — §9.2 three-level profile resolution.
package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// ResolveProfile resolves name through the §9.2 three-level hierarchy and
// resolves one level of `inherits` on the result.
//
// rootPath is the absolute path of the active project root (per MCP roots
// binding, or the working directory for CLI calls). Pass "" to disable
// project-local lookup; the resolver does NOT walk $PWD when rootPath is empty.
//
// catalogLookup may be nil; when nil, the catalog-embedded layer never matches.
func ResolveProfile(rootPath, name string, catalogLookup CatalogLookup) (*Profile, ResolutionSource, error) {
	p, file, source, err := resolveRaw(rootPath, name, catalogLookup)
	if err != nil {
		return nil, "", err
	}
	merged, err := resolveInherits(rootPath, p, file, catalogLookup)
	if err != nil {
		return nil, "", err
	}
	return merged, source, nil
}

// resolveRaw walks the three layers and returns the unmerged profile plus the
// file that defined it, so a sibling definition in the same file can satisfy
// `inherits` without a second filesystem walk.
func resolveRaw(rootPath, name string, catalogLookup CatalogLookup) (*Profile, *File, ResolutionSource, error) {
	// 1. Project-local — only when an explicit root was provided.
	if rootPath != "" {
		for _, dir := range projectProfileDirs(rootPath) {
			p, f, found, err := lookupInDir(dir, name)
			if err != nil {
				return nil, nil, "", err
			}
			if found {
				return p, f, SourceProjectLocal, nil
			}
		}
	}

	// 2. User-global — XDG_CONFIG_HOME → $HOME/.config
	if dir := userProfileDir(); dir != "" {
		p, f, found, err := lookupInDir(dir, name)
		if err != nil {
			return nil, nil, "", err
		}
		if found {
			return p, f, SourceUserGlobal, nil
		}
	}

	// 3. Catalog-embedded — caller-supplied callback.
	if catalogLookup != nil {
		if p, ok := catalogLookup(name); ok {
			return p, nil, SourceCatalogEmbedded, nil
		}
	}

	return nil, nil, "", ErrProfileNotFound
}

// resolveInherits overlays p on its base profile. Spec §9.3 allows one level:
// the base's own `inherits` is dropped rather than followed. The base is looked
// up in p's own file first, which is what makes the documented one-file layout
// (a _base.* definition beside the profile that inherits it) work, then through
// the same three-level hierarchy.
func resolveInherits(rootPath string, p *Profile, file *File, catalogLookup CatalogLookup) (*Profile, error) {
	if p == nil || p.Inherits == "" {
		return p, nil
	}
	base := file.Lookup(p.Inherits)
	if base == nil {
		found, _, _, err := resolveRaw(rootPath, p.Inherits, catalogLookup)
		if err != nil {
			return nil, fmt.Errorf("profile %q: inherits=%q: %w", p.Name, p.Inherits, err)
		}
		base = found
	}
	baseCopy := *base
	baseCopy.Inherits = ""
	merged := MergeProfiles(p, &baseCopy)
	merged.Inherits = ""
	return merged, nil
}

// LoadOverrideBindings returns the merged §9.2 [override_bindings] table from
// the project-local and user-global layers, project-local winning on a shared
// key (docs/expression-profile-dsl.md rule 4). Catalog-embedded profiles carry
// no bindings: the table exists to attach a profile without rebuilding them.
//
// Pass rootPath "" to read the user-global layer alone. Returns nil when no
// file declares a binding.
func LoadOverrideBindings(rootPath string) (map[string]string, error) {
	var layers []map[string]string
	if rootPath != "" {
		for _, dir := range projectProfileDirs(rootPath) {
			m, err := bindingsInDir(dir)
			if err != nil {
				return nil, err
			}
			layers = append(layers, m)
		}
	}
	if dir := userProfileDir(); dir != "" {
		m, err := bindingsInDir(dir)
		if err != nil {
			return nil, err
		}
		layers = append(layers, m)
	}
	return MergeOverrideBindings(layers...), nil
}

// projectProfileDirs returns every `<dir>/.gum/profiles` from rootPath up to the
// filesystem root, nearest first. Non-existent directories are skipped.
func projectProfileDirs(rootPath string) []string {
	var dirs []string
	dir := filepath.Clean(rootPath)
	for {
		candidate := filepath.Join(dir, ".gum", "profiles")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			dirs = append(dirs, candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dirs
		}
		dir = parent
	}
}

// userProfileDir resolves the user-global profile directory using
// XDG_CONFIG_HOME when set, falling back to $HOME/.config. Returns "" when
// neither is available.
func userProfileDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "gum", "profiles")
}

// lookupInDir finds the profile named name in dir. `<name>.toml` is read first
// because that is where a single-profile file lives; when it is absent, or is an
// envelope file that defines other names, the remaining `*.toml` files are read
// in name order, because the envelope lets one file define many profiles.
func lookupInDir(dir, name string) (*Profile, *File, bool, error) {
	direct := filepath.Join(dir, name+".toml")
	f, err := readProfileFile(direct)
	if err != nil {
		return nil, nil, false, err
	}
	if f != nil {
		if p := profileFromFile(f, name); p != nil {
			return p, f, true, nil
		}
	}

	others, err := tomlFilesIn(dir)
	if err != nil {
		return nil, nil, false, err
	}
	for _, path := range others {
		if path == direct {
			continue
		}
		other, oerr := readProfileFile(path)
		if oerr != nil {
			return nil, nil, false, oerr
		}
		if other == nil || !other.Envelope {
			continue
		}
		if p := other.Lookup(name); p != nil {
			return p, other, true, nil
		}
	}
	return nil, nil, false, nil
}

// profileFromFile returns the definition named name. A bare-key file names its
// single profile after the file, so the name is stamped on it here; an envelope
// file carries the name in its table header.
func profileFromFile(f *File, name string) *Profile {
	if !f.Envelope {
		if len(f.Profiles) == 1 {
			f.Profiles[0].Name = name
			return f.Profiles[0]
		}
		return nil
	}
	return f.Lookup(name)
}

// bindingsInDir merges the [override_bindings] tables of every `*.toml` in dir,
// first file in name order winning a shared key.
func bindingsInDir(dir string) (map[string]string, error) {
	paths, err := tomlFilesIn(dir)
	if err != nil {
		return nil, err
	}
	var layers []map[string]string
	for _, path := range paths {
		f, ferr := readProfileFile(path)
		if ferr != nil {
			return nil, ferr
		}
		if f != nil && len(f.OverrideBindings) > 0 {
			layers = append(layers, f.OverrideBindings)
		}
	}
	return MergeOverrideBindings(layers...), nil
}

// tomlFilesIn lists dir's `*.toml` files in name order. A missing directory is
// not an error: it means the layer declares nothing.
func tomlFilesIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		paths = append(paths, filepath.Join(dir, e.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

// readProfileFile parses and validates one profile file. It returns (nil, nil)
// when the file does not exist. A malformed file is an error rather than a skip:
// docs/expression-profile-dsl.md requires the load to fail closed so a typo is
// not silently dropped.
func readProfileFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	f, err := ParseFile(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, p := range f.Profiles {
		if verr := ValidateSemantics(p); verr != nil {
			return nil, fmt.Errorf("%s: %w", path, verr)
		}
	}
	return f, nil
}
