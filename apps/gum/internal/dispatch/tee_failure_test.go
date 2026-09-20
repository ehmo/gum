// Package dispatch — tee_mode = "failures" regression tests.
//
// Defect: writeTeeArtifact ran only at lifecycle step 7c, which the step-7
// error path returns before reaching. A profile that set tee_mode = "failures"
// therefore never wrote an artifact in the one case the mode exists for.
package dispatch

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// stubUpstreamError mimics an adapter error that carries the upstream status and
// body, as adapters.UpstreamError and googleads.upstreamError do.
type stubUpstreamError struct {
	status int
	body   []byte
}

func (e *stubUpstreamError) Error() string        { return "stub upstream failure" }
func (e *stubUpstreamError) HTTPStatusCode() int  { return e.status }
func (e *stubUpstreamError) UpstreamBody() []byte { return e.body }

// failingTeeFixture is newTeeFixture with an adapter that always fails.
func failingTeeFixture(t *testing.T, execErr error) (string, Dispatcher) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "default")
	adapter := &funcAdapter{
		execute: func(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
			return nil, execErr
		},
	}
	d := NewDispatcherWithConfig(minimalCatalog("stub"), map[string]Adapter{"stub": adapter}, DispatcherConfig{
		Tee: TeeConfig{ProfileDir: dir, RetentionHours: 24},
	})
	return dir, d
}

// teeArtifacts returns every artifact path under <dir>/tee.
func teeArtifacts(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	root := filepath.Join(dir, "tee")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".json.gz") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return found
}

// readArtifact gunzips an artifact.
func readArtifact(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() { _ = zr.Close() }()
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	return string(b)
}

// TestTeeFiresOnUpstream5xxWhenModeFailures is the positive 4xx/5xx clause of
// the tee_mode = "failures" definition in docs/expression-profile-dsl.md.
func TestTeeFiresOnUpstream5xxWhenModeFailures(t *testing.T) {
	t.Parallel()
	upstream := `{"error":{"code":503,"message":"backend unavailable"}}`
	dir, d := failingTeeFixture(t, &stubUpstreamError{status: 503, body: []byte(upstream)})

	_, err := d.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "tee-fail-503",
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "local_artifact", TeeMode: "failures"},
	})
	if err == nil {
		t.Fatal("Dispatch returned nil error; want the upstream failure")
	}

	got := teeArtifacts(t, dir)
	if len(got) != 1 {
		t.Fatalf("tee wrote %d artifacts; want 1 (tee_mode=failures on an upstream 503)", len(got))
	}
	if body := readArtifact(t, got[0]); body != upstream {
		t.Errorf("artifact body = %q; want the upstream body %q", body, upstream)
	}
}

// TestTeeFiresOnTransportErrorWithoutBody covers the transport clause: a DNS,
// TLS or timeout failure has no upstream body, and a zero-byte artifact is
// forbidden, so the artifact carries the error envelope instead.
func TestTeeFiresOnTransportErrorWithoutBody(t *testing.T) {
	t.Parallel()
	dir, d := failingTeeFixture(t, errors.New("dial tcp: connection refused"))

	_, err := d.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "tee-fail-transport",
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "local_artifact", TeeMode: "failures"},
	})
	if err == nil {
		t.Fatal("Dispatch returned nil error; want the transport failure")
	}

	got := teeArtifacts(t, dir)
	if len(got) != 1 {
		t.Fatalf("tee wrote %d artifacts; want 1 (transport error is a failures trigger)", len(got))
	}
	body := readArtifact(t, got[0])
	if !strings.Contains(body, "connection refused") {
		t.Errorf("artifact body = %q; want the error text", body)
	}
}

// TestTeeFiresOnFailureWhenModeAlways pins "always" as a superset of
// "failures": a profile that wants every result artifacted also wants the
// failure artifacted.
func TestTeeFiresOnFailureWhenModeAlways(t *testing.T) {
	t.Parallel()
	dir, d := failingTeeFixture(t, &stubUpstreamError{status: 429, body: []byte(`{"error":"slow down"}`)})

	_, err := d.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "tee-fail-always",
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "resource_link", TeeMode: "always"},
	})
	if err == nil {
		t.Fatal("Dispatch returned nil error; want the upstream failure")
	}
	if got := teeArtifacts(t, dir); len(got) != 1 {
		t.Fatalf("tee wrote %d artifacts; want 1 (tee_mode=always on an upstream 429)", len(got))
	}
}

// TestTeeSkippedOnFailureWhenModeOff keeps the explicit opt-out honoured.
func TestTeeSkippedOnFailureWhenModeOff(t *testing.T) {
	t.Parallel()
	dir, d := failingTeeFixture(t, &stubUpstreamError{status: 500, body: []byte(`{"error":"boom"}`)})

	_, err := d.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "tee-fail-off",
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "local_artifact", TeeMode: "off"},
	})
	if err == nil {
		t.Fatal("Dispatch returned nil error; want the upstream failure")
	}
	if got := teeArtifacts(t, dir); len(got) != 0 {
		t.Errorf("tee wrote %d artifacts; want 0 for tee_mode=off", len(got))
	}
}

// TestTeeSkippedOnPreUpstreamError is the normative exclusion: every error that
// fires in lifecycle steps 1-6 has no upstream payload, and writing an artifact
// for one would corrupt the gum://results/{hash} reverse lookup.
func TestTeeSkippedOnPreUpstreamError(t *testing.T) {
	t.Parallel()
	dir, d := failingTeeFixture(t, errors.New("never reached"))

	_, err := d.Dispatch(context.Background(), &Invocation{
		OpID:                   "no.such.op",
		Format:                 "json",
		RequestID:              "tee-fail-pre",
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "local_artifact", TeeMode: "failures"},
	})
	var se *StructuredError
	if !errors.As(err, &se) || se.ErrCode != ErrCodeOpNotFound {
		t.Fatalf("err = %v; want OP_NOT_FOUND", err)
	}
	if got := teeArtifacts(t, dir); len(got) != 0 {
		t.Errorf("tee wrote %d artifacts for a step-2 error; want 0", len(got))
	}
}
