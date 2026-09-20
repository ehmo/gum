// file.go — the profile-file envelope documented in
// docs/expression-profile-dsl.md "File Shape".
//
// A profile file holds one or both top-level tables: [output_profiles."<name>"]
// definitions and an [override_bindings] table. Parse reads a single bare-key
// profile body, which is what a catalog-embedded builtin ships as and what a
// variant's output_profile names; ParseFile reads a whole file in either shape
// and is the only entry point for user-global and project-local files.
package profile

import (
	"fmt"
	"sort"
	"strings"
)

// outputProfilesPrefix opens a profile definition table. The name that follows
// is quoted because profile names carry dots ("gmail.messages.list.v1"), which
// TOML would otherwise read as further table nesting.
const (
	outputProfilesPrefix   = "[output_profiles."
	overrideBindingsHeader = "[override_bindings]"
	testsHeader            = "[[tests]]"
)

// File is one parsed profile TOML file.
type File struct {
	// Profiles are the file's definitions in declaration order. A bare-key file
	// yields exactly one, with an empty Name: only the file name knows it.
	Profiles []*Profile

	// OverrideBindings maps op_id (or variant_id) to a profile name, from the
	// file's [override_bindings] table. Spec §9.2: the binding is evaluated
	// after the three-level profile-name resolution and substitutes the named
	// profile for the targeted operation without touching catalog data. Nil
	// when the file declares no bindings.
	OverrideBindings map[string]string

	// Envelope reports whether the file used the [output_profiles] /
	// [override_bindings] shape rather than the bare-key shape.
	Envelope bool
}

// Lookup returns the definition named name, or nil when this file has none.
func (f *File) Lookup(name string) *Profile {
	if f == nil {
		return nil
	}
	for _, p := range f.Profiles {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// ParseFile parses a whole profile file. It accepts both documented shapes:
//
//   - The envelope: any number of [output_profiles."<name>"] tables, an optional
//     [override_bindings] table, and top-level [[tests]] entries whose `profile`
//     key names the definition each fixture targets.
//   - Bare keys: profile keys at the top level with no [output_profiles] header,
//     one profile per file, its name supplied by the caller (the file name for a
//     filesystem profile, the embed path for a builtin). [[tests]] entries all
//     attach to that one profile.
//
// The two shapes cannot be mixed in one file.
func ParseFile(src string) (*File, error) {
	f := &File{}
	var (
		cur      *Profile     // current [output_profiles."<name>"] table
		curTest  *TestFixture // current [[tests]] entry
		bare     *Profile     // the implicit profile of a bare-key file
		bindings bool         // inside [override_bindings]
		tests    []TestFixture
	)
	for lineNum, line := range strings.Split(src, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "[") {
			cur, curTest, bindings = nil, nil, false
			switch {
			case trimmed == testsHeader:
				tests = append(tests, TestFixture{})
				curTest = &tests[len(tests)-1]
			case trimmed == overrideBindingsHeader:
				f.Envelope = true
				if f.OverrideBindings == nil {
					f.OverrideBindings = map[string]string{}
				}
				bindings = true
			case strings.HasPrefix(trimmed, outputProfilesPrefix):
				name, err := profileSectionName(trimmed, lineNum+1)
				if err != nil {
					return nil, err
				}
				if f.Lookup(name) != nil {
					return nil, fmt.Errorf("profile: line %d: duplicate profile %q", lineNum+1, name)
				}
				f.Envelope = true
				cur = &Profile{Name: name}
				f.Profiles = append(f.Profiles, cur)
			default:
				return nil, fmt.Errorf("profile: line %d: unknown section header %q (expected [output_profiles.%q], [override_bindings], or [[tests]])", lineNum+1, trimmed, "<name>")
			}
			if f.Envelope && bare != nil {
				return nil, fmt.Errorf("profile: line %d: %s cannot follow bare top-level profile keys; put every definition under [output_profiles.\"<name>\"]", lineNum+1, trimmed)
			}
			continue
		}

		key, rawVal, err := splitKeyValue(line, lineNum+1)
		if err != nil {
			return nil, err
		}

		switch {
		case curTest != nil:
			if err := parseTestFixtureKey(curTest, key, rawVal, lineNum+1); err != nil {
				return nil, err
			}
		case bindings:
			if err := parseOverrideBinding(f.OverrideBindings, key, rawVal, lineNum+1); err != nil {
				return nil, err
			}
		case cur != nil:
			if err := parseProfileKey(cur, key, rawVal, lineNum+1); err != nil {
				return nil, err
			}
		default:
			if f.Envelope {
				return nil, fmt.Errorf("profile: line %d: key %q sits outside every table; a file that uses [output_profiles] or [override_bindings] has no bare top-level keys", lineNum+1, key)
			}
			if bare == nil {
				bare = &Profile{}
				f.Profiles = append(f.Profiles, bare)
			}
			if err := parseProfileKey(bare, key, rawVal, lineNum+1); err != nil {
				return nil, err
			}
		}
	}

	if err := f.attachTests(tests); err != nil {
		return nil, err
	}
	for _, p := range f.Profiles {
		if err := checkCollapseOnEmpty(p); err != nil {
			return nil, err
		}
	}
	if len(f.Profiles) == 0 && len(f.OverrideBindings) == 0 {
		return nil, fmt.Errorf("profile: file declares neither a profile nor an [override_bindings] entry")
	}
	return f, nil
}

// profileSectionName extracts "<name>" from [output_profiles."<name>"]. TOML
// allows either quote style for a literal key; both are accepted, an unquoted
// name is not, because a dotted name without quotes is further table nesting.
func profileSectionName(header string, lineNum int) (string, error) {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(header, outputProfilesPrefix), "]"))
	if len(inner) >= 2 && inner[0] == '\'' && inner[len(inner)-1] == '\'' {
		inner = inner[1 : len(inner)-1]
		if inner == "" {
			return "", fmt.Errorf("profile: line %d: empty profile name in %q", lineNum, header)
		}
		return inner, nil
	}
	name, err := parseStringLiteral(inner)
	if err != nil {
		return "", fmt.Errorf("profile: line %d: %q: profile name must be quoted, as [output_profiles.\"gmail.messages.list.v1\"]", lineNum, header)
	}
	if name == "" {
		return "", fmt.Errorf("profile: line %d: empty profile name in %q", lineNum, header)
	}
	return name, nil
}

