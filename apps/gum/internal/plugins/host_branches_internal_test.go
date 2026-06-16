package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestPluginIDNilReceiver pins the nil-receiver guard: callers (notably
// the dispatch happy-path that may pass a half-initialised handle on
// teardown) MUST receive an empty string rather than panic.
func TestPluginIDNilReceiver(t *testing.T) {
	var p *Plugin
	if got := p.PluginID(); got != "" {
		t.Errorf("nil receiver: got %q; want empty", got)
	}
}

// TestPluginStopNilReceiverNoError pins the same contract for Stop:
// nil receiver / nil session must be a no-op, since the host calls
// Stop unconditionally during graceful shutdown.
func TestPluginStopNilReceiverNoError(t *testing.T) {
	var p *Plugin
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("nil receiver: Stop err=%v; want nil", err)
	}
	// Non-nil receiver with nil session — still no-op.
	p2 := &Plugin{}
	if err := p2.Stop(context.Background()); err != nil {
		t.Errorf("nil session: Stop err=%v; want nil", err)
	}
}

// TestRemoveRejectsBadPluginID pins the regex guard. A plugin_id that
// fails the spec §8 manifest grammar must NOT trigger a real RemoveAll
// (path traversal defense — e.g. "../etc").
func TestRemoveRejectsBadPluginID(t *testing.T) {
	dir := t.TempDir()
	h := &Host{cfg: HostConfig{InstallRoot: dir}}
	// Plant a sibling file we don't want touched.
	canary := filepath.Join(filepath.Dir(dir), "should-not-be-removed")
	if err := os.WriteFile(canary, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(canary) })

	if err := h.Remove(context.Background(), "../escape"); !errors.Is(err, ErrManifestInvalid) {
		t.Errorf("Remove(../escape) err=%v; want ErrManifestInvalid", err)
	}
	if _, err := os.Stat(canary); err != nil {
		t.Errorf("canary file disappeared after Remove with bad id: %v", err)
	}
}

// TestListMissingInstallRootReturnsNilNil pins the os.IsNotExist branch:
// when ~/.local/share/gum/plugins/ has never been created, List MUST
// return (nil, nil) rather than surfacing a confusing "no such file" to
// `gum plugin list`.
func TestListMissingInstallRootReturnsNilNil(t *testing.T) {
	h := &Host{cfg: HostConfig{InstallRoot: filepath.Join(t.TempDir(), "never-existed")}}
	got, err := h.List()
	if err != nil {
		t.Errorf("List on missing root: err=%v; want nil", err)
	}
	if got != nil {
		t.Errorf("List on missing root: got %v; want nil slice", got)
	}
}

// TestListSkipsNonDirEntries pins the !entry.IsDir() continue branch:
// stray files (e.g. .DS_Store) in the install root must NOT yield
// manifest parse errors.
func TestListSkipsNonDirEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &Host{cfg: HostConfig{InstallRoot: dir}}
	got, err := h.List()
	if err != nil {
		t.Errorf("List with stray file: err=%v; want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("List with stray file: got %d manifests; want 0", len(got))
	}
}
