// Package dispatch — BLAKE3 source-rehash confirmation token signing (spec §6.1.2).
//
// IssueConfirmationToken / VerifyConfirmationToken are a distinct binding-aware API
// from the existing TokenStore / IssueToken / ConsumeToken (HMAC-SHA256, store-backed).
// The two APIs coexist without sharing purpose enums or signing keys.
package dispatch

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ehmo/gum/internal/fsatomic"
	"lukechampine.com/blake3"
)

// ----------------------------------------------------------------------------
// PUBLIC API
// ----------------------------------------------------------------------------

// ConfirmationParams holds all fields bound into a confirmation token.
type ConfirmationParams struct {
	OpID                 string
	VariantID            string
	ArgsHash             string
	ResourceKey          string // optional; empty = no resource binding
	AuthFingerprint      string
	ProfileName          string
	Scope                string        // JCS-canonical destructive scope or "[]"
	Purpose              string        // closed enum: see ConfirmationPurpose* constants
	TTL                  time.Duration // consumed by IssueConfirmationToken only
	ReplayStoreDir       string        // optional profile data dir for durable replay markers
	RequireDurableReplay bool          // fail closed when ReplayStoreDir cannot be used
}

// Closed purpose enum for IssueConfirmationToken / VerifyConfirmationToken.
const (
	ConfirmationPurposeDestructive = "gum_confirm_destructive"
	ConfirmationPurposeWrite       = "gum_confirm_write"
	ConfirmationPurposeCodeWrite   = "gum_code_write"
	ConfirmationPurposeCodeDestroy = "gum_code_destructive"
)

// Default TTL constants for each confirmation purpose (spec §6.1.2 line 1128).
// Both are 5 minutes — the spec uses a single unified TTL for all confirmation tokens.
const (
	DefaultWriteTokenTTL       = 5 * time.Minute // spec §6.1.2 line 1128
	DefaultDestructiveTokenTTL = 5 * time.Minute // spec §6.1.2 line 1128
)

// DefaultTTLForPurpose returns the spec-defined default TTL for the given confirmation purpose.
// Both ConfirmationPurposeWrite and ConfirmationPurposeDestructive return 5 minutes (spec §6.1.2 line 1128).
// Unknown purposes also return DefaultWriteTokenTTL as a safe fallback.
func DefaultTTLForPurpose(purpose string) time.Duration {
	switch purpose {
	case ConfirmationPurposeDestructive:
		return DefaultDestructiveTokenTTL
	default:
		return DefaultWriteTokenTTL
	}
}

// confirmationPurposeSet is the set form of the closed purpose enum for O(1) lookup.
var confirmationPurposeSet = map[string]bool{
	ConfirmationPurposeDestructive: true,
	ConfirmationPurposeWrite:       true,
	ConfirmationPurposeCodeWrite:   true,
	ConfirmationPurposeCodeDestroy: true,
}

// Reason string constants for CONFIRMATION_TOKEN_INVALID detail["reason"].
const (
	tokenReasonMissing        = "missing"
	tokenReasonExpired        = "expired"
	tokenReasonReplayed       = "replayed"
	tokenReasonMismatch       = "mismatch"
	tokenReasonUnknownPurpose = "unknown_purpose"
	tokenReasonCLIUnsupported = "cli_unsupported"
	tokenReasonReplayStore    = "replay_store_unavailable"
)

// IssueConfirmationToken creates a signed token bound to all params.
func IssueConfirmationToken(params ConfirmationParams) (string, error) {
	now := time.Now()
	issuedAt := now.Unix()
	expiry := now.Add(params.TTL).UnixNano() // nanoseconds for sub-second TTLs

	bindingHash := computeBindingHash(params, getSourceHash())
	sig := computeSignature(getSigningKey(), bindingHash, issuedAt, expiry, params.Purpose)

	tok := fmt.Sprintf("%s.%d.%d.%s.%s.%s",
		tokenVersion,
		issuedAt,
		expiry,
		params.Purpose,
		hex.EncodeToString(bindingHash),
		hex.EncodeToString(sig),
	)
	return tok, nil
}