// parseOverrideBinding reads one `"<op_id or variant_id>" = "<profile>"` line of
// the [override_bindings] table. The key is quoted in every documented example
// because op ids carry dots, but an unquoted key is accepted: it is unambiguous
// here, where every key is a whole target id.
func parseOverrideBinding(into map[string]string, key, rawVal string, lineNum int) error {
	target := key
	if strings.HasPrefix(key, `"`) {
		parsed, err := parseStringLiteral(key)
		if err != nil {
			return fmt.Errorf("profile: line %d: override_bindings: key: %w", lineNum, err)
		}
		target = parsed
	}
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("profile: line %d: override_bindings: empty op_id or variant_id key", lineNum)
	}
	name, err := parseStringLiteral(rawVal)
	if err != nil {
		return fmt.Errorf("profile: line %d: override_bindings[%q]: %w", lineNum, target, err)
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("profile: line %d: override_bindings[%q]: empty profile name", lineNum, target)
	}
	if prev, dup := into[target]; dup {
		return fmt.Errorf("profile: line %d: override_bindings[%q] declared twice (%q then %q)", lineNum, target, prev, name)
	}
	into[target] = name
	return nil
}

// attachTests distributes the file's top-level [[tests]] entries. In the
// envelope shape each fixture's `profile` key names its target, which must be
// defined in the same file; a file with exactly one definition may omit it. A
// bare-key file has one profile, so every fixture attaches to it and the
// `profile` key is documentation.
func (f *File) attachTests(tests []TestFixture) error {
	if len(tests) == 0 {
		return nil
	}
	if !f.Envelope {
		if len(f.Profiles) == 0 {
			return fmt.Errorf("profile: [[tests]] with no profile to attach to")
		}
		f.Profiles[0].Tests = append(f.Profiles[0].Tests, tests...)
		return nil
	}
	for _, fixture := range tests {
		switch {
		case fixture.Profile != "":
			target := f.Lookup(fixture.Profile)
			if target == nil {
				return fmt.Errorf("profile: [[tests]] %q: profile = %q is not defined in this file (defined: %s)", fixture.Name, fixture.Profile, strings.Join(f.names(), ", "))
			}
			target.Tests = append(target.Tests, fixture)
		case len(f.Profiles) == 1:
			f.Profiles[0].Tests = append(f.Profiles[0].Tests, fixture)
		default:
			return fmt.Errorf("profile: [[tests]] %q: set profile = \"<name>\"; this file defines %d profiles (%s)", fixture.Name, len(f.Profiles), strings.Join(f.names(), ", "))
		}
	}
	return nil
}

// names returns the file's profile names, sorted, for error text.
func (f *File) names() []string {
	out := make([]string, 0, len(f.Profiles))
	for _, p := range f.Profiles {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}

// MergeOverrideBindings overlays [override_bindings] tables from highest
// precedence (first) to lowest. The first table that binds a key wins, which is
// docs/expression-profile-dsl.md rule 4: a project-local override file beats a
// user-global one on the same key. Returns nil when no layer binds anything.
func MergeOverrideBindings(layers ...map[string]string) map[string]string {
	var out map[string]string
	for _, layer := range layers {
		for target, name := range layer {
			if out == nil {
				out = make(map[string]string, len(layer))
			}
			if _, present := out[target]; !present {
				out[target] = name
			}
		}
	}
	return out
}
