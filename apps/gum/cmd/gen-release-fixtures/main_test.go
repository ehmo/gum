package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegenerationMatchesReleaseFixtures(t *testing.T) {
	out := filepath.Join(t.TempDir(), "release")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "stale.json"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	savedArgs, savedFlags := os.Args, flag.CommandLine
	os.Args = []string{"gen-release-fixtures", "-out", out}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	t.Cleanup(func() { os.Args, flag.CommandLine = savedArgs, savedFlags })
	main()
	if _, err := os.Stat(filepath.Join(out, "stale.json")); !os.IsNotExist(err) {
		t.Fatalf("stale fixture survived regeneration: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 || manifest.Total != 200 || len(manifest.Entries) != manifest.Total {
		t.Fatalf("manifest schema=%d total=%d entries=%d", manifest.SchemaVersion, manifest.Total, len(manifest.Entries))
	}
	counts := map[string]int{}
	for _, entry := range manifest.Entries {
		counts[entry.Category]++
		request, err := os.ReadFile(filepath.Join(out, entry.Path, "request.json"))
		if err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			OpID string `json:"op_id"`
		}
		if err := json.Unmarshal(request, &parsed); err != nil || parsed.OpID != entry.OpID {
			t.Fatalf("request %s op=%s want %s error=%v", entry.Path, parsed.OpID, entry.OpID, err)
		}
	}
	for category, want := range map[string]int{"workspace_toon_read": 100, "gum_parallel_batch": 40, "non_workspace_read": 30, "write_destructive": 30} {
		if counts[category] != want {
			t.Errorf("%s count=%d want %d", category, counts[category], want)
		}
	}
	baseline := filepath.Join("..", "..", "internal", "bench", "fixtures", "release")
	files := 0
	err = filepath.WalkDir(out, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(out, path)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(filepath.Join(baseline, rel))
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			t.Errorf("fixture drift: %s", rel)
		}
		files++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files != 401 {
		t.Fatalf("generated %d files; want manifest and 200 request/response pairs", files)
	}
}

func TestFixtureWriteFailuresIdentifyOperation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
		invoke  func(string)
	}{
		{"marshal", "marshal", func(dir string) { writeJSON(filepath.Join(dir, "bad.json"), make(chan int)) }},
		{"write", "write", func(dir string) { writeJSON(dir, map[string]any{}) }},
		{"mkdir", "mkdir", func(dir string) {
			blocker := filepath.Join(dir, "file")
			if err := os.WriteFile(blocker, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			writeFixture(blocker, "category", "name", 1, "op", nil, nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				got := recover()
				if err, ok := got.(error); !ok || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("panic=%v; want %s error", got, tc.message)
				}
			}()
			tc.invoke(t.TempDir())
		})
	}
}
