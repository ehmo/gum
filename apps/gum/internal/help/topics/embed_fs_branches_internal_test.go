package topics

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// errFS returns a fixed error from Open, so the fs.ReadDir and fs.ReadFile
// failure arms are reachable. The embedded FS never fails either call.
type errFS struct {
	err  error
	only string // when set, only this name fails; other names come from base
	base fs.FS
}

func (e errFS) Open(name string) (fs.File, error) {
	if e.only == "" || e.only == name {
		return nil, e.err
	}
	return e.base.Open(name)
}

var errBoom = errors.New("boom")

func TestValidateSizesReadDirError(t *testing.T) {
	err := validateSizes(errFS{err: errBoom})
	if !errors.Is(err, errBoom) {
		t.Fatalf("validateSizes(errFS) = %v, want errBoom", err)
	}
}

func TestValidateSizesReadFileError(t *testing.T) {
	base := fstest.MapFS{"broken.md": &fstest.MapFile{Data: []byte("x")}}
	err := validateSizes(errFS{err: errBoom, only: "broken.md", base: base})
	if !errors.Is(err, errBoom) {
		t.Fatalf("validateSizes(unreadable file) = %v, want errBoom", err)
	}
}

func TestValidateSizesSkipsDirsAndNonMarkdown(t *testing.T) {
	fsys := fstest.MapFS{
		"sub/nested.md": &fstest.MapFile{Data: []byte(strings.Repeat("x", MaxTopicBytes+1))},
		"notes.txt":     &fstest.MapFile{Data: []byte(strings.Repeat("x", MaxTopicBytes+1))},
		"ok.md":         &fstest.MapFile{Data: []byte("fits")},
	}
	if err := validateSizes(fsys); err != nil {
		t.Fatalf("validateSizes(dirs and non-markdown) = %v, want nil", err)
	}
}

func TestValidateSizesRejectsOversizedTopic(t *testing.T) {
	fsys := fstest.MapFS{
		"huge.md": &fstest.MapFile{Data: []byte(strings.Repeat("x", MaxTopicBytes+1))},
	}
	err := validateSizes(fsys)
	var tooLarge *ErrTopicTooLarge
	if !errors.As(err, &tooLarge) {
		t.Fatalf("validateSizes(oversized) = %v, want *ErrTopicTooLarge", err)
	}
	if tooLarge.Topic != "huge" {
		t.Errorf("Topic = %q, want %q", tooLarge.Topic, "huge")
	}
	if tooLarge.Size != MaxTopicBytes+1 {
		t.Errorf("Size = %d, want %d", tooLarge.Size, MaxTopicBytes+1)
	}
	if !strings.Contains(tooLarge.Error(), "HELP_TOPIC_TOO_LARGE") {
		t.Errorf("Error() = %q, want the HELP_TOPIC_TOO_LARGE code", tooLarge.Error())
	}
}

func TestNamesReadDirError(t *testing.T) {
	if got := names(errFS{err: errBoom}); got != nil {
		t.Fatalf("names(errFS) = %v, want nil", got)
	}
}

func TestNamesSkipsDirsAndNonMarkdown(t *testing.T) {
	fsys := fstest.MapFS{
		"sub/nested.md": &fstest.MapFile{Data: []byte("nested")},
		"notes.txt":     &fstest.MapFile{Data: []byte("text")},
		"gmail.md":      &fstest.MapFile{Data: []byte("body")},
	}
	got := names(fsys)
	if len(got) != 1 || got[0] != "gmail" {
		t.Fatalf("names() = %v, want [gmail]", got)
	}
}
