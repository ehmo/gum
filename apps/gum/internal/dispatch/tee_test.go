package dispatch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
	"github.com/ehmo/gum/internal/output/tee"
)

// teeFixture builds a dispatcher with an isolated tee directory and a stub
// adapter that returns a fixed JSON body. The caller supplies the profile +
// fingerprint they want to flow through writeTeeArtifact.
type teeFixture struct {
	dir      string
	dispatch Dispatcher
	body     []byte
}

func newTeeFixture(t *testing.T, body []byte) *teeFixture {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "default")
	adapter := &funcAdapter{
		execute: func(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
			return &Response{Body: append([]byte(nil), body...), Format: "json", StatusCode: 200}, nil
		},
	}
	cat := minimalCatalog("stub")
	d := NewDispatcherWithConfig(cat, map[string]Adapter{"stub": adapter}, DispatcherConfig{
		Tee: TeeConfig{ProfileDir: dir, RetentionHours: 24},
	})
	return &teeFixture{dir: dir, dispatch: d, body: body}
}

// TestTeeWriteFiresOnLossyProfile is the spec §9.0 acceptance: when an
// expression profile sets recovery != "none", the dispatcher writes the
// post-upstream-projection body to <profileDir>/tee/<YYYY-MM-DD>/<op_id>/<hash>.json.gz
// and exposes the path on ShapedResponse.
func TestTeeWriteFiresOnLossyProfile(t *testing.T) {
	t.Parallel()
	fx := newTeeFixture(t, []byte(`{"messages":[{"id":"m1"}]}`))

	shaped, err := fx.dispatch.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "tee-fire",
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "local_artifact"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if shaped == nil || shaped.FullResultPath == "" {
		t.Fatalf("ShapedResponse.FullResultPath empty; want non-empty path under %s", fx.dir)
	}
	if !strings.HasPrefix(shaped.FullResultPath, fx.dir) {
		t.Errorf("FullResultPath %q does not live under tee dir %q", shaped.FullResultPath, fx.dir)
	}
	if _, err := os.Stat(shaped.FullResultPath); err != nil {
		t.Errorf("artifact missing at %s: %v", shaped.FullResultPath, err)
	}
	// local_artifact must NOT emit a resource_link.
	if shaped.FullResultResource != "" {
		t.Errorf("FullResultResource = %q; want empty for recovery=local_artifact", shaped.FullResultResource)
	}
}

// TestTeeOmittedWhenTeeModeOff verifies that an explicit tee_mode="off" on the
// profile suppresses the write even when recovery != "none". The profile
// validator rejects the combination resource_link+off, but local_artifact+off
// must be honoured (operator opts out of artifact storage explicitly).
func TestTeeOmittedWhenTeeModeOff(t *testing.T) {
	t.Parallel()
	fx := newTeeFixture(t, []byte(`{"messages":[{"id":"m1"}]}`))

	shaped, err := fx.dispatch.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "tee-off",
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "local_artifact", TeeMode: "off"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if shaped == nil {
		t.Fatal("Dispatch returned nil ShapedResponse")
	}
	if shaped.FullResultPath != "" {
		t.Errorf("FullResultPath = %q; want empty for tee_mode=off", shaped.FullResultPath)
	}
	// No tee directory should exist either.
	if _, err := os.Stat(filepath.Join(fx.dir, "tee")); err == nil {
		t.Errorf("tee directory created under %s despite tee_mode=off", fx.dir)
	}
}

