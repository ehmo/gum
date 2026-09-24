package mcp

// Raw-wire proof of the gum://results/{hash} lifecycle contract (bead gum-sd58,
// docs/test-matrix.md).
//
// The row states a polling pattern, not a push one: a client copies an artifact
// before `artifact_expires_at` because gum sends nothing when the artifact goes
// away. Two halves make that contract usable, and only the first was proved.
//
// The first half is the advertised capability. TestMCPInitializeCapabilities
// already asserts `resources.subscribe` is false in the initialize result.
//
// The second half is the wire. A capability flag is a promise about later
// frames, so the assertion has to watch the stream across real reads: a hit, a
// miss, and a subscribe attempt, with no `notifications/resources/updated` in
// between. A decoded-client test cannot see this, because the SDK client drops
// notifications it has no handler for, and silence then looks identical to a
// notification that was sent and discarded.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/output/tee"
)

// newResultsWireConn isolates a profile home, writes one live artifact into it
// and returns a handshaked raw connection plus that artifact's hash.
func newResultsWireConn(t *testing.T) (*wireConn, string) {
	t.Helper()

	dataHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	profileDir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", profileDir, err)
	}

	secret, err := tee.LoadOrCreateSecret(profileDir)
	if err != nil {
		t.Fatalf("LoadOrCreateSecret: %v", err)
	}
	hash, err := tee.ComputeHash(secret, tee.HashInput{
		OpID:                   "gmail.messages.list",
		VariantIDResolved:      "gmail.messages.list.v1",
		Args:                   map[string]any{"q": "is:unread"},
		AuthSubjectFingerprint: "fp-wire",
	})
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	payload := map[string]any{"messages": []any{map[string]any{"id": "m1"}}}
	if _, err := tee.WriteJSON(profileDir, time.Now().UTC(), "gmail.messages.list", hash, payload); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	conn := newWireConn(t, NewServer(pairingDispatcher{}))
	conn.handshake()
	return conn, hash
}

func TestResultsReadsEmitNoResourceUpdatedNotifications(t *testing.T) {
	conn, hash := newResultsWireConn(t)
	liveURI := "gum://results/" + hash
	missURI := "gum://results/" + strings.Repeat("0", 64)

	// A hit. The artifact is on disk and inside the retention window.
	var read struct {
		Contents []struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(conn.call("resources/read", map[string]any{"uri": liveURI}), &read); err != nil {
		t.Fatalf("decode resources/read result: %v", err)
	}
	if len(read.Contents) != 1 || read.Contents[0].URI != liveURI {
		t.Fatalf("resources/read(%s) returned %+v; want exactly one content item for that URI", liveURI, read.Contents)
	}
	if !strings.Contains(read.Contents[0].Text, `"m1"`) {
		t.Errorf("artifact body = %q; want the stored payload", read.Contents[0].Text)
	}

	// A miss. This is the only signal a client gets that an artifact is gone,
	// which is what makes the polling pattern the row describes necessary.
	_, rpcErr := conn.callRaw("resources/read", map[string]any{"uri": missURI})
	if len(rpcErr) == 0 || string(rpcErr) == "null" {
		t.Fatal("resources/read on an absent hash succeeded; it must return the RESULT_ARTIFACT_EXPIRED error")
	}
	var wireErr struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rpcErr, &wireErr); err != nil {
		t.Fatalf("decode error member %s: %v", rpcErr, err)
	}
	if wireErr.Code != jsonRPCResultArtifactExpired {
		t.Errorf("error code = %d; want %d (RESULT_ARTIFACT_EXPIRED)", wireErr.Code, jsonRPCResultArtifactExpired)
	}
	if !strings.Contains(string(wireErr.Data), "RESULT_ARTIFACT_EXPIRED") {
		t.Errorf("error data = %s; want the RESULT_ARTIFACT_EXPIRED envelope", wireErr.Data)
	}

	// One more successful read, so any notification the server would fire on
	// resource access has had two chances to appear before the drain below.
	conn.call("resources/read", map[string]any{"uri": liveURI})

	assertNoResourceUpdated(t, conn)
}

// jsonRPCMethodNotFound is the JSON-RPC 2.0 code for an unregistered method.
const jsonRPCMethodNotFound = -32601

func TestResultsSubscribeIsRefused(t *testing.T) {
	conn, hash := newResultsWireConn(t)
	uri := "gum://results/" + hash

	// resources.subscribe is advertised false, so a client that subscribes
	// anyway must be told no. A silent success is worse than an error: the
	// client stops polling and waits for an update frame gum never sends.
	result, rpcErr := conn.callRaw("resources/subscribe", map[string]any{"uri": uri})
	if len(rpcErr) == 0 || string(rpcErr) == "null" {
		t.Fatalf("resources/subscribe returned result %s and no error; with resources.subscribe advertised false it must be refused, or a client will wait for notifications/resources/updated frames that gum never emits", result)
	}

	var wireErr struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(rpcErr, &wireErr); err != nil {
		t.Fatalf("decode error member %s: %v", rpcErr, err)
	}
	// -32601 is JSON-RPC method-not-found. Pinning the code catches a build
	// that registers a subscribe handler without turning the capability on:
	// that answers with a result, or with some other error, and either way the
	// refusal the advertised capability implies has stopped being true.
	if wireErr.Code != jsonRPCMethodNotFound {
		t.Errorf("resources/subscribe error code = %d; want %d (method not found), because gum registers no subscribe handler", wireErr.Code, jsonRPCMethodNotFound)
	}

	assertNoResourceUpdated(t, conn)
}

// assertNoResourceUpdated drains the stream and fails if the server sent any
// notifications/resources/updated frame. The drain uses a live request rather
// than a sleep: the server answers in order, so once its reply to a fresh call
// arrives, every frame it queued earlier has already been read.
func assertNoResourceUpdated(t *testing.T, conn *wireConn) {
	t.Helper()

	conn.call("resources/templates/list", map[string]any{})

	for _, note := range conn.notifications {
		if note.Method == "notifications/resources/updated" {
			t.Errorf("server sent %s with params %s; gum emits no resource-updated frames for results, so clients poll against artifact_expires_at instead", note.Method, note.Params)
		}
	}
}