// VerifyConfirmationToken validates token against expected params.
// Returns *StructuredError with ErrCodeConfirmationTokenInvalid and a "reason" detail:
//   - "missing"         — empty or unparseable token
//   - "unknown_purpose" — purpose not in closed enum (checked BEFORE HMAC)
//   - "expired"         — token TTL elapsed
//   - "mismatch"        — binding or signature mismatch
//   - "replayed"        — token already used
func VerifyConfirmationToken(token string, params ConfirmationParams) error {
	// Step 1: empty token → missing.
	if token == "" {
		return tokenInvalidErr(tokenReasonMissing)
	}

	// Step 3 (spec §6.1.2): purpose enum checked BEFORE format parse and HMAC to prevent timing leaks.
	if !confirmationPurposeSet[params.Purpose] {
		return tokenInvalidErr(tokenReasonUnknownPurpose)
	}

	// Step 2: parse token; malformed → missing.
	parts := strings.SplitN(token, ".", 6)
	if len(parts) != 6 || parts[0] != tokenVersion {
		return tokenInvalidErr(tokenReasonMissing)
	}
	var issuedAt, expiry int64
	if _, err := fmt.Sscanf(parts[1], "%d", &issuedAt); err != nil {
		return tokenInvalidErr(tokenReasonMissing)
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &expiry); err != nil {
		return tokenInvalidErr(tokenReasonMissing)
	}
	tokenPurpose, bindingHashHex, sigHex := parts[3], parts[4], parts[5]

	// Step 4: expiry check (nanosecond precision).
	if time.Now().UnixNano() > expiry {
		return tokenInvalidErr(tokenReasonExpired)
	}

	// Step 5: decode hex fields; decode failure → mismatch.
	actualSig, err := hex.DecodeString(sigHex)
	if err != nil {
		return tokenInvalidErr(tokenReasonMismatch)
	}
	actualBindingHash, err := hex.DecodeString(bindingHashHex)
	if err != nil {
		return tokenInvalidErr(tokenReasonMismatch)
	}

	// Step 5 (cont): recompute expected values from params.
	expectedBindingHash := computeBindingHash(params, getSourceHash())
	expectedSig := computeSignature(getSigningKey(), expectedBindingHash, issuedAt, expiry, tokenPurpose)

	// Step 5 (cont): purpose in token must match params.Purpose.
	if tokenPurpose != params.Purpose {
		return tokenInvalidErr(tokenReasonMismatch)
	}

	// Step 6: constant-time signature comparison.
	if subtle.ConstantTimeCompare(actualSig, expectedSig) != 1 {
		return tokenInvalidErr(tokenReasonMismatch)
	}

	// Step 6 (cont): binding hash comparison (defence in depth).
	if subtle.ConstantTimeCompare(actualBindingHash, expectedBindingHash) != 1 {
		return tokenInvalidErr(tokenReasonMismatch)
	}

	// Step 7: replay cache — reject reuse. Dispatch uses durable profile-scoped
	// markers so one-shot CLI processes cannot reuse a token; tests and
	// embedders that omit ReplayStoreDir keep the in-memory fallback.
	expiryTime := time.Unix(0, expiry)
	if params.RequireDurableReplay || params.ReplayStoreDir != "" {
		replayed, err := durableReplaySeen(params.ReplayStoreDir, sigHex, expiryTime)
		if err != nil {
			return tokenInvalidErr(tokenReasonReplayStore)
		}
		if replayed {
			return tokenInvalidErr(tokenReasonReplayed)
		}
	} else if globalReplayCache.seen(sigHex, expiryTime) {
		return tokenInvalidErr(tokenReasonReplayed)
	}

	return nil
}

// ----------------------------------------------------------------------------
// TOKEN FORMAT
// ----------------------------------------------------------------------------

// Token wire format: v1.<issuedAt_unix>.<expiry_unix_nano>.<purpose>.<bindingHashHex>.<sigHex>
// Six dot-separated fields; purpose may not contain ".".

const tokenVersion = "v1"

