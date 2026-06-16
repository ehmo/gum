package tee_test

import (
	"compress/gzip"
	"encoding/hex"
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

// TestSecretGenerateOnFirstAccess covers lifecycle point 1: when tee.secret
// is absent, LoadOrCreateSecret generates exactly 32 random bytes and
// persists them as 64 lowercase hex chars in a mode-600 file inside a
// mode-700 profile dir.
func TestSecretGenerateOnFirstAccess(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")

	key, err := tee.LoadOrCreateSecret(profileDir)
	if err != nil {
		t.Fatalf("LoadOrCreateSecret: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d; want 32", len(key))
	}
	path := tee.SecretPath(profileDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := strings.TrimRight(string(raw), "\n")
	if len(text) != 64 {
		t.Errorf("encoded hex length = %d; want 64", len(text))
	}
	if text != strings.ToLower(text) {
		t.Errorf("encoded hex has uppercase chars; want all lowercase")
	}
	decoded, err := hex.DecodeString(text)
	if err != nil {
		t.Errorf("hex decode: %v", err)
	}
	if string(decoded) != string(key) {
		t.Errorf("on-disk hex does not decode to returned key")
	}

	// Skip permission checks on Windows where Unix mode bits aren't enforced.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("tee.secret mode = %v; want 0600", got)
		}
		dinfo, err := os.Stat(profileDir)
		if err != nil {
			t.Fatal(err)
		}
		if got := dinfo.Mode().Perm(); got != 0o700 {
			t.Errorf("profile dir mode = %v; want 0700", got)
		}
	}
}

// TestTeeSecretStability covers lifecycle point 3 (algorithm stability):
// repeated LoadOrCreateSecret calls on the same profile dir return the same
// 32-byte key — no silent regeneration.
func TestTeeSecretStability(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")

	k1, err := tee.LoadOrCreateSecret(profileDir)
	if err != nil {
		t.Fatalf("first LoadOrCreateSecret: %v", err)
	}
	k2, err := tee.LoadOrCreateSecret(profileDir)
	if err != nil {
		t.Fatalf("second LoadOrCreateSecret: %v", err)
	}
	if string(k1) != string(k2) {
		t.Errorf("tee.secret unstable across reads: k1=%x k2=%x", k1, k2)
	}
	k3, err := tee.LoadSecret(profileDir)
	if err != nil {
		t.Fatalf("LoadSecret: %v", err)
	}
	if string(k1) != string(k3) {
		t.Errorf("LoadSecret returned different key than LoadOrCreateSecret")
	}
}

// TestSecretCorruptDetection covers lifecycle point 2 — TEE_SECRET_CORRUPT
// is raised for every defined corruption mode: empty file, wrong length,
// non-hex chars, uppercase hex chars. Silent regeneration is prohibited.
func TestSecretCorruptDetection(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"short", "deadbeef"},
		{"long", strings.Repeat("ab", 64)}, // 128 chars
		{"non-hex", strings.Repeat("zz", 32)},
		{"uppercase", strings.Repeat("AB", 32)},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			profileDir := filepath.Join(dir, "default")
			if err := os.MkdirAll(profileDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tee.SecretPath(profileDir), []byte(c.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := tee.LoadOrCreateSecret(profileDir)
			if err == nil {
				t.Fatal("expected error for corrupt secret; got nil — silent regeneration is prohibited (spec §9 lifecycle point 2)")
			}
			if !errors.Is(err, tee.ErrSecretCorrupt) {
				t.Errorf("err = %v; want ErrSecretCorrupt", err)
			}
			var sce *tee.SecretCorruptError
			if !errors.As(err, &sce) {
				t.Fatalf("err = %T; want *SecretCorruptError", err)
			}
			if sce.Path != tee.SecretPath(profileDir) {
				t.Errorf("SecretCorruptError.Path = %q; want %q", sce.Path, tee.SecretPath(profileDir))
			}
			if sce.Suggestion() == "" {
				t.Errorf("SecretCorruptError.Suggestion is empty; spec §9 requires a remediation hint")
			}
		})
	}
}

// TestHashDeterministic covers spec §9 lifecycle point 3 algorithm stability:
// identical (op, variant, args, principal) inputs produce identical hashes
// regardless of key-order variation in args (JCS canonical form normalises
// key order).
func TestHashDeterministic(t *testing.T) {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}
	in := tee.HashInput{
		OpID:                   "gmail.users.messages.list",
		VariantIDResolved:      "gmail.users.messages.list.v1",
		Args:                   map[string]any{"userId": "me", "labelIds": []any{"INBOX"}, "maxResults": 10},
		AuthSubjectFingerprint: "sub-fingerprint-abc",
	}
	h1, err := tee.ComputeHash(secret, in)
	if err != nil {
		t.Fatalf("ComputeHash 1: %v", err)
	}
	// Same logical args, different Go-map iteration order would JCS to same bytes.
	in2 := in
	in2.Args = map[string]any{"maxResults": 10, "labelIds": []any{"INBOX"}, "userId": "me"}
	h2, err := tee.ComputeHash(secret, in2)
	if err != nil {
		t.Fatalf("ComputeHash 2: %v", err)
	}
	if h1 != h2 {
		t.Errorf("hash differs across map iteration order: h1=%s h2=%s — JCS not canonicalising", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("hex hash length = %d; want 64 (SHA-256)", len(h1))
	}
}

