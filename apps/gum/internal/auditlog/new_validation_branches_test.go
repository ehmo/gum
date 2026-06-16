package auditlog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/auditlog"
)

// TestNewRejectsEmptyProfileDir pins the `profileDir == "" → error` guard.
// The Writer roots all of its paths off profileDir; an empty string would
// silently degrade to writing into the current working directory (the
// "audit.jsonl" path would resolve relative). Spec §11 requires an
// explicit profile-rooted audit log, so an empty profileDir MUST surface
// as a hard error at construction time rather than picking a surprising
// default.
func TestNewRejectsEmptyProfileDir(t *testing.T) {
	w, err := auditlog.New("")
	if err == nil {
		_ = w.Close()
		t.Fatal("auditlog.New(\"\") returned nil err; want empty-profileDir guard")
	}
	if !strings.Contains(err.Error(), "empty profileDir") {
		t.Errorf("err=%v; want 'empty profileDir' substring", err)
	}
}

// TestNewMkdirAllFailureSurfacesAsError pins the `os.MkdirAll err →
// return wrapped err` arm. Plant a regular file at the parent path so
// MkdirAll fails with ENOTDIR when it tries to mkdir profileDir as a
// child of a non-directory. Spec §11: construction failures must
// propagate, not silently no-op, because a missing audit log would
// erode the operator's only visibility into destructive calls.
func TestNewMkdirAllFailureSurfacesAsError(t *testing.T) {
	tmp := t.TempDir()
	// Plant a regular file where the profile-parent directory would be
	// created — MkdirAll on a path beneath a file returns ENOTDIR.
	parentAsFile := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(parentAsFile, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("plant file: %v", err)
	}
	profileDir := filepath.Join(parentAsFile, "audit")

	w, err := auditlog.New(profileDir)
	if err == nil {
		_ = w.Close()
		t.Fatal("auditlog.New(non-dir-parent) returned nil err; want MkdirAll surface")
	}
	if !strings.Contains(err.Error(), "auditlog: mkdir") {
		t.Errorf("err=%v; want 'auditlog: mkdir' wrap (got bare or mis-wrapped error)", err)
	}
}
