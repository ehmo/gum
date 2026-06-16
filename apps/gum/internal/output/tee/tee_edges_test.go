package tee_test

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/output/tee"
)

// TestWriteJSONMarshalsAndPersists covers tee.WriteJSON (was 0%): the
// payload is JSON-marshaled and written through Write to ArtifactPath.
func TestWriteJSONMarshalsAndPersists(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	hash := strings.Repeat("c", 64)

	payload := map[string]any{"k": "v", "n": 7}
	dst, err := tee.WriteJSON(profileDir, day, "op.json", hash, payload)
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if dst != tee.ArtifactPath(profileDir, day, "op.json", hash) {
		t.Errorf("dst=%q, want canonical ArtifactPath", dst)
	}

	got, err := tee.Read(dst)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(got, &round); err != nil {
		t.Fatalf("payload not JSON: %v\nbody=%s", err, got)
	}
	if round["k"] != "v" {
		t.Errorf("k=%v, want v", round["k"])
	}
}

// TestWriteJSONMarshalError covers the marshal-error branch (channels are
// unmarshalable in encoding/json).
func TestWriteJSONMarshalError(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)

	_, err := tee.WriteJSON(profileDir, day, "op.bad", strings.Repeat("d", 64), make(chan int))
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
	if !strings.Contains(err.Error(), "marshal payload") {
		t.Errorf("err=%v; want mention of marshal payload", err)
	}
}

// TestSecretCorruptError_Format covers the (*SecretCorruptError).Error
// method (was 0%): the formatted string must include both Path and Reason.
func TestSecretCorruptError_Format(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Write a corrupt secret (empty file).
	if err := os.WriteFile(tee.SecretPath(profileDir), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := tee.LoadOrCreateSecret(profileDir)
	if err == nil {
		t.Fatal("expected error from corrupt secret, got nil")
	}
	var sce *tee.SecretCorruptError
	if !errors.As(err, &sce) {
		t.Fatalf("expected *SecretCorruptError, got %T: %v", err, err)
	}
	msg := sce.Error()
	if !strings.Contains(msg, tee.SecretPath(profileDir)) {
		t.Errorf("Error()=%q missing path", msg)
	}
	if !strings.Contains(msg, "empty file") {
		t.Errorf("Error()=%q missing reason", msg)
	}
	if sce.Suggestion() == "" {
		t.Error("Suggestion() empty")
	}
}

// TestWriteMkdirAllError covers the os.MkdirAll error branch of Write by
// pre-creating a regular file where the dir should go.
func TestWriteMkdirAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file-as-dir collision behaves differently on Windows")
	}
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	// Pre-create a *file* named "tee" so MkdirAll(profileDir/tee/...) fails.
	if err := os.WriteFile(filepath.Join(profileDir, "tee"), []byte("conflict"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := tee.Write(profileDir, day, "op", strings.Repeat("e", 64), []byte("x"))
	if err == nil {
		t.Fatal("expected mkdir error, got nil")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("err=%v; want mkdir mention", err)
	}
}

// TestLoadOrCreateSecretMkdirError covers the profileDir-creation error
// branch (line ~86) when LoadOrCreateSecret can't create the dir. We
// need: (1) the secret file path missing so loadSecret returns
// fs.ErrNotExist (control flow falls into the mkdir branch), AND
// (2) the parent dir read-only so MkdirAll fails inside the branch.
func TestLoadOrCreateSecretMkdirError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("read-only-dir test requires non-root unix")
	}
	dir := t.TempDir()
	parent := filepath.Join(dir, "ro-parent")
	if err := os.MkdirAll(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(parent, 0o700) }()
	profileDir := filepath.Join(parent, "new-profile")
	_, err := tee.LoadOrCreateSecret(profileDir)
	if err == nil {
		t.Fatal("expected mkdir error, got nil")
	}
	if !strings.Contains(err.Error(), "create profile dir") {
		t.Errorf("err=%v; want create profile dir mention", err)
	}
}