// TestPrincipalScopedRecoveryAndCache covers spec §9.0 line 1846: the same
// (op, variant, args, profile) but DIFFERENT credential subject MUST produce
// a different hash. This is the "cross-principal handles are not reusable"
// guarantee — a hash collision would leak one user's data to another in a
// multi-account profile reuse scenario.
func TestPrincipalScopedRecoveryAndCache(t *testing.T) {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = 0xAA
	}
	base := tee.HashInput{
		OpID:                   "drive.files.list",
		VariantIDResolved:      "drive.files.list.v1",
		Args:                   map[string]any{"q": "name contains 'foo'"},
		AuthSubjectFingerprint: "user-a-fp",
	}
	hA, err := tee.ComputeHash(secret, base)
	if err != nil {
		t.Fatalf("ComputeHash A: %v", err)
	}
	base.AuthSubjectFingerprint = "user-b-fp"
	hB, err := tee.ComputeHash(secret, base)
	if err != nil {
		t.Fatalf("ComputeHash B: %v", err)
	}
	if hA == hB {
		t.Errorf("hash did not change with auth subject: hA=hB=%s — recovery URIs would leak across principals", hA)
	}
	// Sanity: same subject → same hash.
	base.AuthSubjectFingerprint = "user-a-fp"
	hA2, err := tee.ComputeHash(secret, base)
	if err != nil {
		t.Fatalf("ComputeHash A2: %v", err)
	}
	if hA != hA2 {
		t.Errorf("hash unstable for same subject: %s vs %s", hA, hA2)
	}
}

// TestHashChangesPerComponent verifies that toggling any one of the four
// hash inputs (op_id, variant_id, args, auth_subject) changes the hash.
// Without this, two ops could collide on the same recovery URI.
func TestHashChangesPerComponent(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	base := tee.HashInput{
		OpID:                   "op.a",
		VariantIDResolved:      "op.a.v1",
		Args:                   map[string]any{"k": "v"},
		AuthSubjectFingerprint: "sub",
	}
	h0, err := tee.ComputeHash(secret, base)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		mut  func(*tee.HashInput)
	}{
		{"OpID", func(in *tee.HashInput) { in.OpID = "op.b" }},
		{"VariantID", func(in *tee.HashInput) { in.VariantIDResolved = "op.a.v2" }},
		{"Args", func(in *tee.HashInput) { in.Args = map[string]any{"k": "v2"} }},
		{"AuthSubject", func(in *tee.HashInput) { in.AuthSubjectFingerprint = "other" }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			in := base
			c.mut(&in)
			h, err := tee.ComputeHash(secret, in)
			if err != nil {
				t.Fatal(err)
			}
			if h == h0 {
				t.Errorf("toggling %s did not change hash: %s", c.name, h)
			}
		})
	}
}

// TestWriteAndReadArtifactRoundTrip covers the artifact write/read pair:
// gzip-compressed JSON at the spec-mandated path with mode-600 permissions.
func TestWriteAndReadArtifactRoundTrip(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}

	day := time.Date(2026, 5, 23, 4, 0, 0, 0, time.UTC)
	payload := []byte(`{"messages":[{"id":"abc"}]}`)
	hash := strings.Repeat("a", 64)

	dst, err := tee.Write(profileDir, day, "gmail.users.messages.list", hash, payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	expected := tee.ArtifactPath(profileDir, day, "gmail.users.messages.list", hash)
	if dst != expected {
		t.Errorf("Write returned %q; want %q", dst, expected)
	}

	// Path components must match spec §9.0 line 1846.
	want := filepath.Join(profileDir, "tee", "2026-05-23", "gmail.users.messages.list", hash+".json.gz")
	if dst != want {
		t.Errorf("artifact path = %q; want %q", dst, want)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("artifact mode = %v; want 0600", got)
		}
	}

	got, err := tee.Read(dst)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("payload round-trip differs:\n got: %s\nwant: %s", got, payload)
	}
}

// TestWriteIsGzipCompressed verifies the file on disk has a valid gzip
// header — otherwise external tools (e.g., `gunzip` for an operator's
// inspection) could not read the artifact.
func TestWriteIsGzipCompressed(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	payload := []byte(`{"hello":"world"}`)
	dst, err := tee.Write(profileDir, day, "op.test", strings.Repeat("b", 64), payload)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v — artifact is not gzip-compressed", err)
	}
	defer func() { _ = gz.Close() }()
	body, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != string(payload) {
		t.Errorf("body = %q; want %q", body, payload)
	}
}

