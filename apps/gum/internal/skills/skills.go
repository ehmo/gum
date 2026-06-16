package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	LatestVersion   = "latest"
	NamePattern     = "^[a-z0-9-]+$"
	VersionPattern  = "^(latest|[0-9]+\\.[0-9]+\\.[0-9]+)$"
	MaxNameBytes    = 64
	MaxVersionBytes = 32
	latestVersion   = LatestVersion
)

var (
	ErrUnknownSkill   = errors.New("unknown skill")
	ErrUnknownVersion = errors.New("unknown skill version")
)

type Definition struct {
	Name    string
	Version string
	Summary string
	MinGum  string
	Body    string
}

type Skill struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Summary string `json:"summary"`
	MinGum  string `json:"min_gum"`
	SHA256  string `json:"sha256"`
	Bytes   int    `json:"bytes"`
	Body    string `json:"body"`
}

type Summary struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Summary string `json:"summary"`
	MinGum  string `json:"min_gum"`
	SHA256  string `json:"sha256"`
	Bytes   int    `json:"bytes"`
}

type Registry struct {
	byName map[string][]record
}

type record struct {
	skill   Skill
	version semver
}

type semver struct {
	major int
	minor int
	patch int
}

func NewRegistry(defs []Definition) (Registry, error) {
	registry := Registry{byName: map[string][]record{}}
	seen := map[string]bool{}
	for _, def := range defs {
		if !ValidName(def.Name) {
			return Registry{}, fmt.Errorf("invalid skill name: %s", def.Name)
		}
		version, err := parseSemver(def.Version)
		if err != nil {
			return Registry{}, fmt.Errorf("invalid skill version %s: %w", def.Version, err)
		}
		if _, err := parseSemver(def.MinGum); err != nil {
			return Registry{}, fmt.Errorf("invalid min gum version %s: %w", def.MinGum, err)
		}
		if strings.TrimSpace(def.Summary) == "" {
			return Registry{}, fmt.Errorf("skill %s@%s missing summary", def.Name, def.Version)
		}
		if strings.TrimSpace(def.Body) == "" {
			return Registry{}, fmt.Errorf("skill %s@%s missing body", def.Name, def.Version)
		}
		key := def.Name + "@" + def.Version
		if seen[key] {
			return Registry{}, fmt.Errorf("duplicate skill version: %s", key)
		}
		seen[key] = true
		sum := sha256.Sum256([]byte(def.Body))
		skill := Skill{
			Name:    def.Name,
			Version: def.Version,
			Summary: def.Summary,
			MinGum:  def.MinGum,
			SHA256:  hex.EncodeToString(sum[:]),
			Bytes:   len([]byte(def.Body)),
			Body:    def.Body,
		}
		registry.byName[def.Name] = append(registry.byName[def.Name], record{skill: skill, version: version})
	}
	for name := range registry.byName {
		sort.Slice(registry.byName[name], func(i, j int) bool {
			return registry.byName[name][i].version.less(registry.byName[name][j].version)
		})
	}
	return registry, nil
}

func DefaultRegistry() Registry {
	registry, err := NewRegistry(defaultDefinitions)
	if err != nil {
		panic(err)
	}
	return registry
}

func (r Registry) List() []Summary {
	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Summary, 0, len(names))
	for _, name := range names {
		entries := r.byName[name]
		out = append(out, summaryFor(entries[len(entries)-1].skill))
	}
	return out
}

func (r Registry) Resolve(name, version string) (Skill, error) {
	if version == "" {
		version = latestVersion
	}
	entries, ok := r.byName[name]
	if !ok {
		return Skill{}, fmt.Errorf("%w: %s", ErrUnknownSkill, name)
	}
	if version == latestVersion {
		return cloneSkill(entries[len(entries)-1].skill), nil
	}
	for _, entry := range entries {
		if entry.skill.Version == version {
			return cloneSkill(entry.skill), nil
		}
	}
	return Skill{}, fmt.Errorf("%w: %s@%s", ErrUnknownVersion, name, version)
}

func summaryFor(skill Skill) Summary {
	return Summary{
		Name:    skill.Name,
		Version: skill.Version,
		Summary: skill.Summary,
		MinGum:  skill.MinGum,
		SHA256:  skill.SHA256,
		Bytes:   skill.Bytes,
	}
}

func cloneSkill(skill Skill) Skill {
	return Skill{
		Name:    skill.Name,
		Version: skill.Version,
		Summary: skill.Summary,
		MinGum:  skill.MinGum,
		SHA256:  skill.SHA256,
		Bytes:   skill.Bytes,
		Body:    string([]byte(skill.Body)),
	}
}

func ValidName(raw string) bool {
	if raw == "" || len(raw) > MaxNameBytes {
		return false
	}
	for _, ch := range raw {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func ValidVersionSelector(raw string) bool {
	if raw == "" || raw == LatestVersion {
		return true
	}
	if len(raw) > MaxVersionBytes {
		return false
	}
	_, err := parseSemver(raw)
	return err == nil
}

func parseSemver(raw string) (semver, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return semver{}, errors.New("must be MAJOR.MINOR.PATCH")
	}
	values := [3]int{}
	for i, part := range parts {
		if part == "" {
			return semver{}, errors.New("empty version component")
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return semver{}, errors.New("version components must be numeric")
			}
			values[i] = values[i]*10 + int(ch-'0')
		}
	}
	return semver{major: values[0], minor: values[1], patch: values[2]}, nil
}

func (v semver) less(other semver) bool {
	if v.major != other.major {
		return v.major < other.major
	}
	if v.minor != other.minor {
		return v.minor < other.minor
	}
	return v.patch < other.patch
}
