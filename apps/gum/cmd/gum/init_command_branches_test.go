package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestInitInvalidTargetSurfacesResolveTargetError pins the
// `initpkg.ResolveTarget err → return terr` arm. A user typing
// `gum init --target=unknown-host` MUST get a clean error from
// ResolveTarget rather than crashing later in PlanPatch with an
// opaque file-not-found surface. The wrap path preserves the
// original error message so operators see "unknown target ..." not
// "init: ..." (the latter is reserved for filesystem-resolution
// failures earlier in the command).
func TestInitInvalidTargetSurfacesResolveTargetError(t *testing.T) {
	root := newRootCmd()
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"init", "--target=not-a-real-host"})
	err = root.Execute()
	if err == nil {
		t.Fatalf("init --target=not-a-real-host succeeded; want ResolveTarget err\nstdout:\n%s", out.String())
	}
	// Must NOT carry the "init:" prefix (reserved for UserHomeDir /
	// Getwd wraps); ResolveTarget's err passes through verbatim.
	if strings.HasPrefix(err.Error(), "init: ") {
		t.Errorf("err=%q; should not carry 'init:' prefix (that's reserved for fs-resolution wraps)", err)
	}
}

// TestInitDeclinedPromptSurfacesPatchDeclinedError pins the
// `!promptConfirm → return "init: patch declined by user"` arm. When
// the operator types anything other than "y" / "yes" at the diff-
// and-prompt step, gum MUST abort with a clean error string the
// shell can grep for in CI — and crucially MUST NOT have already
// written GUM.md or patched settings.json. This is the spec §12.2
// "never silently patch" contract.
func TestInitDeclinedPromptSurfacesPatchDeclinedError(t *testing.T) {
	root := newRootCmd()
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	// Pipe "n\n" as stdin — promptConfirm reads one line, anything
	// other than y/yes is a decline.
	root.SetIn(strings.NewReader("n\n"))
	root.SetArgs([]string{"init"}) // no --yes, so the prompt fires

	err = root.Execute()
	if err == nil {
		t.Fatal("init (declined)=nil err; want 'patch declined by user'")
	}
	if !strings.Contains(err.Error(), "patch declined by user") {
		t.Errorf("err=%q; want 'patch declined by user' surface", err)
	}
}
