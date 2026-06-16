package profile

import (
	"path/filepath"
	"testing"
)

func TestParseProfileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		want    Name
		wantErr bool
	}{
		{name: "empty_defaults", raw: "", want: DefaultName},
		{name: "simple", raw: "team-a", want: "team-a"},
		{name: "dot_underscore", raw: "team.a_b", want: "team.a_b"},
		{name: "slash", raw: "../escape", wantErr: true},
		{name: "double_dot", raw: "team..a", wantErr: true},
		{name: "space", raw: "team a", wantErr: true},
		{name: "trimmed_space", raw: " team", wantErr: true},
		{name: "absolute", raw: "/tmp/profile", wantErr: true},
		{name: "control", raw: "team\nx", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q)=nil err, want error", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("Parse(%q)=%q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestResolveProfilePrecedence(t *testing.T) {
	t.Setenv(EnvVar, "from-env")
	got, err := Resolve("from-flag", true)
	if err != nil {
		t.Fatalf("Resolve flag: %v", err)
	}
	if got != "from-flag" {
		t.Errorf("flag changed got %q, want from-flag", got)
	}

	got, err = Resolve("default", false)
	if err != nil {
		t.Fatalf("Resolve env: %v", err)
	}
	if got != "from-env" {
		t.Errorf("env got %q, want from-env", got)
	}
}

func TestResolveProfileFallsBackToFlagDefault(t *testing.T) {
	t.Setenv(EnvVar, "")
	got, err := Resolve("flag-default", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "flag-default" {
		t.Errorf("got %q, want flag-default", got)
	}
}

func TestProfilePathsStayUnderGumRoot(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))

	name, err := Parse("team-a")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for label, fn := range map[string]func() (string, error){
		"data":   name.DataDir,
		"cache":  name.CacheDir,
		"config": name.ConfigPath,
		"notify": name.NotifyPath,
	} {
		got, err := fn()
		if err != nil {
			t.Fatalf("%s path: %v", label, err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("%s path %q is not absolute", label, got)
		}
		if filepath.Clean(got) != got {
			t.Errorf("%s path %q is not clean", label, got)
		}
	}
}

func TestProfilePathHomeFallbacks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	name := Name("")

	gotConfig, err := name.ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if want := filepath.Join(home, ".config", "gum", "default", "config.toml"); gotConfig != want {
		t.Errorf("ConfigPath = %q, want %q", gotConfig, want)
	}

	gotData, err := name.DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if want := filepath.Join(home, ".local", "share", "gum", "default"); gotData != want {
		t.Errorf("DataDir = %q, want %q", gotData, want)
	}

	gotCache, err := name.CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	if want := filepath.Join(home, ".cache", "gum", "default"); gotCache != want {
		t.Errorf("CacheDir = %q, want %q", gotCache, want)
	}

	gotNotify, err := name.NotifyPath()
	if err != nil {
		t.Fatalf("NotifyPath: %v", err)
	}
	if want := filepath.Join(home, ".cache", "gum", "default", "notify.json"); gotNotify != want {
		t.Errorf("NotifyPath = %q, want %q", gotNotify, want)
	}
}
