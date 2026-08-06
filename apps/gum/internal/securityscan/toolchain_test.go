package securityscan

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// toolchainAttempts bounds the retry applied to every nested `go` invocation
// in this package.
//
// Nested `go build` / `go list` calls transiently fail under the concurrent-
// compilation load of a full `go test ./...` run (bead gum-qooz): the outer
// runner is rebuilding every package while these tests ask the toolchain for
// another one, and the build cache and linker contend. The failure is a load
// artifact, not a security-gate result.
//
// A bounded retry masks only that transient. A real compile error, a genuine
// CGo leak, or a missing package fails identically on all three attempts, so
// no gate is weakened. The reproducibility check is likewise unaffected: it
// compares hashes of two *successful* builds, and a non-deterministic build
// still produces two differing hashes.
const toolchainAttempts = 3

// retryBackoff is the pause before attempt n+1. Linear: the contention this
// waits out is the outer runner's compile burst, which drains in well under a
// second.
const retryBackoff = 500 * time.Millisecond

// withRetry runs attempt up to toolchainAttempts times and stops at the first
// success. Between attempts it calls sleep with a linear backoff. It returns
// nil once an attempt succeeds, otherwise the last attempt's error. sleep is a
// parameter so tests do not pay the real backoff.
func withRetry(attempt func() error, sleep func(time.Duration)) error {
	var err error
	for n := 1; n <= toolchainAttempts; n++ {
		if err = attempt(); err == nil {
			return nil
		}
		if n < toolchainAttempts {
			sleep(time.Duration(n) * retryBackoff)
		}
	}
	return err
}

// runGo runs `go <args...>` from the module root with extraEnv appended to the
// inherited environment and returns stdout. It retries the transient load
// failure described on toolchainAttempts, and fails the test when every
// attempt fails.
func runGo(t *testing.T, extraEnv []string, args ...string) []byte {
	t.Helper()
	root := moduleRoot(t)

	var stdout, stderr bytes.Buffer
	err := withRetry(func() error {
		// Reset per attempt, or a failed attempt's diagnostics would be
		// concatenated onto the output the caller parses.
		stdout.Reset()
		stderr.Reset()
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		cmd.Env = append(cmd.Environ(), extraEnv...)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		return cmd.Run()
	}, time.Sleep)
	if err != nil {
		t.Fatalf("go %s failed after %d attempts: %v\nstderr: %s",
			strings.Join(args, " "), toolchainAttempts, err, stderr.String())
	}
	return stdout.Bytes()
}

// TestWithRetrySucceedsAfterTransientFailure pins the behaviour the nested
// builds depend on: a command that fails twice and then succeeds must be
// reported as a success. Without this, the load flake in gum-qooz fails the
// whole security gate.
func TestWithRetrySucceedsAfterTransientFailure(t *testing.T) {
	calls := 0
	slept := 0
	err := withRetry(func() error {
		calls++
		if calls < 3 {
			return errors.New("transient load failure")
		}
		return nil
	}, func(time.Duration) { slept++ })
	if err != nil {
		t.Errorf("withRetry err = %v; want nil once an attempt succeeds", err)
	}
	if calls != 3 {
		t.Errorf("attempts = %d; want 3 (two failures then a success)", calls)
	}
	if slept != 2 {
		t.Errorf("backoff pauses = %d; want 2 (one after each failure, none after success)", slept)
	}
}

// TestWithRetryGivesUpOnPersistentFailure pins the other half: a deterministic
// failure — a real compile error or an actual CGo leak — must still fail, and
// must not be retried without bound.
func TestWithRetryGivesUpOnPersistentFailure(t *testing.T) {
	calls := 0
	slept := 0
	want := errors.New("compile error")
	err := withRetry(func() error {
		calls++
		return want
	}, func(time.Duration) { slept++ })
	if !errors.Is(err, want) {
		t.Errorf("withRetry err = %v; want the attempt's own error surfaced", err)
	}
	if calls != toolchainAttempts {
		t.Errorf("attempts = %d; want exactly %d", calls, toolchainAttempts)
	}
	if slept != toolchainAttempts-1 {
		t.Errorf("backoff pauses = %d; want %d (none after the final attempt)", slept, toolchainAttempts-1)
	}
}

// TestWithRetryFirstAttemptSucceedsWithoutSleeping pins the common path: when
// the toolchain is not under load, nothing is retried and no backoff is paid.
func TestWithRetryFirstAttemptSucceedsWithoutSleeping(t *testing.T) {
	calls := 0
	slept := 0
	if err := withRetry(func() error { calls++; return nil }, func(time.Duration) { slept++ }); err != nil {
		t.Errorf("withRetry err = %v; want nil", err)
	}
	if calls != 1 || slept != 0 {
		t.Errorf("attempts = %d, pauses = %d; want 1 and 0", calls, slept)
	}
}

// TestRunGoReturnsStdoutOnly verifies runGo hands back the command's stdout
// and not its stderr — the `go list` callers parse the return value line by
// line, so a stderr leak would be read as a package name.
func TestRunGoReturnsStdoutOnly(t *testing.T) {
	out := runGo(t, nil, "list", "-f", "{{.ImportPath}}", "./internal/securityscan")
	got := strings.TrimSpace(string(out))
	if got != "github.com/ehmo/gum/internal/securityscan" {
		t.Errorf("runGo stdout = %q; want just the import path", got)
	}
}
