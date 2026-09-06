package skills

import (
	"errors"
	"strings"
	"testing"
)

func TestInstallableSkillsRequireAllDefinitions(t *testing.T) {
	for _, missing := range []string{"core", "mcp", "hasp"} {
		t.Run(missing, func(t *testing.T) {
			var defs []Definition
			for _, def := range defaultDefinitions {
				if def.Name != missing {
					defs = append(defs, def)
				}
			}
			registry, err := NewRegistry(defs)
			if err != nil {
				t.Fatal(err)
			}
			files, err := InstallableSkills(registry)
			if files != nil || !errors.Is(err, ErrUnknownSkill) || !strings.Contains(err.Error(), missing) {
				t.Fatalf("files=%v error=%v; want missing %s", files, err, missing)
			}
		})
	}
}

func TestRegistryLatestUsesNumericVersionOrder(t *testing.T) {
	versions := []string{"2.0.0", "1.10.0", "1.2.1", "1.2.0", "1.0.0"}
	var defs []Definition
	for _, version := range versions {
		defs = append(defs, Definition{Name: "core", Version: version, MinGum: "1.0.0", Summary: version, Body: version})
	}
	registry, err := NewRegistry(defs)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := registry.Resolve("core", "")
	if err != nil || latest.Version != "2.0.0" {
		t.Fatalf("latest=%v error=%v", latest, err)
	}
	for i, want := range []string{"1.0.0", "1.2.0", "1.2.1", "1.10.0", "2.0.0"} {
		if got := registry.byName["core"][i].skill.Version; got != want {
			t.Errorf("version[%d]=%s want %s", i, got, want)
		}
	}
}

func TestVersionSelectorRejectsEmptyComponentsAndOversize(t *testing.T) {
	for _, raw := range []string{"1..0", ".1.0", "1.0.", strings.Repeat("1", MaxVersionBytes) + ".0.0"} {
		if ValidVersionSelector(raw) {
			t.Errorf("accepted invalid version %q", raw)
		}
	}
}

func TestDefaultRegistryPanicsForInvalidBuiltins(t *testing.T) {
	saved := defaultDefinitions
	defaultDefinitions = []Definition{{Name: "invalid/name"}}
	t.Cleanup(func() { defaultDefinitions = saved })
	defer func() {
		if got := recover(); got == nil {
			t.Fatal("invalid built-in definition did not panic")
		} else if err, ok := got.(error); !ok || !strings.Contains(err.Error(), "invalid skill name") {
			t.Fatalf("panic=%v; want invalid skill name", got)
		}
	}()
	DefaultRegistry()
}
