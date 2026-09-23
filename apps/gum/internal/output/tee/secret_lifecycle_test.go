package tee_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/output/tee"
)

// Matrix row 231, spec §9 tee.secret lifecycle point 5. The secret is keyed
// to profile identity alone: nothing else in the profile directory, and no
// catalog or index artifact, participates in its derivation or rewrites it.
//
// gum ships no embedding-model identity to rotate. Spec §5.7 pins
// embeddings.bin as a reserved placeholder, no embeddings.model file exists in
// the tree, and internal/embed is bm25-only-v1 with no external model call. So
// the row's independence claim is proved the only way it can be: the secret
// survives arbitrary churn in the files an index rebuild would touch, and two
// profiles never share one.
func TestTeeSecretEmbeddingIndependence(t *testing.T) {
	t.Run("a second load returns the same key and does not rewrite the file", func(t *testing.T) {
		dir := t.TempDir()
		first, err := tee.LoadOrCreateSecret(dir)
		if err != nil {
			t.Fatalf("LoadOrCreateSecret: %v", err)
		}
		before := statSecret(t, dir)

		second, err := tee.LoadOrCreateSecret(dir)
		if err != nil {
			t.Fatalf("LoadOrCreateSecret (second): %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("second load returned a different key; the secret was regenerated")
		}
		if after := statSecret(t, dir); after != before {
			t.Errorf("secret file changed across a read: %q -> %q", before, after)
		}
	})

	t.Run("index and catalog churn in the profile dir leaves the secret alone", func(t *testing.T) {
		dir := t.TempDir()
		key, err := tee.LoadOrCreateSecret(dir)
		if err != nil {
			t.Fatalf("LoadOrCreateSecret: %v", err)
		}
		hashBefore := computeHash(t, key)
		stamp := statSecret(t, dir)

		// Everything an embedding-model rotation would rewrite, twice with
		// different contents: the model identity stamp the spec reserves, the
		// vector index, the sparse index, and a catalog snapshot.
		for _, content := range []string{"model-a", "model-b"} {
			for _, name := range []string{"embeddings.model", "embeddings.bin", "bm25.bin", "catalog.json"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			reloaded, err := tee.LoadOrCreateSecret(dir)
			if err != nil {
				t.Fatalf("LoadOrCreateSecret after %s: %v", content, err)
			}
			if !bytes.Equal(reloaded, key) {
				t.Fatalf("secret changed after writing %s artifacts", content)
			}
			if got := computeHash(t, reloaded); got != hashBefore {
				t.Fatalf("artifact hash changed after %s: %s -> %s", content, hashBefore, got)
			}
			if now := statSecret(t, dir); now != stamp {
				t.Fatalf("secret file rewritten after %s: %q -> %q", content, stamp, now)
			}
		}
	})

	t.Run("two profiles never share a secret or a handle", func(t *testing.T) {
		a, err := tee.LoadOrCreateSecret(t.TempDir())
		if err != nil {
			t.Fatalf("profile a: %v", err)
		}
		b, err := tee.LoadOrCreateSecret(t.TempDir())
		if err != nil {
			t.Fatalf("profile b: %v", err)
		}
		if bytes.Equal(a, b) {
			t.Fatal("two profiles produced the same tee.secret; the key is not profile-scoped")
		}
		if computeHash(t, a) == computeHash(t, b) {
			t.Fatal("identical hash across profiles; a handle from one profile would resolve in another")
		}
	})

	t.Run("a corrupt secret is refused, never silently rotated", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := tee.LoadOrCreateSecret(dir); err != nil {
			t.Fatalf("LoadOrCreateSecret: %v", err)
		}
		path := tee.SecretPath(dir)
		if err := os.WriteFile(path, []byte("deadbeef"), 0o600); err != nil {
			t.Fatalf("corrupt: %v", err)
		}
		if _, err := tee.LoadOrCreateSecret(dir); err == nil {
			t.Fatal("a short secret was accepted; §9 lifecycle point 2 forbids silent regeneration")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(raw) != "deadbeef" {
			t.Errorf("the corrupt secret was rewritten to %q; it must be left for the operator", raw)
		}
	})
}

// statSecret returns a change stamp for the secret file: its bytes plus size
// and mode. Content is the load-bearing part; mode catches a rewrite that
// happens to reproduce the same key.
func statSecret(t *testing.T, dir string) string {
	t.Helper()
	path := tee.SecretPath(dir)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read secret: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat secret: %v", err)
	}
	return hex.EncodeToString(raw) + "|" + info.Mode().String()
}

// computeHash derives the artifact handle for one fixed invocation, so a
// change in the handle can only come from a change in the secret.
func computeHash(t *testing.T, key []byte) string {
	t.Helper()
	h, err := tee.ComputeHash(key, tee.HashInput{
		OpID:                   "gmail.users.messages.list",
		VariantIDResolved:      "gmail.v1.rest.users.messages.list",
		Args:                   map[string]any{"maxResults": 10, "q": "is:unread"},
		AuthSubjectFingerprint: "sha256:fixed-principal",
	})
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	return h
}
