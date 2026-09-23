package dispatch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// keyFileIn returns the signing-key path loadOrCreateSigningKey derives from
// an XDG_DATA_HOME root.
func keyFileIn(dataHome string) string {
	return filepath.Join(dataHome, "gum", "confirmation-signing.key")
}

// TestSigningKeyPathHonorsXDGDataHome pins both branches of the base lookup.
func TestSigningKeyPathHonorsXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg")
	got, err := signingKeyPath()
	if err != nil {
		t.Fatalf("signingKeyPath: %v", err)
	}
	if want := filepath.Join("/xdg", "gum", "confirmation-signing.key"); got != want {
		t.Errorf("signingKeyPath = %q; want %q", got, want)
	}

	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/someone")
	got, err = signingKeyPath()
	if err != nil {
		t.Fatalf("signingKeyPath with no XDG_DATA_HOME: %v", err)
	}
	want := filepath.Join("/home/someone", ".local", "share", "gum", "confirmation-signing.key")
	if got != want {
		t.Errorf("signingKeyPath = %q; want the ~/.local/share fallback %q", got, want)
	}
}

// TestSigningKeyPathWithoutAHome covers the error return. With no XDG_DATA_HOME
// and no HOME there is nowhere to persist the key, and the caller must fall
// back to a per-process key rather than guess a path.
func TestSigningKeyPathWithoutAHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")
	if _, err := signingKeyPath(); err == nil {
		t.Fatal("signingKeyPath with no HOME returned no error")
	}
	if key, ok := loadOrCreateSigningKey(); ok {
		t.Errorf("loadOrCreateSigningKey with no HOME reported ok (key %x)", key[:4])
	}
}

// TestLoadOrCreateSigningKeyRoundTrips covers the create-then-read path: the
// first call writes a 0600 key file and the second reads the same bytes back,
// which is what makes a cross-process `gum destructive --token` confirmation
// work at all.
func TestLoadOrCreateSigningKeyRoundTrips(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	first, ok := loadOrCreateSigningKey()
	if !ok {
		t.Fatal("first loadOrCreateSigningKey reported failure")
	}
	info, err := os.Stat(keyFileIn(dataHome))
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file mode = %o; want 600", perm)
	}

	second, ok := loadOrCreateSigningKey()
	if !ok {
		t.Fatal("second loadOrCreateSigningKey reported failure")
	}
	if first != second {
		t.Error("the persisted key was not read back on the second call")
	}
}

// TestLoadOrCreateSigningKeyRefusesASymlinkedKeyFile is the regression test for
// the O_NOFOLLOW bypass. The fast-path read refuses a symlink, but the adopt
// path that runs after the O_EXCL create loses used a plain os.ReadFile, which
// follows one. An attacker with write access to the data dir could plant a
// symlink to a 32-byte file they control and have gum sign confirmation tokens
// with a key they know (review gum-t8x1).
func TestLoadOrCreateSigningKeyRefusesASymlinkedKeyFile(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	if err := os.MkdirAll(filepath.Join(dataHome, "gum"), 0o700); err != nil {
		t.Fatalf("mkdir key dir: %v", err)
	}
	var planted [32]byte
	for i := range planted {
		planted[i] = 0xAB
	}
	attacker := filepath.Join(dataHome, "attacker.key")
	if err := os.WriteFile(attacker, planted[:], 0o600); err != nil {
		t.Fatalf("write attacker key: %v", err)
	}
	if err := os.Symlink(attacker, keyFileIn(dataHome)); err != nil {
		t.Fatalf("plant symlink: %v", err)
	}

	key, ok := loadOrCreateSigningKey()
	if ok && key == planted {
		t.Fatal("loadOrCreateSigningKey adopted the symlink target as the signing key")
	}
	if ok {
		t.Errorf("loadOrCreateSigningKey reported ok for a symlinked key file (key %x)", key[:4])
	}
}

// TestLoadOrCreateSigningKeyAdoptsTheWinnersKey covers the lost-race branch: a
// real 32-byte file appears at the path after the fast-path read, so the O_EXCL
// create fails and the loser must adopt the winner's bytes instead of falling
// back to an ephemeral key both processes would disagree on.
func TestLoadOrCreateSigningKeyAdoptsTheWinnersKey(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	if err := os.MkdirAll(filepath.Join(dataHome, "gum"), 0o700); err != nil {
		t.Fatalf("mkdir key dir: %v", err)
	}
	var winner [32]byte
	for i := range winner {
		winner[i] = byte(i)
	}
	if err := os.WriteFile(keyFileIn(dataHome), winner[:], 0o600); err != nil {
		t.Fatalf("write winner key: %v", err)
	}

	got, ok := loadOrCreateSigningKey()
	if !ok {
		t.Fatal("loadOrCreateSigningKey did not adopt an existing key")
	}
	if got != winner {
		t.Errorf("adopted key = %x...; want the on-disk key", got[:4])
	}
}

