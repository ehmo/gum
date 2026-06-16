package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultRegistryListAndResolve(t *testing.T) {
	reg := DefaultRegistry()
	items := reg.List()
	if len(items) != 3 {
		t.Fatalf("List len=%d want 3: %#v", len(items), items)
	}
	core, err := reg.Resolve("core", LatestVersion)
	if err != nil {
		t.Fatalf("Resolve core: %v", err)
	}
	if core.Version != "1.0.0" || core.MinGum != "1.0.0" || !strings.Contains(core.Body, "gum auth use-oauth-client") {
		t.Fatalf("core skill = %#v", core)
	}
	hasp, err := reg.Resolve("hasp", LatestVersion)
	if err != nil {
		t.Fatalf("Resolve hasp: %v", err)
	}
	if !strings.Contains(hasp.Body, "hasp run") || !strings.Contains(hasp.Body, "Keep secret values out of prompts") {
		t.Fatalf("hasp skill = %#v", hasp)
	}
	if _, err := reg.Resolve("missing", LatestVersion); err == nil {
		t.Fatal("Resolve missing = nil error")
	}
}

func TestInstallableSkillsMatchRepoCopies(t *testing.T) {
	installables := DefaultInstallableSkills()
	for _, item := range installables {
		for _, file := range item.Files {
			repoPath := filepath.Join("..", "..", "..", "..", "skills", item.Directory, filepath.FromSlash(file.Path))
			got, err := os.ReadFile(repoPath)
			if err != nil {
				t.Fatalf("read %s: %v", repoPath, err)
			}
			if string(got) != file.Contents {
				t.Fatalf("%s drifted from embedded installable", repoPath)
			}
		}
	}
}

func TestValidationHelpers(t *testing.T) {
	if !ValidName("gum-mcp") || ValidName("Gum") || ValidName(strings.Repeat("a", MaxNameBytes+1)) {
		t.Fatalf("ValidName contract changed")
	}
	if !ValidVersionSelector("") || !ValidVersionSelector("latest") || !ValidVersionSelector("1.2.3") || ValidVersionSelector("v1.2.3") {
		t.Fatalf("ValidVersionSelector contract changed")
	}
}

func TestRegistryRejectsInvalidDefinitions(t *testing.T) {
	cases := []Definition{
		{Name: "Bad", Version: "1.0.0", MinGum: "1.0.0", Summary: "x", Body: "x"},
		{Name: "x", Version: "v1", MinGum: "1.0.0", Summary: "x", Body: "x"},
		{Name: "x", Version: "1.0.0", MinGum: "bad", Summary: "x", Body: "x"},
		{Name: "x", Version: "1.0.0", MinGum: "1.0.0", Body: "x"},
		{Name: "x", Version: "1.0.0", MinGum: "1.0.0", Summary: "x"},
	}
	for _, def := range cases {
		if _, err := NewRegistry([]Definition{def}); err == nil {
			t.Fatalf("NewRegistry(%#v) err=nil", def)
		}
	}
	if _, err := NewRegistry([]Definition{
		{Name: "x", Version: "1.0.0", MinGum: "1.0.0", Summary: "x", Body: "x"},
		{Name: "x", Version: "1.0.0", MinGum: "1.0.0", Summary: "x", Body: "x"},
	}); err == nil {
		t.Fatal("duplicate registry err=nil")
	}
}

func TestRegistrySortsVersionsAndResolvesSpecificVersion(t *testing.T) {
	reg, err := NewRegistry([]Definition{
		{Name: "x", Version: "1.0.1", MinGum: "1.0.0", Summary: "new", Body: "new"},
		{Name: "x", Version: "1.0.0", MinGum: "1.0.0", Summary: "old", Body: "old"},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	latest, err := reg.Resolve("x", LatestVersion)
	if err != nil || latest.Body != "new" {
		t.Fatalf("latest=%#v err=%v", latest, err)
	}
	old, err := reg.Resolve("x", "1.0.0")
	if err != nil || old.Body != "old" {
		t.Fatalf("old=%#v err=%v", old, err)
	}
	if _, err := reg.Resolve("x", "9.9.9"); err == nil {
		t.Fatal("missing version err=nil")
	}
}
