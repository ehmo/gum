package auth_test

import (
	"path/filepath"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
)

// TestPersistentBucketCloseIsIdempotent pins PersistentBucket.Close's
// `b.closed → return nil` arm (persistent_bucket.go:131-133). Calling
// Close twice MUST be safe — the docstring explicitly promises
// idempotency.
func TestPersistentBucketCloseIsIdempotent(t *testing.T) {
	defer goleak.VerifyNone(t)
	b, err := auth.OpenBucket(auth.BucketConfig{
		Path:                     filepath.Join(t.TempDir(), "bucket.db"),
		DefaultCapacity:          1,
		DefaultLeakRatePerSecond: 0,
	})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("second Close: %v; want nil (idempotent)", err)
	}
}
