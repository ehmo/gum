package dispatch

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/output/profile"
	"github.com/ehmo/gum/internal/output/tee"
)

// The expiry half of the §7 result-artifact polling contract (bead gum-sd58).
//
// A client's only defence against RESULT_ARTIFACT_EXPIRED is
// _expression.artifact_expires_at: read it once when the tool returns, then
// fetch or copy the artifact before it. That is worth nothing if the
// timestamp ignores the profile's configured retention, so this asserts the
// configured window end to end rather than the helper in isolation.

// newRetentionFixture is newTeeFixture with a caller-chosen retention window.
func newRetentionFixture(t *testing.T, retentionHours int) (Dispatcher, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "default")
	adapter := &funcAdapter{
		execute: func(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
			return &Response{Body: []byte(`{"hits":1}`), Format: "json", StatusCode: 200}, nil
		},
	}
	d := NewDispatcherWithConfig(minimalCatalog("stub"), map[string]Adapter{"stub": adapter}, DispatcherConfig{
		Tee: TeeConfig{ProfileDir: dir, RetentionHours: retentionHours},
	})
	return d, dir
}

func dispatchForArtifact(t *testing.T, d Dispatcher, requestID string) *ShapedResponse {
	t.Helper()
	shaped, err := d.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              requestID,
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "resource_link"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if shaped == nil {
		t.Fatal("Dispatch returned nil ShapedResponse")
	}
	return shaped
}

// TestArtifactExpiresAtFollowsConfiguredRetention uses 72 hours, not the
// spec default: with 24 a wired-through value and a hardcoded default are
// indistinguishable, so the assertion would pass against a build that dropped
// the configured window entirely.
func TestArtifactExpiresAtFollowsConfiguredRetention(t *testing.T) {
	t.Parallel()
	const retention = 72
	d, _ := newRetentionFixture(t, retention)

	before := time.Now().UTC()
	shaped := dispatchForArtifact(t, d, "expiry-configured")
	after := time.Now().UTC()

	if shaped.Expression == nil || shaped.Expression.ArtifactExpiresAt == nil {
		t.Fatalf("_expression.artifact_expires_at absent; the client has nothing to poll against: %+v", shaped.Expression)
	}
	got, err := time.Parse(time.RFC3339, *shaped.Expression.ArtifactExpiresAt)
	if err != nil {
		t.Fatalf("artifact_expires_at = %q is not RFC 3339: %v", *shaped.Expression.ArtifactExpiresAt, err)
	}
	lo := before.Add(retention * time.Hour).Truncate(time.Second)
	hi := after.Add(retention * time.Hour).Add(time.Second)
	if got.Before(lo) || got.After(hi) {
		t.Errorf("artifact_expires_at = %s; want within [%s, %s], i.e. now plus the configured %dh retention", got, lo, hi, retention)
	}
	if got.Location() != time.UTC && got.UTC() != got {
		t.Errorf("artifact_expires_at = %s; spec §13 requires a UTC RFC 3339 timestamp", got)
	}
}

// TestArtifactHandlesTravelTogether pins the three fields a client needs to
// act on the polling contract. A resource link with no expiry leaves the
// client guessing when to copy; an expiry with no handle leaves it nothing to
// copy.
func TestArtifactHandlesTravelTogether(t *testing.T) {
	t.Parallel()
	d, dir := newRetentionFixture(t, 0)

	shaped := dispatchForArtifact(t, d, "expiry-handles")

	meta := shaped.Expression
	if meta == nil {
		t.Fatal("ShapedResponse.Expression nil")
	}
	if meta.FullResultPath == "" || meta.FullResultResource == "" || meta.ArtifactExpiresAt == nil {
		t.Fatalf("_expression carries path=%q resource=%q expires=%v; recovery=resource_link must emit all three",
			meta.FullResultPath, meta.FullResultResource, meta.ArtifactExpiresAt)
	}
	if meta.FullResultPath != shaped.FullResultPath {
		t.Errorf("_expression.full_result_path = %q; want the shaped handle %q", meta.FullResultPath, shaped.FullResultPath)
	}
	if filepath.Dir(filepath.Dir(filepath.Dir(meta.FullResultPath))) != filepath.Join(dir, "tee") {
		t.Errorf("full_result_path %q is not <profile>/tee/<day>/<op>/<hash>.json.gz", meta.FullResultPath)
	}

	// Retention 0 means the kernel applies the spec §9.0 default.
	got, err := time.Parse(time.RFC3339, *meta.ArtifactExpiresAt)
	if err != nil {
		t.Fatalf("artifact_expires_at = %q is not RFC 3339: %v", *meta.ArtifactExpiresAt, err)
	}
	if delta := time.Until(got); delta < 23*time.Hour || delta > 25*time.Hour {
		t.Errorf("artifact_expires_at is %s away; want the default %dh window", delta, defaultTeeRetentionHours)
	}
}

// TestNoArtifactHandlesWithoutRecovery covers the other arm: a lossless
// profile writes nothing, so it must advertise no expiry. A stale
// artifact_expires_at on a response with no artifact would send the client
// polling a URI that never existed.
func TestNoArtifactHandlesWithoutRecovery(t *testing.T) {
	t.Parallel()
	d, _ := newRetentionFixture(t, 48)

	shaped, err := d.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "expiry-none",
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "none"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if shaped.Expression != nil && shaped.Expression.ArtifactExpiresAt != nil {
		t.Errorf("artifact_expires_at = %q with recovery=none; want absent", *shaped.Expression.ArtifactExpiresAt)
	}
}

// TestArtifactExpiryClampsAnAbsurdRetention covers the arithmetic edge.
// output.tee_retention_hours is a free-form string that strconv.Atoi will
// happily turn into a number near math.MaxInt. time.Duration is int64
// nanoseconds, so multiplying that by time.Hour wraps, and the wrap lands in
// the past: the client reads an artifact_expires_at older than the response
// carrying it and discards a handle that is in fact good for the full window.
func TestArtifactExpiryClampsAnAbsurdRetention(t *testing.T) {
	t.Parallel()

	for _, retention := range []int{math.MaxInt, math.MaxInt - 1, math.MaxInt32, tee.MaxRetentionHours + 1} {
		d, _ := newRetentionFixture(t, retention)
		shaped := dispatchForArtifact(t, d, "expiry-absurd")

		if shaped.Expression == nil || shaped.Expression.ArtifactExpiresAt == nil {
			t.Fatalf("retention %d: artifact_expires_at absent", retention)
		}
		got, err := time.Parse(time.RFC3339, *shaped.Expression.ArtifactExpiresAt)
		if err != nil {
			t.Fatalf("retention %d: artifact_expires_at = %q is not RFC 3339: %v", retention, *shaped.Expression.ArtifactExpiresAt, err)
		}
		if !got.After(time.Now().UTC()) {
			t.Errorf("retention %d: artifact_expires_at = %s is not in the future; the client will treat a live artifact as already expired", retention, got)
		}
	}
}
