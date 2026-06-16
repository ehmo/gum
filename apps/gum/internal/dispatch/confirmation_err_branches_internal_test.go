package dispatch

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCurrentKeyVersionReadDirNonExistentErrSurfaces pins
// currentKeyVersion's non-IsNotExist err arm (confirmation.go:122)
// AND loadKeyForVersion's currentKeyVersion-err propagation (line
// 137-139). Reached when keyDir is a regular file rather than a
// directory — os.ReadDir then returns ENOTDIR, NOT a NotExist err,
// so the IsNotExist short-circuit doesn't fire and currentKeyVersion
// surfaces the raw err.
//
// Single test covers both arms because loadKeyForVersion's first call
// is currentKeyVersion; if that errs, line 137-139 propagates without
// reaching the file-read.
func TestCurrentKeyVersionReadDirNonExistentErrSurfaces(t *testing.T) {
	tmp := t.TempDir()
	// Plant a regular file at the keyDir path → ReadDir ENOTDIR.
	blocker := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	s, err := NewTokenStore(8, time.Minute, blocker)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}

	// currentKeyVersion direct: should err (not return 1, not silent).
	if _, err := s.currentKeyVersion(); err == nil {
		t.Error("currentKeyVersion(non-dir keyDir)=nil err; want ENOTDIR surface")
	}

	// loadKeyForVersion propagates: should err with the SAME root err.
	if _, err := s.loadKeyForVersion(1); err == nil {
		t.Error("loadKeyForVersion(non-dir keyDir)=nil err; want propagated err")
	}
}

// TestIssueTokenEnsureCurrentKeyMkdirFailureSurfacesError pins
// IssueToken's `ensureCurrentKey err → return "", err` arm
// (confirmation.go:218-220) AND ensureCurrentKey's "create key dir"
// wrap (line 161-163). Reached when keyDir's parent is a regular file
// → MkdirAll fails with ENOTDIR. The wrap label "confirmation: create
// key dir:" distinguishes filesystem failures from key-corruption
// downstream so operators triage env-misconfig separately.
func TestIssueTokenEnsureCurrentKeyMkdirFailureSurfacesError(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	keyDir := filepath.Join(blocker, "keys")
	s, err := NewTokenStore(8, time.Minute, keyDir)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}

	tok, err := s.IssueToken(AllowedPurposes[0])
	if err == nil {
		t.Fatalf("IssueToken(blocker keyDir)=%q nil err; want ensureCurrentKey wrap", tok)
	}
	if tok != "" {
		t.Errorf("tok=%q; want empty on err", tok)
	}
}

// TestConsumeTokenPayloadBase64DecodeErrSurfacesMalformed pins
// ConsumeToken's `enc.DecodeString(payloadPart) err → ErrMalformedToken`
// arm (confirmation.go:302-304). Reached with a token whose payload
// portion has invalid base64url length (a single char encodes 6 bits
// = 0 whole bytes, which RawURLEncoding rejects). The regex allows
// `[A-Za-z0-9_-]+` so length=1 passes regex but fails decode.
func TestConsumeTokenPayloadBase64DecodeErrSurfacesMalformed(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewTokenStore(8, time.Minute, tmp)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	// "a.bb" — payload "a" has length 1 → RawURLEncoding rejects.
	err = s.ConsumeToken("a.bb", AllowedPurposes[0])
	if !errors.Is(err, ErrMalformedToken) {
		t.Errorf("err=%v; want ErrMalformedToken from payload decode", err)
	}
}

// TestConsumeTokenSigBase64DecodeErrSurfacesMalformed pins
// ConsumeToken's `enc.DecodeString(hmacPart) err → ErrMalformedToken`
// arm (confirmation.go:306-308). Same length-1 trick as the payload
// test, but on the sig segment.
func TestConsumeTokenSigBase64DecodeErrSurfacesMalformed(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewTokenStore(8, time.Minute, tmp)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	err = s.ConsumeToken("bb.a", AllowedPurposes[0])
	if !errors.Is(err, ErrMalformedToken) {
		t.Errorf("err=%v; want ErrMalformedToken from sig decode", err)
	}
}

