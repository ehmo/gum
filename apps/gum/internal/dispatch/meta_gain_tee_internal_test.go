package dispatch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
)

// TestRetryTrackerEvictsEntriesOlderThanTheWindow covers the sweep inside
// observe. Without it the map grows for the life of the process, so a long MCP
// session leaks one entry per distinct call.
func TestRetryTrackerEvictsEntriesOlderThanTheWindow(t *testing.T) {
	t.Parallel()
	tr := &retryTracker{}
	t0 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	if tr.observe("a", t0) {
		t.Fatal("first observation of a reported a retry")
	}
	if !tr.observe("a", t0.Add(time.Minute)) {
		t.Error("second observation inside the window did not report a retry")
	}
	// "a" was last seen at t0+1m, so the sweep must run past that stamp.
	if tr.observe("b", t0.Add(time.Minute+gainRetryWindow+time.Second)) {
		t.Error("first observation of b reported a retry")
	}
	if _, still := tr.seen["a"]; still {
		t.Error("entry a survived past the retry window")
	}
	if len(tr.seen) != 1 {
		t.Errorf("tracker holds %d entries; want 1", len(tr.seen))
	}
}

// TestGainFieldMaskStatusNilInvocation covers the degraded-path guard. Step 9
// must not panic when it is reached without an invocation.
func TestGainFieldMaskStatusNilInvocation(t *testing.T) {
	t.Parallel()
	if got := gainFieldMaskStatus(nil); got != "not_applicable" {
		t.Errorf("gainFieldMaskStatus(nil) = %q; want \"not_applicable\"", got)
	}
}

// TestMeasureGainTokensEmptyInput covers the short circuit. An empty body costs
// no tokenizer work and must report 0 rather than the tokenizer's answer for "".
func TestMeasureGainTokensEmptyInput(t *testing.T) {
	t.Parallel()
	if got := measureGainTokens(nil); got != 0 {
		t.Errorf("measureGainTokens(nil) = %d; want 0", got)
	}
	if got := measureGainTokens([]byte{}); got != 0 {
		t.Errorf("measureGainTokens(empty) = %d; want 0", got)
	}
}