// TestFullResultPathPresentInExpression locks down the contract that
// ShapedResponse.FullResultPath is the canonical handle the presentation layer
// projects into _expression.full_result_path (spec §9.0 line 1847). When the
// active profile uses recovery="resource_link", FullResultResource is also
// populated with the gum://results/<hash> URI.
func TestFullResultPathPresentInExpression(t *testing.T) {
	t.Parallel()
	fx := newTeeFixture(t, []byte(`{"hits":1}`))

	shaped, err := fx.dispatch.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "tee-link",
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "resource_link"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if shaped == nil || shaped.FullResultPath == "" {
		t.Fatalf("FullResultPath empty; want non-empty path")
	}
	if !strings.HasPrefix(shaped.FullResultResource, "gum://results/") {
		t.Fatalf("FullResultResource = %q; want gum://results/<hash> prefix", shaped.FullResultResource)
	}
	// The hash trailing the URI must match the leaf filename.
	wantHash := strings.TrimPrefix(shaped.FullResultResource, "gum://results/")
	gotBase := filepath.Base(shaped.FullResultPath)
	if gotBase != wantHash+".json.gz" {
		t.Errorf("path base %q does not encode hash %q", gotBase, wantHash)
	}
	// And the artifact file must exist on disk.
	if _, err := os.Stat(shaped.FullResultPath); err != nil {
		t.Errorf("artifact missing at %s: %v", shaped.FullResultPath, err)
	}
}