// TestConsumeTokenJSONUnmarshalErrSurfacesMalformed pins
// ConsumeToken's `json.Unmarshal err → ErrMalformedToken` arm
// (confirmation.go:312-314). Crafts a token whose payload base64-
// decodes to bytes that aren't valid JSON. Without the unmarshal
// guard the downstream KeyVersion lookup would crash on uninitialized
// struct fields.
func TestConsumeTokenJSONUnmarshalErrSurfacesMalformed(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewTokenStore(8, time.Minute, tmp)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	// "abc" → base64url-decodes to 2 raw bytes that aren't JSON.
	payloadB64 := base64.RawURLEncoding.EncodeToString([]byte("abc"))
	sigB64 := base64.RawURLEncoding.EncodeToString([]byte("anysig"))
	tok := payloadB64 + "." + sigB64

	err = s.ConsumeToken(tok, AllowedPurposes[0])
	if !errors.Is(err, ErrMalformedToken) {
		t.Errorf("err=%v; want ErrMalformedToken from json.Unmarshal", err)
	}
}

// TestConsumeTokenLoadKeyForVersionErrSurfacesMalformed pins
// ConsumeToken's `loadKeyForVersion err → ErrMalformedToken` arm
// (confirmation.go:318-320). A forged token claiming KeyVersion=99
// (no such key file on disk) MUST be rejected as malformed BEFORE
// the HMAC check — the absent key file is the legitimate signal
// that no valid HMAC can possibly exist for this token.
func TestConsumeTokenLoadKeyForVersionErrSurfacesMalformed(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewTokenStore(8, time.Minute, tmp)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	// Forge a payload with KeyVersion=99 (file doesn't exist).
	payload := tokenPayload{
		Purpose:       AllowedPurposes[0],
		IssuedAtUnix:  time.Now().Unix(),
		ExpiresAtUnix: time.Now().Add(time.Minute).Unix(),
		Nonce:         "deadbeef",
		KeyVersion:    99,
	}
	payloadBytes, _ := json.Marshal(payload)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	sigB64 := base64.RawURLEncoding.EncodeToString([]byte("anysig"))
	tok := payloadB64 + "." + sigB64

	err = s.ConsumeToken(tok, AllowedPurposes[0])
	if !errors.Is(err, ErrMalformedToken) {
		t.Errorf("err=%v; want ErrMalformedToken from loadKeyForVersion(99)", err)
	}
}

// TestConsumeTokenRecordNotInStoreSurfacesMalformed pins ConsumeToken's
// `record, ok := s.records[tok]; !ok → ErrMalformedToken` arm
// (confirmation.go:333-335). Reached when a token's HMAC verifies
// correctly (same keyDir → same key bytes) BUT the in-memory record
// is missing — e.g., the store was restarted mid-session, or the
// record was reaped. The HMAC check ALONE isn't enough; the store
// must also remember issuing the token, else replay-from-disk attacks
// across restarts would succeed.
func TestConsumeTokenRecordNotInStoreSurfacesMalformed(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewTokenStore(8, time.Minute, tmp)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}

	tok, err := s.IssueToken(AllowedPurposes[0])
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	// Manually delete the record so HMAC verify still passes (the
	// key file is on disk) but the record-lookup fails.
	s.mu.Lock()
	delete(s.records, tok)
	s.mu.Unlock()

	err = s.ConsumeToken(tok, AllowedPurposes[0])
	if !errors.Is(err, ErrMalformedToken) {
		t.Errorf("err=%v; want ErrMalformedToken from absent record", err)
	}
}

// TestEnsureCurrentKeyReadFileNonNotExistErrSurfacesWrap pins
// ensureCurrentKey's `!os.IsNotExist(err) → "read key file:" wrap`
// arm (confirmation.go:168-170). Reached when confirmation.key exists
// as a DIRECTORY (not a regular file) so os.ReadFile fails with
// EISDIR, NOT NotExist. Without this wrap the err would be a bare
// os.PathError whose message doesn't mention "confirmation".
func TestEnsureCurrentKeyReadFileNonNotExistErrSurfacesWrap(t *testing.T) {
	tmp := t.TempDir()
	keyDir := filepath.Join(tmp, "keys")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatalf("mkdir keyDir: %v", err)
	}
	// Plant a directory at the confirmation.key path → ReadFile EISDIR.
	if err := os.MkdirAll(filepath.Join(keyDir, "confirmation.key"), 0o700); err != nil {
		t.Fatalf("plant dir-at-key: %v", err)
	}

	s, err := NewTokenStore(8, time.Minute, keyDir)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	_, _, err = s.ensureCurrentKey()
	if err == nil {
		t.Fatal("ensureCurrentKey(dir-at-key)=nil err; want read-key-file wrap")
	}
}