// TestLoadOrCreateSigningKeyRejectsAWrongSizedFile covers the give-up return: a
// truncated key file is not a key, and signing with a short one would make
// every token unverifiable by the next process.
func TestLoadOrCreateSigningKeyRejectsAWrongSizedFile(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	if err := os.MkdirAll(filepath.Join(dataHome, "gum"), 0o700); err != nil {
		t.Fatalf("mkdir key dir: %v", err)
	}
	if err := os.WriteFile(keyFileIn(dataHome), []byte("short"), 0o600); err != nil {
		t.Fatalf("write short key: %v", err)
	}

	if _, ok := loadOrCreateSigningKey(); ok {
		t.Error("loadOrCreateSigningKey accepted a 5-byte key file")
	}
}

// TestLoadOrCreateSigningKeyCannotCreateTheDirectory covers the MkdirAll error
// arm: a regular file sits where the data directory must go.
func TestLoadOrCreateSigningKeyCannotCreateTheDirectory(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(blocker, "data"))

	if _, ok := loadOrCreateSigningKey(); ok {
		t.Error("loadOrCreateSigningKey reported ok with an uncreatable data dir")
	}
}

// TestDurableReplaySeenMarksAndDetects covers the happy path and the replay
// detection that makes a confirmation token single-use across processes.
func TestDurableReplaySeenMarksAndDetects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	expiry := time.Now().Add(time.Hour)

	seen, err := durableReplaySeen(dir, "aabbcc", expiry)
	if err != nil {
		t.Fatalf("first durableReplaySeen: %v", err)
	}
	if seen {
		t.Error("first use reported as a replay")
	}

	seen, err = durableReplaySeen(dir, "aabbcc", expiry)
	if err != nil {
		t.Fatalf("second durableReplaySeen: %v", err)
	}
	if !seen {
		t.Error("second use was not reported as a replay")
	}
}

// TestDurableReplaySeenErrorArms covers the two failure returns: no store
// directory configured, and a store directory that cannot be created.
func TestDurableReplaySeenErrorArms(t *testing.T) {
	t.Parallel()
	if _, err := durableReplaySeen("", "aabbcc", time.Now()); err == nil {
		t.Error("durableReplaySeen with an empty dir returned no error")
	}

	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	_, err := durableReplaySeen(filepath.Join(blocker, "profile"), "aabbcc", time.Now())
	if err == nil {
		t.Fatal("durableReplaySeen with an uncreatable store returned no error")
	}
	if !strings.Contains(err.Error(), "create replay store") {
		t.Errorf("err = %q; want the create-store wrap", err)
	}

	// A marker path that cannot be created for a reason other than "it already
	// exists" must surface, not be read as a replay: treating an I/O failure as
	// "already used" would reject a caller's first legitimate confirmation.
	_, err = durableReplaySeen(t.TempDir(), filepath.Join("missing", "marker"), time.Now())
	if err == nil {
		t.Fatal("durableReplaySeen with an uncreatable marker returned no error")
	}
	if !strings.Contains(err.Error(), "create replay marker") {
		t.Errorf("err = %q; want the create-marker wrap", err)
	}
}

// TestSweepExpiredReplayMarkers covers every arm of the sweep: an unreadable
// store, a subdirectory, an unreadable entry, an unparseable marker, an expired
// marker, and a live marker that must survive.
func TestSweepExpiredReplayMarkers(t *testing.T) {
	t.Parallel()

	// An absent store directory is a no-op, not a panic.
	sweepExpiredReplayMarkers(filepath.Join(t.TempDir(), "nope"))

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	// A dangling symlink is listed by ReadDir but fails ReadFile, so the sweep
	// must skip it instead of aborting the pass.
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "dangling")); err != nil {
		t.Fatalf("plant dangling symlink: %v", err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("garbage", "not-a-number")
	write("expired", strconv.FormatInt(time.Now().Add(-time.Hour).UnixNano(), 10))
	write("live", strconv.FormatInt(time.Now().Add(time.Hour).UnixNano(), 10))

	sweepExpiredReplayMarkers(dir)

	for _, gone := range []string{"garbage", "expired"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("marker %q survived the sweep", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "live")); err != nil {
		t.Errorf("live marker was swept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "subdir")); err != nil {
		t.Errorf("subdirectory was swept: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "dangling")); err != nil {
		t.Errorf("dangling symlink was swept: %v", err)
	}
}
