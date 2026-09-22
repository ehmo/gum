package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/embedded"
)

// probeNow is a fixed clock for the audit-log probe assertions.
var probeNow = time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)

// skipIfRoot guards the permission-based seams below. Root bypasses the
// mode bits, so the failure arms would never fire.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission arms are unreachable")
	}
}

// TestProbeAuditLogCannotCreateDir pins probeAuditLog's MkdirAll arm
// (health_probes.go:95-101). The profile dir does not exist and its
// parent refuses writes, so the probe MUST report degraded instead of
// panicking on a half-created path.
func TestProbeAuditLogCannotCreateDir(t *testing.T) {
	skipIfRoot(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	got := probeAuditLog(probeNow, filepath.Join(parent, "profile"))
	if got.Status != "degraded" {
		t.Errorf("Status=%q; want degraded", got.Status)
	}
	if !strings.Contains(got.Detail, "cannot create audit dir") {
		t.Errorf("Detail=%q; want the mkdir failure", got.Detail)
	}
}

// TestProbeAuditLogCannotInspectSentinel pins the `!os.IsNotExist` arm
// (health_probes.go:111-117). The profile dir exists but is not
// searchable, so stat of audit.broken fails with EACCES rather than
// ENOENT. A probe that treated every stat error as "absent" would
// report healthy on an unreadable audit sink.
func TestProbeAuditLogCannotInspectSentinel(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o600); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	got := probeAuditLog(probeNow, dir)
	if got.Status != "degraded" {
		t.Errorf("Status=%q; want degraded", got.Status)
	}
	if !strings.Contains(got.Detail, "cannot inspect audit.broken") {
		t.Errorf("Detail=%q; want the stat failure", got.Detail)
	}
}

// TestProbeAuditLogDirNotWritable pins the probe-write arm
// (health_probes.go:120-126). The dir is readable and searchable, so
// the sentinel stat reports ENOENT, but the probe file cannot be
// written.
func TestProbeAuditLogDirNotWritable(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	got := probeAuditLog(probeNow, dir)
	if got.Status != "degraded" {
		t.Errorf("Status=%q; want degraded", got.Status)
	}
	if !strings.Contains(got.Detail, "audit dir not writable") {
		t.Errorf("Detail=%q; want the write failure", got.Detail)
	}
}

// TestAuditBrokenHintArms pins readHealthAuditBrokenHint's three
// fallbacks (health_probes.go:138-150): an unopenable sentinel, a
// sentinel that stats fine but cannot be read, and an empty sentinel.
// Each fallback MUST still yield a non-empty hint so the degraded
// detail names something an operator can act on.
func TestAuditBrokenHintArms(t *testing.T) {
	skipIfRoot(t)

	t.Run("open_denied", func(t *testing.T) {
		dir := t.TempDir()
		sentinel := filepath.Join(dir, "audit.broken")
		if err := os.WriteFile(sentinel, []byte("disk full"), 0o000); err != nil {
			t.Fatalf("write sentinel: %v", err)
		}
		got := probeAuditLog(probeNow, dir)
		if !strings.Contains(got.Detail, "permission denied") {
			t.Errorf("Detail=%q; want the open failure", got.Detail)
		}
	})

	t.Run("read_fails", func(t *testing.T) {
		dir := t.TempDir()
		// A directory opens read-only on unix and reports EISDIR on the
		// first read, so the failure lands inside io.ReadAll.
		if err := os.Mkdir(filepath.Join(dir, "audit.broken"), 0o700); err != nil {
			t.Fatalf("mkdir sentinel: %v", err)
		}
		got := probeAuditLog(probeNow, dir)
		if !strings.Contains(got.Detail, "is a directory") {
			t.Errorf("Detail=%q; want the read failure", got.Detail)
		}
	})

	t.Run("empty_sentinel", func(t *testing.T) {
		dir := t.TempDir()
		sentinel := filepath.Join(dir, "audit.broken")
		if err := os.WriteFile(sentinel, nil, 0o600); err != nil {
			t.Fatalf("write sentinel: %v", err)
		}
		got := probeAuditLog(probeNow, dir)
		if !strings.Contains(got.Detail, sentinel) {
			t.Errorf("Detail=%q; want the sentinel path as the hint", got.Detail)
		}
	})
}

// swapHelpManifest replaces the embedded help manifest for one test and
// restores it afterwards. go:embed only initialises the var, so an
// assignment is enough to drive the parse and lookup arms that the
// shipped manifest can never reach.
func swapHelpManifest(t *testing.T, body string) {
	t.Helper()
	prev := embedded.HelpTopicsJSON
	embedded.HelpTopicsJSON = []byte(body)
	t.Cleanup(func() { embedded.HelpTopicsJSON = prev })
}