// TestReadAbsentArtifactReturnsErrNotExist confirms callers can distinguish
// "expired/deleted" from other I/O errors — gum-uuh maps this to
// RESULT_ARTIFACT_EXPIRED.
func TestReadAbsentArtifactReturnsErrNotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := tee.Read(filepath.Join(dir, "does-not-exist.json.gz"))
	if err == nil {
		t.Fatal("expected error reading non-existent artifact; got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v; want os.ErrNotExist (so callers can map to RESULT_ARTIFACT_EXPIRED)", err)
	}
}

// TestFindArtifactFindsHashAcrossDayDirs writes artifacts on three different
// days and verifies FindArtifact locates each by hash without the caller
// having to know the day. This is the reverse-lookup invariant gum-uuh
// (gum://results/{hash}) depends on.
func TestFindArtifactFindsHashAcrossDayDirs(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	day1 := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	day3 := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	hA := strings.Repeat("1", 64)
	hB := strings.Repeat("2", 64)
	hC := strings.Repeat("3", 64)
	if _, err := tee.Write(profileDir, day1, "op.a", hA, []byte(`"a"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tee.Write(profileDir, day2, "op.b", hB, []byte(`"b"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tee.Write(profileDir, day3, "op.c", hC, []byte(`"c"`)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		hash string
		day  time.Time
		op   string
	}{
		{hA, day1, "op.a"},
		{hB, day2, "op.b"},
		{hC, day3, "op.c"},
	} {
		got, ok, err := tee.FindArtifact(profileDir, c.hash, 30)
		if err != nil {
			t.Fatalf("FindArtifact(%s): %v", c.hash, err)
		}
		if !ok {
			t.Errorf("hash %s: not found", c.hash)
			continue
		}
		want := tee.ArtifactPath(profileDir, c.day, c.op, c.hash)
		if got != want {
			t.Errorf("hash %s: path = %q; want %q", c.hash, got, want)
		}
	}
}

// TestFindArtifactReturnsFalseForMissing covers the gum://results/{hash}
// "expired/deleted" path — FindArtifact must report ok=false (no error) so
// the handler can map to RESULT_ARTIFACT_EXPIRED.
func TestFindArtifactReturnsFalseForMissing(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	got, ok, err := tee.FindArtifact(profileDir, strings.Repeat("9", 64), 7)
	if err != nil {
		t.Fatalf("FindArtifact: %v", err)
	}
	if ok {
		t.Errorf("unexpected hit at %q for non-existent hash", got)
	}
}

// TestFindArtifactRespectsMaxDays guarantees an older artifact outside the
// scan window is NOT returned. Retention pruning relies on this invariant.
func TestFindArtifactRespectsMaxDays(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	hOld := strings.Repeat("a", 64)
	hRecent := strings.Repeat("b", 64)
	if _, err := tee.Write(profileDir, old, "op.x", hOld, []byte(`"old"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tee.Write(profileDir, recent, "op.x", hRecent, []byte(`"recent"`)); err != nil {
		t.Fatal(err)
	}
	// maxDays=1 must find only the newest date directory (recent), not the old one.
	if _, ok, _ := tee.FindArtifact(profileDir, hRecent, 1); !ok {
		t.Errorf("recent hash not found within 1-day window")
	}
	if path, ok, _ := tee.FindArtifact(profileDir, hOld, 1); ok {
		t.Errorf("old hash unexpectedly found at %q within 1-day window — retention not honoured", path)
	}
	// maxDays=30 should now reach the old artifact.
	if _, ok, _ := tee.FindArtifact(profileDir, hOld, 30); !ok {
		t.Errorf("old hash not found within 30-day window")
	}
}

// TestArtifactDateIsUTC ensures the <YYYY-MM-DD> component is UTC-stable so
// the same logical "today" is consistent across timezones (operators must
// be able to compute artifact paths from auditing tools without TZ guesswork).
func TestArtifactDateIsUTC(t *testing.T) {
	utc := time.Date(2026, 5, 23, 23, 59, 0, 0, time.UTC)
	loc, err := time.LoadLocation("Asia/Tokyo") // +9: 2026-05-24 in local time
	if err != nil {
		t.Skip("Asia/Tokyo TZ unavailable")
	}
	local := utc.In(loc)
	if got := tee.ArtifactDate(utc); got != "2026-05-23" {
		t.Errorf("UTC date = %q; want 2026-05-23", got)
	}
	if got := tee.ArtifactDate(local); got != "2026-05-23" {
		t.Errorf("local date = %q; want 2026-05-23 (must convert to UTC)", got)
	}
}
