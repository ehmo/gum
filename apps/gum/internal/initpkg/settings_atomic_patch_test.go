package initpkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSettingsAtomicPatch is the docs/test-matrix.md row 217 proof for the
// spec §12.2 "Atomic settings.json patch" requirement: the advisory lock is
// the canonical mutex for the whole read-merge-write, concurrent `gum init`
// runs serialize instead of clobbering each other, and a lock that cannot be
// taken fails the patch rather than racing it.
func TestSettingsAtomicPatch(t *testing.T) {
	t.Run("a key written after the preview survives the patch", func(t *testing.T) {
		dir := t.TempDir()
		target := patchTarget(dir)
		writeSettings(t, target, `{"other":"a"}`)

		// gum init previews the diff before it asks for confirmation, so the
		// read behind this plan is always older than the write below.
		plan, err := PlanPatch(target, "gum", DefaultMCPEntry())
		if err != nil {
			t.Fatalf("PlanPatch: %v", err)
		}
		if _, stale := plan.After["theirs"]; stale {
			t.Fatal("preview already sees the concurrent key; the test proves nothing")
		}
		// Another writer lands between the preview and the apply.
		writeSettings(t, target, `{"other":"a","theirs":"b"}`)

		if err := Apply(target, "gum", DefaultMCPEntry(), time.Second); err != nil {
			t.Fatalf("Apply: %v", err)
		}

		got := readSettings(t, target)
		if got["theirs"] != "b" {
			t.Errorf("concurrent key dropped: settings = %v", got)
		}
		if _, ok := patchedServers(t, got)["gum"]; !ok {
			t.Errorf("gum entry missing: settings = %v", got)
		}
	})

	t.Run("concurrent patches all survive", func(t *testing.T) {
		dir := t.TempDir()
		target := patchTarget(dir)

		const writers = 8
		names := make([]string, writers)
		for i := range names {
			names[i] = fmt.Sprintf("server%d", i)
		}

		var start sync.WaitGroup
		start.Add(1)
		var done sync.WaitGroup
		errs := make([]error, writers)
		for i := range names {
			done.Add(1)
			go func(i int) {
				defer done.Done()
				start.Wait()
				errs[i] = Apply(target, names[i], DefaultMCPEntry(), 10*time.Second)
			}(i)
		}
		start.Done()
		done.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("Apply %s: %v", names[i], err)
			}
		}
		servers := patchedServers(t, readSettings(t, target))
		for _, name := range names {
			if _, ok := servers[name]; !ok {
				t.Errorf("entry %s lost; file holds %d of %d entries", name, len(servers), writers)
			}
		}
	})

	t.Run("a held lock fails the patch without touching the file", func(t *testing.T) {
		dir := t.TempDir()
		target := patchTarget(dir)
		writeSettings(t, target, `{"other":"a"}`)
		before, err := os.ReadFile(target.Path)
		if err != nil {
			t.Fatalf("read prior: %v", err)
		}

		release, err := acquireSettingsLock(target.LockPath, time.Second)
		if err != nil {
			t.Fatalf("hold lock: %v", err)
		}
		defer func() { _ = release() }()

		err = Apply(target, "gum", DefaultMCPEntry(), 150*time.Millisecond)
		if err == nil {
			t.Fatal("Apply err=nil; want the lock timeout")
		}
		if !strings.Contains(err.Error(), "timeout after") {
			t.Errorf("err=%v; want a lock-timeout error", err)
		}
		if !strings.Contains(err.Error(), target.LockPath) {
			t.Errorf("err=%v; want the lock path named", err)
		}
		after, err := os.ReadFile(target.Path)
		if err != nil {
			t.Fatalf("read after: %v", err)
		}
		if string(after) != string(before) {
			t.Errorf("settings changed after a failed lock: %s", after)
		}
	})
}

func patchTarget(dir string) SettingsTarget {
	return SettingsTarget{
		Path:     filepath.Join(dir, ".claude", "settings.json"),
		LockPath: filepath.Join(dir, ".claude", "settings.lock"),
	}
}

func writeSettings(t *testing.T, target SettingsTarget, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target.Path, []byte(body+"\n"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

func readSettings(t *testing.T, target SettingsTarget) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(target.Path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("settings is not valid JSON (%v): %s", err, raw)
	}
	return doc
}

func patchedServers(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing or not an object: %v", doc)
	}
	return servers
}