// TestHelpManifestParseFailureArms pins loadHelpManifest's unmarshal
// arm (help_resource.go:145-147) through both callers: the topics
// listing (help_resource.go:89-92) and a single topic read
// (help_resource.go:112-114). A corrupt manifest MUST surface
// RESOURCE_NOT_FOUND, never a partial listing.
func TestHelpManifestParseFailureArms(t *testing.T) {
	swapHelpManifest(t, "{not json")
	s := makeHelpServer()

	listReq := &sdkmcp.ReadResourceRequest{
		Params: &sdkmcp.ReadResourceParams{URI: helpTopicsURI},
	}
	if res, err := s.handleHelpTopicsList(context.Background(), listReq); err == nil {
		t.Fatalf("handleHelpTopicsList=%+v nil err; want RESOURCE_NOT_FOUND", res)
	}

	readReq := &sdkmcp.ReadResourceRequest{
		Params: &sdkmcp.ReadResourceParams{URI: helpTopicURIPrefix + "auth"},
	}
	if res, err := s.handleHelpTopicRead(context.Background(), readReq); err == nil {
		t.Fatalf("handleHelpTopicRead=%+v nil err; want RESOURCE_NOT_FOUND", res)
	}
}

// TestHelpTopicDeprecatedReturnsRedirect pins the deprecated-row arm
// (help_resource.go:119-129). A deprecated topic answers with the §7
// JSON redirect shape, not markdown, so a client can follow the move.
func TestHelpTopicDeprecatedReturnsRedirect(t *testing.T) {
	swapHelpManifest(t, `{"schema_version":1,"topics":[
		{"topic":"old-topic","status":"deprecated","redirect_topic":"new-topic"}]}`)
	s := makeHelpServer()

	req := &sdkmcp.ReadResourceRequest{
		Params: &sdkmcp.ReadResourceParams{URI: helpTopicURIPrefix + "old-topic"},
	}
	res, err := s.handleHelpTopicRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleHelpTopicRead(deprecated) err=%v; want the redirect body", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("Contents=%d; want 1", len(res.Contents))
	}
	if res.Contents[0].MIMEType != "application/json" {
		t.Errorf("MIMEType=%q; want application/json", res.Contents[0].MIMEType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal redirect body: %v", err)
	}
	if payload["status"] != "deprecated" || payload["redirect"] != "new-topic" {
		t.Errorf("payload=%v; want status=deprecated redirect=new-topic", payload)
	}
	// Spec §13 line 3264 says the redirect body contains only these two
	// keys. Leaking one_line_description or the row's own topic name would
	// hand a client fields it must not start depending on (bead gum-p1ko).
	if len(payload) != 2 {
		t.Errorf("payload has %d keys (%v); want exactly status and redirect", len(payload), payload)
	}
}

// TestHelpTopicWithoutMarkdownReturnsNotFound pins the `topics.Read
// !ok` drift arm (help_resource.go:130-135). The manifest claims a
// topic that ships no markdown body; the read MUST fail loudly rather
// than serve an empty document.
func TestHelpTopicWithoutMarkdownReturnsNotFound(t *testing.T) {
	swapHelpManifest(t, `{"schema_version":1,"topics":[
		{"topic":"ghost-topic","status":"active"}]}`)
	s := makeHelpServer()

	req := &sdkmcp.ReadResourceRequest{
		Params: &sdkmcp.ReadResourceParams{URI: helpTopicURIPrefix + "ghost-topic"},
	}
	res, err := s.handleHelpTopicRead(context.Background(), req)
	if err == nil {
		t.Fatalf("handleHelpTopicRead(ghost)=%+v nil err; want RESOURCE_NOT_FOUND", res)
	}
}

// TestSearchAPIsTuningUnloadableConfigUsesDefaults pins
// loadSearchAPIsTuning's `config.Load err → defaults` arm
// (meta_tool_profiles.go:34-37). An unparseable profile name makes the
// load fail; the handler MUST keep serving with the spec defaults.
func TestSearchAPIsTuningUnloadableConfigUsesDefaults(t *testing.T) {
	got := loadSearchAPIsTuning("bad/name", nil)
	if got.k != 5 || got.defaultChars != 120 {
		t.Errorf("tuning=%+v; want k=5 defaultChars=120", got)
	}
	if got.maxItemsBound {
		t.Error("maxItemsBound=true; want false when the config never loaded")
	}
}