// TestReadNonGzipFile covers the gzip.NewReader error branch of Read
// (file exists but is not gzip-compressed).
func TestReadNonGzipFile(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "not-gzip.json.gz")
	if err := os.WriteFile(plain, []byte("this is not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := tee.Read(plain)
	if err == nil {
		t.Fatal("expected gzip reader error, got nil")
	}
	if !strings.Contains(err.Error(), "gzip") {
		t.Errorf("err=%v; want gzip mention", err)
	}
}

// TestFindArtifactEmptyHashOrZeroDays covers the early-return branches
// of FindArtifact.
func TestFindArtifactEmptyHashOrZeroDays(t *testing.T) {
	dir := t.TempDir()
	got, ok, err := tee.FindArtifact(dir, "", 5)
	if err != nil || ok || got != "" {
		t.Errorf("empty hash: got=%q ok=%v err=%v", got, ok, err)
	}
	got, ok, err = tee.FindArtifact(dir, "abc", 0)
	if err != nil || ok || got != "" {
		t.Errorf("zero days: got=%q ok=%v err=%v", got, ok, err)
	}
}

// TestFindArtifactNoTeeRoot covers the early-return when the tee root
// directory doesn't exist yet.
func TestFindArtifactNoTeeRoot(t *testing.T) {
	dir := t.TempDir()
	got, ok, err := tee.FindArtifact(dir, strings.Repeat("a", 64), 5)
	if err != nil {
		t.Errorf("expected no error for missing tee dir, got %v", err)
	}
	if ok || got != "" {
		t.Errorf("expected (\"\", false), got (%q, %v)", got, ok)
	}
}

// TestWriteRenameOverNonEmptyDir covers the os.Rename error branch by
// pre-creating a non-empty directory at the destination path. On Unix,
// renaming a file over a non-empty directory fails with ENOTEMPTY.
func TestWriteRenameOverNonEmptyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename-over-dir semantics differ on Windows")
	}
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	hash := strings.Repeat("g", 64)

	// Pre-create the destination as a non-empty directory.
	dstPath := tee.ArtifactPath(profileDir, day, "op.rename", hash)
	if err := os.MkdirAll(dstPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstPath, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := tee.Write(profileDir, day, "op.rename", hash, []byte(`{"k":"v"}`))
	if err == nil {
		t.Fatal("expected rename error, got nil")
	}
	if !strings.Contains(err.Error(), "rename") {
		t.Errorf("err=%v; want rename mention", err)
	}
}

// TestWriteCreateTempError covers the CreateTemp error branch of Write
// (line ~67) by pre-creating the artifact dir read-only so MkdirAll
// succeeds (dir exists) but CreateTemp inside it fails with EACCES.
func TestWriteCreateTempError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("read-only-dir test requires non-root unix")
	}
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	hash := strings.Repeat("h", 64)

	// Pre-create the artifact dir (writable), then chmod the leaf
	// read-only so MkdirAll is a no-op but CreateTemp inside fails.
	artDir := tee.ArtifactDir(profileDir, day, "op.ro")
	if err := os.MkdirAll(artDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(artDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(artDir, 0o700) }() // restore for cleanup
	_, err := tee.Write(profileDir, day, "op.ro", hash, []byte(`{}`))
	if err == nil {
		t.Fatal("expected create-temp error, got nil")
	}
	if !strings.Contains(err.Error(), "create temp artifact") {
		t.Errorf("err=%v; want create temp artifact mention", err)
	}
}

// TestLoadOrCreateSecretCreateTempError covers the CreateTemp error
// branch of LoadOrCreateSecret (line ~95) when the profile dir exists
// but is read-only.
func TestLoadOrCreateSecretCreateTempError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("read-only-dir test requires non-root unix")
	}
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(profileDir, 0o700) }()
	_, err := tee.LoadOrCreateSecret(profileDir)
	if err == nil {
		t.Fatal("expected create-temp error, got nil")
	}
	if !strings.Contains(err.Error(), "create temp secret") {
		t.Errorf("err=%v; want create temp secret mention", err)
	}
}

// TestReadPermissionDeniedSurfacesError covers Read's non-ErrNotExist
// error branch (line 102) by opening a file with no read permission.
func TestReadPermissionDeniedSurfacesError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("chmod-based read denial requires non-root unix")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "unreadable.json.gz")
	if err := os.WriteFile(path, []byte("anything"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(path, 0o600) }() // restore for cleanup
	_, err := tee.Read(path)
	if err == nil {
		t.Fatal("expected permission error, got nil")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Errorf("err mapped to ErrNotExist; want a non-NotExist error: %v", err)
	}
	if !strings.Contains(err.Error(), "open artifact") {
		t.Errorf("err=%v; want open artifact mention", err)
	}
}

