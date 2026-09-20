package plugins

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// cancelledCtx returns a context that is already cancelled, which is the
// hermetic way to make registry.WriteTransaction fail: it checks ctx.Err()
// before it takes the install lock, so no filesystem damage is needed.
func cancelledCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// TestSetupCredentialsCanaryAndRegistryBothFail pins setup.go:129-133: the
// canary failed AND the quarantine could not be recorded. The returned error
// must name the registry failure, because a plugin the host could not
// quarantine is a different operator problem from one it did.
func TestSetupCredentialsCanaryAndRegistryBothFail(t *testing.T) {
	installRoot := t.TempDir()
	descs := []CredentialDescriptor{{
		Alias: "session", Env: "PLUG_SESSION", Kind: "session",
		DisplayName: "Session", SetupHint: "see docs",
	}}
	writeTestManifest(t, installRoot, "p", []string{"PLUG_SESSION"}, descs)

	err := SetupCredentials(cancelledCtx(t), "p", SetupOptions{
		Registry:    registry.New(t.TempDir()),
		Profile:     "prof",
		InstallRoot: installRoot,
		Keyring:     newFakeKeyring(),
		In:          strings.NewReader("sekret\n"),
		Out:         &bytes.Buffer{},
		RunCanary:   func(context.Context, string) error { return errors.New("canary down") },
	})
	if err == nil {
		t.Fatal("SetupCredentials err=nil; want the registry failure")
	}
	if !strings.Contains(err.Error(), "registry update failed") {
		t.Errorf("err=%v; want the registry-update arm", err)
	}
	if strings.Contains(err.Error(), "canary down") {
		t.Errorf("err=%v; must not leak the canary's own error text", err)
	}
}

// TestSupervisorStartCrashRecordFailure pins supervisor.go:255-257: the spawn
// failed and the crash could not be persisted. The returned error names both,
// because a lost crash record means the backoff ladder did not advance.
func TestSupervisorStartCrashRecordFailure(t *testing.T) {
	sup := NewSupervisor(registry.New(t.TempDir()), func(context.Context, string) (*Plugin, error) {
		return nil, errors.New("spawn refused")
	}, time.Now)

	_, err := sup.Start(cancelledCtx(t), "flights")
	if err == nil {
		t.Fatal("Start err=nil; want the persistence failure")
	}
	if !strings.Contains(err.Error(), "state persistence failed") {
		t.Errorf("err=%v; want the persistence arm", err)
	}
	if !strings.Contains(err.Error(), "spawn refused") {
		t.Errorf("err=%v; want the spawn cause named too", err)
	}
}

// TestSupervisorStartClearQuarantineFailure pins supervisor.go:260-264: the
// spawn succeeded but the backoff state could not be cleared. The handle is
// stopped before the error returns, so a plugin the supervisor cannot account
// for never stays live.
func TestSupervisorStartClearQuarantineFailure(t *testing.T) {
	reg := registry.New(t.TempDir())
	crashed := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	if _, err := RecordCrash(context.Background(), reg, "flights", "SERVICE_DOWN", crashed); err != nil {
		t.Fatalf("seed crash: %v", err)
	}

	spawned := &Plugin{pluginID: "flights"}
	sup := NewSupervisor(reg, func(context.Context, string) (*Plugin, error) {
		return spawned, nil
	}, func() time.Time { return crashed.Add(time.Hour) })

	_, err := sup.Start(cancelledCtx(t), "flights")
	if err == nil {
		t.Fatal("Start err=nil; want the ClearQuarantine failure")
	}
	if !strings.Contains(err.Error(), "ClearQuarantine failed") {
		t.Errorf("err=%v; want the clear-quarantine arm", err)
	}
	if !spawned.dead.Load() {
		t.Error("spawned handle left live after the supervisor gave up on it")
	}
}