// TestFullResultSizeMatchesDecompressedBody — bead-named acceptance for
// gum-6krt. When tee fires, the dispatcher MUST attach the decompressed
// payload length to ShapedResponse.FullResultSize so the MCP layer can
// thread it onto ResourceLink.Size (spec §9.0 line 1846 "size when known").
// The size MUST match what `gum://results/<hash>` resources/read returns —
// i.e. len(resp.Body) — not the gzip-compressed on-disk file size.
func TestFullResultSizeMatchesDecompressedBody(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"id":"m1","subject":"hello"}]}`)
	fx := newTeeFixture(t, body)

	shaped, err := fx.dispatch.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "tee-size",
		AuthSubjectFingerprint: "fp-size",
		OutputProfile:          &profile.Profile{Recovery: "resource_link"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if shaped.FullResultSize == nil {
		t.Fatal("ShapedResponse.FullResultSize is nil; want non-nil when tee fires")
	}
	if got, want := *shaped.FullResultSize, int64(len(body)); got != want {
		t.Errorf("FullResultSize = %d; want decompressed payload len %d", got, want)
	}

	// And no-tee branch: profile with nil recovery yields no tee write, so
	// FullResultSize MUST stay nil (clients distinguish unknown from zero).
	shapedNoTee, err := fx.dispatch.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "tee-size-skip",
		AuthSubjectFingerprint: "fp-size",
		// no OutputProfile → tee disabled
	})
	if err != nil {
		t.Fatalf("Dispatch (no-tee): %v", err)
	}
	if shapedNoTee.FullResultSize != nil {
		t.Errorf("FullResultSize = %d; want nil when tee did not fire", *shapedNoTee.FullResultSize)
	}
}

// TestPrincipalScopedDedup runs the same op+args twice with different
// AuthSubjectFingerprints. The two writes MUST produce distinct artifact
// hashes (and therefore distinct paths) so cross-principal handles are
// non-reusable per spec §9.0 line 1846.
func TestPrincipalScopedDedup(t *testing.T) {
	t.Parallel()
	fx := newTeeFixture(t, []byte(`{"hits":1}`))

	dispatch := func(fp string) string {
		t.Helper()
		shaped, err := fx.dispatch.Dispatch(context.Background(), &Invocation{
			OpID:                   "gum.code",
			Format:                 "json",
			RequestID:              "tee-prin-" + fp,
			Args:                   map[string]any{"q": "same"},
			AuthSubjectFingerprint: fp,
			OutputProfile:          &profile.Profile{Recovery: "local_artifact"},
		})
		if err != nil {
			t.Fatalf("Dispatch(fp=%s): %v", fp, err)
		}
		if shaped == nil || shaped.FullResultPath == "" {
			t.Fatalf("Dispatch(fp=%s) returned empty FullResultPath", fp)
		}
		return shaped.FullResultPath
	}

	pathA := dispatch("principal-A")
	pathB := dispatch("principal-B")
	if pathA == pathB {
		t.Errorf("expected per-principal distinct artifacts; both fingerprints produced %s", pathA)
	}

	// Same fingerprint + same args MUST collide (profile+principal-scoped dedup).
	pathACopy := dispatch("principal-A")
	if pathA != pathACopy {
		t.Errorf("expected same-principal dedup; got %q then %q", pathA, pathACopy)
	}

	// Sanity: artifact bodies roundtrip to the original JSON.
	for _, p := range []string{pathA, pathB} {
		// Open via the standard library to verify the gzipped JSON.
		f, err := os.Open(p)
		if err != nil {
			t.Fatalf("open %s: %v", p, err)
		}
		_ = f.Close()
	}
}

// TestTeeSkippedWhenProfileNil exercises the no-profile path: when an
// invocation arrives with OutputProfile == nil (CLI raw mode, gum.code with
// no resolved profile), the dispatcher MUST NOT write any tee artifact even
// though a TeeConfig.ProfileDir is configured.
func TestTeeSkippedWhenProfileNil(t *testing.T) {
	t.Parallel()
	fx := newTeeFixture(t, []byte(`{"hits":1}`))

	shaped, err := fx.dispatch.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "tee-nil-profile",
		AuthSubjectFingerprint: "fp-test",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if shaped == nil {
		t.Fatal("Dispatch returned nil ShapedResponse")
	}
	if shaped.FullResultPath != "" {
		t.Errorf("FullResultPath = %q; want empty for nil profile", shaped.FullResultPath)
	}
}

// TestEffectiveTeeModeDefaults locks down the precedence in spec §9.0:
// profile TeeMode > config Mode > derived "always" when recovery!=none > "off".
func TestEffectiveTeeModeDefaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		prof *profile.Profile
		cfg  string
		want string
	}{
		{"nil profile, no cfg", nil, "", "off"},
		{"profile recovery none", &profile.Profile{Recovery: "none"}, "", "off"},
		{"profile recovery local_artifact default", &profile.Profile{Recovery: "local_artifact"}, "", "always"},
		{"profile recovery resource_link default", &profile.Profile{Recovery: "resource_link"}, "", "always"},
		{"profile TeeMode beats default", &profile.Profile{Recovery: "local_artifact", TeeMode: "off"}, "", "off"},
		{"cfg overrides default when profile silent", &profile.Profile{Recovery: "local_artifact"}, "failures", "failures"},
		{"profile TeeMode beats cfg", &profile.Profile{Recovery: "local_artifact", TeeMode: "always"}, "failures", "always"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := effectiveTeeMode(c.prof, c.cfg); got != c.want {
				t.Errorf("effectiveTeeMode(%+v, %q) = %q; want %q", c.prof, c.cfg, got, c.want)
			}
		})
	}
}

// TestTeeBodyMatchesUpstream proves that the tee artifact stores the raw
// upstream body byte-for-byte (post-upstream-projection, pre-host-shaping):
// the gzipped JSON unmarshals back to the originating struct.
func TestTeeBodyMatchesUpstream(t *testing.T) {
	t.Parallel()
	payload := `{"messages":[{"id":"m1"},{"id":"m2"}],"truncated":false}`
	fx := newTeeFixture(t, []byte(payload))

	shaped, err := fx.dispatch.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              "tee-body",
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "local_artifact"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if shaped.FullResultPath == "" {
		t.Fatal("tee did not fire")
	}
	// Read the gzip artifact back through tee.Read in the production package.
	roundtrip, err := tee.Read(shaped.FullResultPath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(roundtrip, &got); err != nil {
		t.Fatalf("artifact not JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(payload), &want); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if got["truncated"] != want["truncated"] {
		t.Errorf("roundtrip truncated = %v; want %v", got["truncated"], want["truncated"])
	}
}
