package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenRegistryMkdirFailureSurfacesWrap pins the
// `os.MkdirAll err → "gum plugin: mkdir profile dir:" wrap` arm.
// Reached when the profile dir's parent is a regular file rather
// than a directory (ENOTDIR). The wrap label "gum plugin: mkdir
// profile dir:" is what operators grep for to triage filesystem
// issues separately from registry/manifest-parsing failures
// downstream — without the explicit wrap the same surface would
// appear as a generic os.PathError that's harder to attribute.
func TestOpenRegistryMkdirFailureSurfacesWrap(t *testing.T) {
	tmp := t.TempDir()
	// Plant a regular file at the parent path → MkdirAll on a child
	// path returns ENOTDIR.
	blocker := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatalf("plant blocker file: %v", err)
	}
	profileDir := filepath.Join(blocker, "profile")

	_, err := openRegistry(profileDir, nil)
	if err == nil {
		t.Fatal("openRegistry(blocker)=nil err; want mkdir wrap")
	}
	if !strings.Contains(err.Error(), "gum plugin: mkdir profile dir") {
		t.Errorf("err=%q; want 'gum plugin: mkdir profile dir' wrap", err)
	}
}
