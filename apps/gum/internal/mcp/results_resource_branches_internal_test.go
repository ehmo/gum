package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestProfileDataDirReturnsEmptyOnNoHome pins profileDataDir's
// `os.UserHomeDir err → return ""` arm (results_resource.go:147-149).
// On Unix, os.UserHomeDir returns the "$HOME is not defined" error
// when $HOME is empty *and* XDG_DATA_HOME is not set. The empty
// return is the spec-mandated sentinel that maps to
// RESULT_ARTIFACT_EXPIRED at the caller (line 62-64).
func TestProfileDataDirReturnsEmptyOnNoHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")
	s := &Server{profile: "default"}
	if got := s.profileDataDir(); got != "" {
		t.Errorf("profileDataDir()=%q; want \"\" (no XDG_DATA_HOME, no HOME)", got)
	}
}

// TestHandleResultsResourceNoProfileDirReturnsExpired pins
// handleResultsResource's `profileDir == "" → expiredArtifactError`
// arm (results_resource.go:62-64). When the home directory cannot be
// resolved (a hostile sandbox or unset environment), the handler MUST
// surface RESULT_ARTIFACT_EXPIRED rather than leak the
// "$HOME is not defined" error string out to the client.
func TestHandleResultsResourceNoProfileDirReturnsExpired(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")
	s := &Server{profile: "default"}

	uri := "gum://results/deadbeef00112233"
	req := &sdkmcp.ReadResourceRequest{
		Params: &sdkmcp.ReadResourceParams{URI: uri},
	}
	res, err := s.handleResultsResource(context.Background(), req)
	if err == nil {
		t.Fatalf("handleResultsResource(no home)=%+v nil err; want RESULT_ARTIFACT_EXPIRED", res)
	}
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err type=%T; want *jsonrpc.Error", err)
	}
	if rpcErr.Code != jsonRPCResultArtifactExpired {
		t.Errorf("rpcErr.Code=%d; want %d", rpcErr.Code, jsonRPCResultArtifactExpired)
	}
	var env map[string]any
	if err := json.Unmarshal(rpcErr.Data, &env); err != nil {
		t.Fatalf("rpcErr.Data not JSON: %v", err)
	}
	// The hash MUST be carried through even when profileDir is empty —
	// it's the only piece of correlation a recovery tool can lock onto.
	if env["hash"] != "deadbeef00112233" {
		t.Errorf("envelope.hash=%v; want deadbeef00112233", env["hash"])
	}
}

// TestHandleResultsResourceCorruptArtifactReturnsExpired pins
// handleResultsResource's `tee.Read err → expiredArtifactError` arm
// (results_resource.go:70-72). When FindArtifact locates the file but
// the on-disk bytes are not valid gzip (artifact truncated mid-write,
// disk corruption, manual tampering), the handler MUST degrade to
// RESULT_ARTIFACT_EXPIRED rather than crash or leak the gzip parser
// error to the wire.
func TestHandleResultsResourceCorruptArtifactReturnsExpired(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	profileDir := filepath.Join(dataHome, "gum", "default")

	// Plant a file at the canonical artifact path that FindArtifact
	// will discover: <profileDir>/tee/<YYYY-MM-DD>/<op_id>/<hash>.json.gz.
	hash := "abc0123456789def"
	day := time.Now().UTC().Format("2006-01-02")
	opDir := filepath.Join(profileDir, "tee", day, "test.op")
	if err := os.MkdirAll(opDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// "not gzip" — gzip.NewReader returns ErrHeader.
	if err := os.WriteFile(filepath.Join(opDir, hash+".json.gz"), []byte("not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Server{profile: "default"}
	uri := "gum://results/" + hash
	req := &sdkmcp.ReadResourceRequest{
		Params: &sdkmcp.ReadResourceParams{URI: uri},
	}
	res, err := s.handleResultsResource(context.Background(), req)
	if err == nil {
		t.Fatalf("handleResultsResource(corrupt artifact)=%+v nil err; want RESULT_ARTIFACT_EXPIRED", res)
	}
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err type=%T; want *jsonrpc.Error", err)
	}
	if rpcErr.Code != jsonRPCResultArtifactExpired {
		t.Errorf("rpcErr.Code=%d; want %d (gzip read failure must map to artifact-expired)", rpcErr.Code, jsonRPCResultArtifactExpired)
	}
	var env map[string]any
	if err := json.Unmarshal(rpcErr.Data, &env); err != nil {
		t.Fatalf("rpcErr.Data not JSON: %v", err)
	}
	if env["hash"] != hash {
		t.Errorf("envelope.hash=%v; want %s", env["hash"], hash)
	}
	if env["uri"] != uri {
		t.Errorf("envelope.uri=%v; want %s", env["uri"], uri)
	}
}
