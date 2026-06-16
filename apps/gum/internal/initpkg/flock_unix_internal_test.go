//go:build !windows

package initpkg

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAcquireSettingsLockHappyPath pins the no-contention return: a
// fresh lock path acquires immediately and the release closer unwinds
// the flock without error.
func TestAcquireSettingsLockHappyPath(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "claude_desktop_config.lock")
	release, err := acquireSettingsLock(lockPath, 1*time.Second)
	if err != nil {
		t.Fatalf("acquireSettingsLock: %v", err)
	}
	if release == nil {
		t.Fatal("release func nil on success")
	}
	if err := release(); err != nil {
		t.Errorf("release: %v", err)
	}
}

// TestAcquireSettingsLockTimeoutOnContention drives the EWOULDBLOCK
// retry loop into the deadline branch: holding the lock from a
// goroutine for longer than the second acquire's timeout must return
// a "timeout after" error rather than blocking forever.
func TestAcquireSettingsLockTimeoutOnContention(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "settings.lock")

	// Take the lock and hold it past the second acquire's deadline.
	holdRelease, err := acquireSettingsLock(lockPath, 1*time.Second)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	hold := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-hold
		_ = holdRelease()
	}()
	defer func() {
		close(hold)
		wg.Wait()
	}()

	start := time.Now()
	if _, err := acquireSettingsLock(lockPath, 100*time.Millisecond); err == nil {
		t.Fatal("contended acquire returned nil error; want timeout")
	} else if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("err=%q; want 'timeout' marker", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("returned too fast: %v; want >=100ms", elapsed)
	}
}

// TestParentDirShapes pins the path-tail extraction the lock-mkdir uses.
// The default-"." branch covers caller paths like a bare "name" that
// have no separator.
func TestParentDirShapes(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/a/b/c", "/a/b"},
		{`C:\foo\bar`, `C:\foo`},
		{"name", "."},
		{"", "."},
	}
	for _, tc := range cases {
		if got := parentDir(tc.in); got != tc.want {
			t.Errorf("parentDir(%q)=%q; want %q", tc.in, got, tc.want)
		}
	}
}