// TestFindArtifactRootIsFile covers the os.ReadDir(root) error branch
// (line ~152): tee/ exists but is a regular file, not a directory.
func TestFindArtifactRootIsFile(t *testing.T) {
	dir := t.TempDir()
	teeRoot := filepath.Join(dir, "tee")
	if err := os.WriteFile(teeRoot, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := tee.FindArtifact(dir, strings.Repeat("a", 64), 5)
	if err == nil {
		t.Fatal("expected error from ReadDir on a file, got nil")
	}
	if !strings.Contains(err.Error(), "read tee root") {
		t.Errorf("err=%v; want read tee root mention", err)
	}
}

// TestFindArtifactSkipsNonDirOpDayEntries covers the skip-non-dir
// branches (line ~158: not a date dir; line ~179: op entry not a dir).
func TestFindArtifactSkipsNonDirOpDayEntries(t *testing.T) {
	dir := t.TempDir()
	teeRoot := filepath.Join(dir, "tee")
	if err := os.MkdirAll(teeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	// Plant a non-dir entry (regular file) under tee/.
	if err := os.WriteFile(filepath.Join(teeRoot, "random.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Plant a malformed date dir name (only digits, wrong format).
	if err := os.MkdirAll(filepath.Join(teeRoot, "20260523"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Plant a proper date dir with a non-dir op entry.
	if err := os.MkdirAll(filepath.Join(teeRoot, "2026-05-23"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(teeRoot, "2026-05-23", "file-not-dir.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok, err := tee.FindArtifact(dir, strings.Repeat("z", 64), 5)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if ok || got != "" {
		t.Errorf("expected no match, got (%q, %v)", got, ok)
	}
}

// TestComputeHashJCSError covers the jcs.Marshal error branch of
// ComputeHash by passing an unsupported value (a channel) as Args.
func TestComputeHashJCSError(t *testing.T) {
	in := tee.HashInput{
		OpID:                   "op",
		VariantIDResolved:      "v",
		Args:                   make(chan int), // JCS can't encode this
		AuthSubjectFingerprint: "auth",
	}
	secret := []byte("0123456789abcdef0123456789abcdef")
	_, err := tee.ComputeHash(secret, in)
	if err == nil {
		t.Fatal("expected JCS marshal error, got nil")
	}
}

// TestReadTruncatedGzipBody covers the buf.ReadFrom error branch in
// Read (line ~111). A truncated gzip stream passes header validation
// but fails when the body is consumed.
func TestReadTruncatedGzipBody(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	hash := strings.Repeat("t", 64)
	dst, err := tee.Write(profileDir, day, "op.trunc", hash, []byte(`{"k":"v"}`))
	if err != nil {
		t.Fatal(err)
	}
	// Truncate the file mid-stream (keep enough for the gzip header,
	// drop the trailer and most of the deflated body).
	full, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) < 12 {
		t.Skipf("gzip artifact too small to truncate meaningfully: %d bytes", len(full))
	}
	if err := os.WriteFile(dst, full[:10], 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = tee.Read(dst)
	if err == nil {
		t.Fatal("expected read error from truncated body, got nil")
	}
	if !strings.Contains(err.Error(), "gzip") {
		t.Errorf("err=%v; want gzip-related mention", err)
	}
}

// TestFindArtifactStatTeeRootError covers the os.Stat(root) error
// branch (line ~146) where the path is unreachable due to a
// non-traversable parent dir.
func TestFindArtifactStatTeeRootError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("requires non-root unix for traverse-denial")
	}
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Remove traverse permission so Stat(profileDir/tee) fails with EACCES.
	if err := os.Chmod(profileDir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(profileDir, 0o700) }()
	_, _, err := tee.FindArtifact(profileDir, strings.Repeat("a", 64), 5)
	if err == nil {
		t.Fatal("expected stat error, got nil")
	}
	if !strings.Contains(err.Error(), "stat tee root") {
		t.Errorf("err=%v; want stat tee root mention", err)
	}
}

// TestFindArtifactDayDirReadDirError covers the ReadDir-on-dayDir error
// branch (line ~175) by making the dayDir unreadable.
func TestFindArtifactDayDirReadDirError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("read-only-dir test requires non-root unix")
	}
	dir := t.TempDir()
	teeRoot := filepath.Join(dir, "tee")
	dayDir := filepath.Join(teeRoot, "2026-05-23")
	if err := os.MkdirAll(dayDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dayDir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dayDir, 0o700) }()
	// FindArtifact should swallow the per-day ReadDir error and return
	// (no match, no error).
	got, ok, err := tee.FindArtifact(dir, strings.Repeat("z", 64), 5)
	if err != nil {
		t.Errorf("expected no error (per-day errors are swallowed), got %v", err)
	}
	if ok || got != "" {
		t.Errorf("expected no match, got (%q, %v)", got, ok)
	}
}

// TestWriteGoldenRoundTripGzipReadable confirms the file Write produces
// is gzip-decompressable, exercising Read's success branch fully.
func TestWriteGoldenRoundTripGzipReadable(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 23, 1, 0, 0, 0, time.UTC)
	payload := []byte(`{"x":1}`)
	dst, err := tee.Write(profileDir, day, "op.gz", strings.Repeat("f", 64), payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer func() { _ = gz.Close() }()
	body, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != string(payload) {
		t.Errorf("payload mismatch: %s", body)
	}
}