// TestNewExpressionMetaWithoutApplyOutput covers the out == nil return. A
// dispatch that produced no shaping result still owes the caller an envelope
// carrying the op_id and variant it resolved.
func TestNewExpressionMetaWithoutApplyOutput(t *testing.T) {
	t.Parallel()
	inv := &Invocation{OpID: "gmail.messages.list"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v1"}}
	meta := newExpressionMeta(inv, rv, &profile.Profile{Name: "p"}, nil)

	if meta.OpID != "gmail.messages.list" {
		t.Errorf("OpID = %q; want the invocation op", meta.OpID)
	}
	if meta.Profile != "p" {
		t.Errorf("Profile = %q; want \"p\"", meta.Profile)
	}
	if meta.VariantID == nil || *meta.VariantID != "v1" {
		t.Errorf("VariantID = %v; want v1", meta.VariantID)
	}
	if meta.Lossy {
		t.Error("Lossy = true with no shaping result; want false")
	}
}

// TestOpIDOfNilInvocation covers the nil-safe accessor.
func TestOpIDOfNilInvocation(t *testing.T) {
	t.Parallel()
	if got := opIDOf(nil); got != "" {
		t.Errorf("opIDOf(nil) = %q; want \"\"", got)
	}
}

// TestAttachArtifactHandlesGuards covers the two arms a degraded caller hits:
// no envelope or no artifact path is a no-op, and a zero retention falls back
// to the default rather than expiring the artifact immediately.
func TestAttachArtifactHandlesGuards(t *testing.T) {
	t.Parallel()
	var nilMeta *ExpressionMeta
	nilMeta.attachArtifactHandles("/p", "gum://results/x", 24, time.Now()) // must not panic

	m := &ExpressionMeta{}
	m.attachArtifactHandles("", "gum://results/x", 24, time.Now())
	if m.FullResultResource != "" || m.ArtifactExpiresAt != nil {
		t.Errorf("empty path still set handles: %+v", m)
	}

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	m2 := &ExpressionMeta{}
	m2.attachArtifactHandles("/p", "gum://results/x", 0, now)
	if m2.ArtifactExpiresAt == nil {
		t.Fatal("zero retention left ArtifactExpiresAt nil")
	}
	want := now.Add(time.Duration(defaultTeeRetentionHours) * time.Hour).Format(time.RFC3339)
	if *m2.ArtifactExpiresAt != want {
		t.Errorf("ArtifactExpiresAt = %q; want the default retention %q", *m2.ArtifactExpiresAt, want)
	}
}

// TestWrapKernelErrorNil covers the nil short circuit: step 7 calls this on
// every return, including the successful one.
func TestWrapKernelErrorNil(t *testing.T) {
	t.Parallel()
	if got := wrapKernelError(&Invocation{OpID: "x"}, nil); got != nil {
		t.Errorf("wrapKernelError(nil err) = %v; want nil", got)
	}
}

// TestWrappedKernelErrorAsFallsThroughForOtherTargets covers the false return
// in As. Returning true for every target would swallow the HTTPStatuser lookup
// that error mapping depends on, so the wrap must decline anything else and let
// errors.As keep walking to the cause.
func TestWrappedKernelErrorAsFallsThroughForOtherTargets(t *testing.T) {
	t.Parallel()
	cause := &stubUpstreamError{status: 503, body: []byte("boom")}
	wrapped := wrapKernelError(&Invocation{OpID: "gmail.messages.list"}, errors.New("plain kernel failure"))

	var notFound HTTPStatuser
	if errors.As(wrapped, &notFound) {
		t.Error("errors.As found an HTTPStatuser in a plain-cause wrap")
	}

	viaCause := &wrappedKernelError{
		StructuredError: NewStructuredError(ErrCodeServiceDown, "x"),
		cause:           cause,
	}
	var hs HTTPStatuser
	if !errors.As(viaCause, &hs) {
		t.Fatal("errors.As did not reach the cause's HTTPStatuser through the wrap")
	}
	if hs.HTTPStatusCode() != 503 {
		t.Errorf("HTTPStatusCode = %d; want 503", hs.HTTPStatusCode())
	}
}

// TestFailureTeePayloadSourceOrder covers the payload selection. The upstream
// body wins, then the error's carried body, then the mapped envelope; a nil
// error yields nothing to artifact.
func TestFailureTeePayloadSourceOrder(t *testing.T) {
	t.Parallel()

	resp := &Response{Body: []byte(`{"upstream":true}`)}
	carrier := &stubUpstreamError{status: 500, body: []byte(`{"carried":true}`)}
	if got := string(failureTeePayload(resp, carrier)); got != `{"upstream":true}` {
		t.Errorf("payload = %q; want the upstream body to win", got)
	}

	if got := failureTeePayload(nil, nil); got != nil {
		t.Errorf("payload with no response and no error = %q; want nil", got)
	}
	if got := failureTeePayload(&Response{}, nil); got != nil {
		t.Errorf("payload with an empty response body = %q; want nil", got)
	}

	// A 429 maps to a StructuredError, so the artifact carries the §7 envelope
	// rather than the adapter's Go error string.
	rateLimited := &stubUpstreamError{status: 429}
	got := failureTeePayload(nil, rateLimited)
	if len(got) == 0 {
		t.Fatal("a rate-limited failure produced no payload")
	}
	var env map[string]any
	if err := json.Unmarshal(got, &env); err != nil {
		t.Fatalf("payload is not JSON: %v (%q)", err, got)
	}
	if env["error_code"] != string(ErrCodeRateLimited) {
		t.Errorf("payload error_code = %v; want %s", env["error_code"], ErrCodeRateLimited)
	}
}

// TestWriteFailureTeeSkipsAnEmptyPayload covers the empty-payload return. A
// zero-byte artifact would corrupt the gum://results/{hash} reverse lookup.
func TestWriteFailureTeeSkipsAnEmptyPayload(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "default")
	d := &dispatcher{teeConfig: TeeConfig{ProfileDir: dir, Mode: "always", RetentionHours: 24}}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v1"}}

	d.writeFailureTee(&Invocation{OpID: "gmail.messages.list"}, rv, nil, nil, nil)

	if entries, err := os.ReadDir(filepath.Join(dir, "tee")); err == nil && len(entries) > 0 {
		t.Errorf("an empty payload still wrote %d tee entries", len(entries))
	}
}

// TestWriteFailureTeeSwallowsAWriteFailure covers the logged-error return. A
// tee failure must never replace the upstream error the caller needs to see.
func TestWriteFailureTeeSwallowsAWriteFailure(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	// ProfileDir is a child of a regular file, so secret creation fails ENOTDIR.
	d := &dispatcher{teeConfig: TeeConfig{
		ProfileDir:     filepath.Join(blocker, "profile"),
		Mode:           "failures",
		RetentionHours: 24,
	}}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v1"}}
	inv := &Invocation{OpID: "gmail.messages.list"}
	resp := &Response{Body: []byte(`{"upstream":"error"}`)}

	d.writeFailureTee(inv, rv, nil, resp, errors.New("upstream failed"))

	if _, err := os.Stat(filepath.Join(blocker, "profile")); err == nil {
		t.Error("the blocked profile dir was somehow created")
	}
}

// TestStructuredErrorMarshalRejectsAnUnencodableDetail pins the detail-value
// error arm. Detail is map[string]any, so an adapter that stashes a Go value
// with no JSON form must surface a marshal error instead of emitting a
// truncated envelope.
func TestStructuredErrorMarshalRejectsAnUnencodableDetail(t *testing.T) {
	t.Parallel()
	se := NewStructuredError(ErrCodeServiceDown, "boom").WithDetail("bad", make(chan int))
	got, err := se.MarshalJSON()
	if err == nil {
		t.Fatalf("MarshalJSON = %q, nil err; want a marshal error", got)
	}
	if !strings.Contains(err.Error(), "chan") {
		t.Errorf("err = %q; want the unsupported type named", err)
	}
}
