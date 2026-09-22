package gain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
	"github.com/ehmo/gum/internal/output/toon"
)

// writeReplayFixture plants one fixture leaf under a fresh temp root and
// returns the root and the leaf.
func writeReplayFixture(t *testing.T, opID, responseJSON string) (root, leaf string) {
	t.Helper()

	root = t.TempDir()
	leaf = filepath.Join(root, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	req, err := json.Marshal(map[string]string{"op_id": opID})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leaf, "request.json"), req, 0o644); err != nil {
		t.Fatalf("request.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leaf, "response.json"), []byte(responseJSON), 0o644); err != nil {
		t.Fatalf("response.json: %v", err)
	}
	return root, leaf
}

// readReplayCounts reads the expected-tokens-cl100k.json a replay wrote.
func readReplayCounts(t *testing.T, leaf string) map[string]int {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(leaf, "expected-tokens-cl100k.json"))
	if err != nil {
		t.Fatalf("read expected-tokens-cl100k.json: %v", err)
	}
	var counts map[string]int
	if err := json.Unmarshal(body, &counts); err != nil {
		t.Fatalf("parse expected-tokens-cl100k.json: %v", err)
	}
	return counts
}

// TestReplayMeasuresTheNineZeroDocument pins bead gum-5ij4: the replay
// estimator MUST measure the bytes the response path emits, which is the
// §9.0 two-section document (profile.Apply -> toon.EncodeDocument), not
// the headerless toon.Encode form. Before the fix expected-toon.txt held
// `messages=[{...}]` on one line, which DecodeTOONDocument rejects, and
// out_toon was the count of those bytes.
func TestReplayMeasuresTheNineZeroDocument(t *testing.T) {
	const opID = "gmail.users.messages.list"
	root, leaf := writeReplayFixture(t, opID,
		`{"messages":[{"id":"a","threadId":"t1"},{"id":"b","threadId":"t2"}],"resultSizeEstimate":2}`)

	if _, err := gain.RunFixtureReplay(root, "toon"); err != nil {
		t.Fatalf("RunFixtureReplay: %v", err)
	}

	emitted, err := os.ReadFile(filepath.Join(leaf, "expected-toon.txt"))
	if err != nil {
		t.Fatalf("read expected-toon.txt: %v", err)
	}
	doc, err := toon.DecodeTOONDocument(emitted)
	if err != nil {
		t.Fatalf("expected-toon.txt is not a §9.0 document: %v\n%s", err, emitted)
	}
	if doc.Op != opID {
		t.Errorf("op header = %q; want %q", doc.Op, opID)
	}
	if doc.Count != 2 {
		t.Errorf("count header = %d; want 2", doc.Count)
	}
	if doc.RecordKey != "messages" {
		t.Errorf("records header = %q; want \"messages\"", doc.RecordKey)
	}

	want, err := gain.MeasureTokensCl100k(emitted)
	if err != nil {
		t.Fatalf("measure emitted: %v", err)
	}
	if got := readReplayCounts(t, leaf)["out_toon"]; got != want {
		t.Errorf("out_toon = %d; want %d (the token count of the emitted document)", got, want)
	}
}

// TestReplayFallsBackToJSONWhenNotRepresentable pins the other half of the
// §9.0 contract: a body §9.0 cannot carry relabels to JSON on the response
// path, so the replay measures the same JSON bytes rather than a TOON form
// gum never sends. A nested object graph with no record array is the
// fallback case.
func TestReplayFallsBackToJSONWhenNotRepresentable(t *testing.T) {
	root, leaf := writeReplayFixture(t, "drive.files.get",
		`{"id":"f1","owners":[{"emailAddress":"a@example.com"}],"capabilities":{"canEdit":true}}`)

	if _, err := gain.RunFixtureReplay(root, "toon"); err != nil {
		t.Fatalf("RunFixtureReplay: %v", err)
	}

	emitted, err := os.ReadFile(filepath.Join(leaf, "expected-toon.txt"))
	if err != nil {
		t.Fatalf("read expected-toon.txt: %v", err)
	}
	if _, err := toon.DecodeTOONDocument(emitted); err == nil {
		t.Fatalf("a non-representable body produced a §9.0 document:\n%s", emitted)
	}
	var round any
	if err := json.Unmarshal(emitted, &round); err != nil {
		t.Fatalf("fallback bytes are neither a §9.0 document nor JSON: %v\n%s", err, emitted)
	}

	counts := readReplayCounts(t, leaf)
	if counts["out_toon"] != counts["out_json"] {
		t.Errorf("out_toon = %d, out_json = %d; the JSON fallback must report the JSON count",
			counts["out_toon"], counts["out_json"])
	}
}
