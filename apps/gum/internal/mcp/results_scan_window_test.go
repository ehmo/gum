package mcp_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/output/tee"
)

// gum://results/{hash} must stay readable for as long as the expiry it
// advertised (bead gum-sd58).
//
// The lookup is a directory scan bounded by a day count. Hardcoding that count
// at 2 breaks the §7 polling contract the moment an operator raises
// output.tee_retention_hours: gum tells the client the artifact lives until
// _expression.artifact_expires_at, then answers RESULT_ARTIFACT_EXPIRED for a
// file that is still on disk and still inside its window.

// writeDatedArtifact writes a gzip JSON artifact under the UTC day of `when`
// and returns its hash. Each artifact gets its own args so the hashes differ.
func writeDatedArtifact(t *testing.T, profileDir, opID string, when time.Time, marker string) string {
	t.Helper()
	secret, err := tee.LoadOrCreateSecret(profileDir)
	if err != nil {
		t.Fatalf("LoadOrCreateSecret: %v", err)
	}
	hash, err := tee.ComputeHash(secret, tee.HashInput{
		OpID:                   opID,
		VariantIDResolved:      "v1",
		Args:                   map[string]any{"marker": marker},
		AuthSubjectFingerprint: "fp-test",
	})
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	if _, err := tee.WriteJSON(profileDir, when.UTC(), opID, hash, map[string]any{"marker": marker}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	return hash
}

func writeRetentionConfig(t *testing.T, hours string) {
	t.Helper()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	dir := filepath.Join(configHome, "gum", "default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body := "output.tee_retention_hours = \"" + hours + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestResultsScanWindowFollowsConfiguredRetention(t *testing.T) {
	defer goleak.VerifyNone(t)
	writeRetentionConfig(t, "168")
	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()

	now := time.Now().UTC()
	// Two newer day directories, so the 2-day default window would stop
	// before reaching the artifact under test.
	writeDatedArtifact(t, profileDir, "gmail.users.messages.list", now, "today")
	writeDatedArtifact(t, profileDir, "gmail.users.messages.list", now.AddDate(0, 0, -1), "yesterday")
	hash := writeDatedArtifact(t, profileDir, "gmail.users.messages.list", now.AddDate(0, 0, -3), "three-days-ago")

	uri := "gum://results/" + hash
	res, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource(%s): %v\nwith output.tee_retention_hours=168 the artifact is 3 days into a 7-day window, so the scan must still reach it", uri, err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("Contents len = %d; want 1", len(res.Contents))
	}
}

// TestResultsScanWindowStopsAtTheDefault pins the other side: with no
// configured retention the scan stays at the spec §9.0 24-hour window plus
// the UTC-day boundary, so an artifact well past it reads as expired rather
// than walking an unbounded tee tree.
func TestResultsScanWindowStopsAtTheDefault(t *testing.T) {
	defer goleak.VerifyNone(t)
	writeRetentionConfig(t, "24")
	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()

	now := time.Now().UTC()
	writeDatedArtifact(t, profileDir, "gmail.users.messages.list", now, "today")
	writeDatedArtifact(t, profileDir, "gmail.users.messages.list", now.AddDate(0, 0, -1), "yesterday")
	hash := writeDatedArtifact(t, profileDir, "gmail.users.messages.list", now.AddDate(0, 0, -3), "three-days-ago")

	uri := "gum://results/" + hash
	if _, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri}); err == nil {
		t.Fatalf("ReadResource(%s) succeeded; a 3-day-old artifact is outside the default 24h window and must read as RESULT_ARTIFACT_EXPIRED", uri)
	}
}
