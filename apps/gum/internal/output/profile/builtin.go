package profile

import (
	"embed"
	"strings"
	"sync"
)

// builtinFS holds the catalog-embedded (spec §9.2 third-layer) expression
// profiles baked into the binary. Each `builtin/<name>.toml` is a single flat
// profile whose file stem is the profile name referenced by a catalog variant's
// output_profile field.
//
//go:embed builtin/*.toml
var builtinFS embed.FS

var (
	builtinOnce   sync.Once
	builtinByName map[string]*Profile
)

func loadBuiltins() {
	builtinByName = map[string]*Profile{}
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		data, rerr := builtinFS.ReadFile("builtin/" + e.Name())
		if rerr != nil {
			continue
		}
		p, perr := Parse(string(data))
		if perr != nil {
			// A malformed built-in is a build-time authoring error caught by
			// TestBuiltinProfilesValid; skip it at runtime rather than panic.
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".toml")
		p.Name = name
		builtinByName[name] = p
	}
}

// BuiltinLookup resolves a built-in (catalog-embedded, §9.2 third-layer)
// expression profile by name. It satisfies both the dispatch ProfileLookup
// shape and the resolver CatalogLookup callback, so presentation layers and the
// dispatch kernel resolve the same set. Returns (nil, false) on a miss.
func BuiltinLookup(name string) (*Profile, bool) {
	builtinOnce.Do(loadBuiltins)
	p, ok := builtinByName[name]
	return p, ok
}

// BuiltinNames returns the sorted list of built-in profile names. Used by tests
// and `gum profile` tooling to enumerate the embedded set.
func BuiltinNames() []string {
	builtinOnce.Do(loadBuiltins)
	names := make([]string, 0, len(builtinByName))
	for n := range builtinByName {
		names = append(names, n)
	}
	// small set; simple insertion-free sort
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j-1] > names[j]; j-- {
			names[j-1], names[j] = names[j], names[j-1]
		}
	}
	return names
}