func computeBindingHash(p ConfirmationParams, sourceHash string) []byte {
	// Binding tuple per spec §4.1 / §6.1:
	//   - Write tier:       opID, variantID, argsHash, resourceKey, authFingerprint, sourceHash
	//                       (scope excluded — spec §4.1)
	//   - Destructive tier: opID, variantID, argsHash, resourceKey, authFingerprint, scope, sourceHash
	const sep = "\x1f"
	fields := []string{p.OpID, p.VariantID, p.ArgsHash, p.ResourceKey, p.AuthFingerprint, p.ProfileName}
	if p.Purpose != ConfirmationPurposeWrite {
		// Destructive (and any future tier) includes destructive_scope_canonical.
		fields = append(fields, p.Scope)
	}
	fields = append(fields, sourceHash)
	h := blake3.Sum256([]byte(strings.Join(fields, sep)))
	return h[:]
}

func computeSignature(key [32]byte, bindingHash []byte, issuedAt, expiry int64, purpose string) []byte {
	// BLAKE3-keyed(key, bindingHash || 0x1f || issuedAt || 0x1f || expiry || 0x1f
	// || purpose). bindingHash is a fixed 32 bytes so needs no separator, but
	// issuedAt/expiry/purpose are variable-length: without the 0x1f field
	// separators (issuedAt=1, expiry=23) would hash identically to
	// (issuedAt=12, expiry=3) — a MAC-input canonicalization defect.
	h := blake3.New(32, key[:])
	_, _ = h.Write(bindingHash)
	_, _ = fmt.Fprintf(h, "\x1f%d\x1f%d\x1f%s", issuedAt, expiry, purpose)
	return h.Sum(nil)
}

func tokenInvalidErr(reason string) *StructuredError {
	return NewStructuredError(ErrCodeConfirmationTokenInvalid,
		fmt.Sprintf("confirmation token invalid: %s", reason)).
		WithDetail("reason", reason)
}

// ----------------------------------------------------------------------------
// REPLAY CACHE
// ----------------------------------------------------------------------------

type replayCache struct {
	mu      sync.Mutex
	entries map[string]time.Time // sigHex → expiry
}

var globalReplayCache = &replayCache{
	entries: make(map[string]time.Time),
}

// MaxReplayCacheEntries is the maximum number of entries the replay cache will hold.
// Entries beyond this cap are evicted (oldest-first) to bound memory usage.
// Exported so that tests (gum-1otq.5) can assert the cap is respected.
const MaxReplayCacheEntries = 1024

// GlobalReplayCacheSize returns the current number of entries in the global replay cache.
// Exported for test observability (gum-1otq.5 bounded-size assertions).
func GlobalReplayCacheSize() int {
	globalReplayCache.mu.Lock()
	defer globalReplayCache.mu.Unlock()
	return len(globalReplayCache.entries)
}

// ResetReplayCacheForTest clears all entries in the global replay cache.
// Test-only helper; ensures inter-test isolation so tests do not pollute each other's state.
func ResetReplayCacheForTest() {
	globalReplayCache.mu.Lock()
	defer globalReplayCache.mu.Unlock()
	globalReplayCache.entries = make(map[string]time.Time)
}

// seen returns true (replay) if sig is already in the cache; otherwise records it.
// Sweeps expired entries in the same O(n) pass. If the cache is still at or
// over MaxReplayCacheEntries after sweeping, evicts the entry with the earliest
// expiry to enforce the size bound.
func (rc *replayCache) seen(sig string, expiry time.Time) bool {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	now := time.Now()
	for k, exp := range rc.entries {
		if now.After(exp) {
			delete(rc.entries, k)
		}
	}

	if _, exists := rc.entries[sig]; exists {
		return true
	}

	for len(rc.entries) >= MaxReplayCacheEntries {
		var oldestKey string
		var oldestExp time.Time
		for k, exp := range rc.entries {
			if oldestKey == "" || exp.Before(oldestExp) {
				oldestKey = k
				oldestExp = exp
			}
		}
		delete(rc.entries, oldestKey)
	}

	rc.entries[sig] = expiry
	return false
}

func durableReplaySeen(profileDir, sigHex string, expiry time.Time) (bool, error) {
	if profileDir == "" {
		return false, fmt.Errorf("confirmation: replay store dir is empty")
	}
	dir := filepath.Join(profileDir, "confirmation-replay")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("confirmation: create replay store: %w", err)
	}
	sweepExpiredReplayMarkers(dir)

	path := filepath.Join(dir, sigHex)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("confirmation: create replay marker: %w", err)
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.WriteString(strconv.FormatInt(expiry.UnixNano(), 10)); err != nil {
		return false, fmt.Errorf("confirmation: write replay marker: %w", err)
	}
	if err := f.Sync(); err != nil {
		return false, fmt.Errorf("confirmation: sync replay marker: %w", err)
	}
	ok = true
	return false, nil
}

func sweepExpiredReplayMarkers(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := time.Now().UnixNano()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		expiry, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil || expiry < now {
			_ = os.Remove(path)
		}
	}
}

// ----------------------------------------------------------------------------
// SOURCE HASH
// ----------------------------------------------------------------------------

// confirmationSourceHash is the source-rehash value bound into every token.
// Default sentinel for v0.1.0; SetSourceHashForTest replaces it for testing.
var (
	confirmationSourceHash   = "spec-v1.34-model-free"
	confirmationSourceHashMu sync.RWMutex
)

// SetSourceHashForTest replaces the source-hash used for token binding.
// Test-only helper; production code uses an embedded build-time hash.
func SetSourceHashForTest(hash string) {
	confirmationSourceHashMu.Lock()
	defer confirmationSourceHashMu.Unlock()
	confirmationSourceHash = hash
}

func getSourceHash() string {
	confirmationSourceHashMu.RLock()
	defer confirmationSourceHashMu.RUnlock()
	return confirmationSourceHash
}

// Per-user signing key, loaded once at first use. It MUST be stable across
// processes: a destructive confirmation is issued by one `gum call` process and
// verified by a SEPARATE one, so a process-random key (the original bug) made
// every CLI confirm fail with reason=mismatch. The key is persisted 0600 at
// <XDG_DATA_HOME>/gum/confirmation-signing.key and shared by all gum processes
// for this user.
var (
	confirmationSigningKey     [32]byte
	confirmationSigningKeyOnce sync.Once
)

func getSigningKey() [32]byte {
	confirmationSigningKeyOnce.Do(func() {
		if k, ok := loadOrCreateSigningKey(); ok {
			confirmationSigningKey = k
			return
		}
		// Degraded fallback when the key file can't be read/created (read-only
		// FS, no HOME): an ephemeral per-process key. Cross-process CLI confirm
		// won't work in this mode, but a long-running server still functions.
		if _, err := rand.Read(confirmationSigningKey[:]); err != nil {
			panic(fmt.Sprintf("confirmation_token: generate signing key: %v", err))
		}
	})
	return confirmationSigningKey
}

// signingKeyPath returns the per-user persistent location of the confirmation
// signing key, honoring XDG_DATA_HOME and falling back to ~/.local/share.
func signingKeyPath() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "gum", "confirmation-signing.key"), nil
}

// loadOrCreateSigningKey returns the persisted 32-byte signing key, generating
// and writing it (0600) on first use. An O_EXCL create resolves the rare
// first-run race between concurrent processes: the loser reads the winner's key.
func loadOrCreateSigningKey() ([32]byte, bool) {
	var key [32]byte
	path, err := signingKeyPath()
	if err != nil {
		return key, false
	}
	// Read with O_NOFOLLOW so a symlink swapped in for the key file can't
	// redirect the read to an attacker-chosen 32-byte file, letting them forge
	// confirmation tokens (review gum-t8x1). Full content-integrity (a keychain
	// MAC) is deferred — it would pull the keychain dependency into the pure
	// dispatch kernel; local write access to the data dir is already a severe
	// compromise.
	if f, oerr := fsatomic.OpenNoFollow(path); oerr == nil {
		b, rerr := io.ReadAll(f)
		_ = f.Close()
		if rerr == nil && len(b) == len(key) {
			copy(key[:], b)
			return key, true
		}
	}
	if _, err := rand.Read(key[:]); err != nil {
		return key, false
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return key, false
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		// Another process created it first (or a non-race error): adopt the
		// on-disk key so both processes agree.
		if b, rerr := os.ReadFile(path); rerr == nil && len(b) == len(key) {
			copy(key[:], b)
			return key, true
		}
		return key, false
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(key[:]); err != nil {
		// A partial/failed write leaves a zero-byte file that ReadFile later
		// returns as len != 32 while the O_EXCL create keeps failing with
		// IsExist — permanently wedging cross-process confirmation onto a
		// per-process ephemeral key. Remove it so the next call recreates it.
		_ = os.Remove(path)
		return key, false
	}
	return key, true
}
