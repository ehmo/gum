package dispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ehmo/gum/internal/output/profile"
	"github.com/ehmo/gum/internal/output/tee"
)

// TeeConfig configures the per-profile filesystem tee artifact pipeline run
// by the dispatcher in §9.0 stage 'artifact'. The zero value disables tee
// writes (ProfileDir empty), which is the right default for in-process tests
// that have no $HOME.
type TeeConfig struct {
	// ProfileDir is the absolute path of <data home>/gum/<profile>/. The
	// dispatcher writes artifacts under <ProfileDir>/tee/... and reads or
	// creates <ProfileDir>/tee.secret on first use.
	ProfileDir string

	// Mode is the global tee-mode override applied when the active expression
	// profile leaves TeeMode empty. Accepted values: "" (use profile default),
	// "off", "failures", "always". Profile.TeeMode wins over this when set.
	Mode string

	// RetentionHours is the configured artifact retention window in hours.
	// 0 means use the spec default (24h). Currently consumed by the
	// gum://results/{hash} reverse-lookup scan window, not by writes.
	RetentionHours int
}

// teeArtifact carries the result of a successful tee write so step 8 can
// attach it to ShapedResponse.
type teeArtifact struct {
	Path     string
	Hash     string
	Recovery string // raw profile.Recovery; "resource_link" enables MCP link emission

	// Size is the decompressed byte length of the artifact payload — i.e. the
	// size of the JSON body that `gum://results/<hash>` resources/read will
	// return. The on-disk artifact is gzip-compressed (smaller); we report
	// decompressed because that's what the URI client receives. Spec §9.0
	// line 1846 marks Size as "when known" on the resource_link block.
	Size int64
}

// effectiveTeeMode applies the spec §9 default rule.
//
// Precedence (high → low):
//  1. profile.TeeMode when non-empty.
//  2. TeeConfig.Mode when non-empty (global gum config override).
//  3. "always" if profile.Recovery is set and not "none" (spec default for lossy).
//  4. "off" otherwise.
func effectiveTeeMode(prof *profile.Profile, cfgMode string) string {
	var profMode, recovery string
	if prof != nil {
		profMode = prof.TeeMode
		recovery = prof.Recovery
	}
	if profMode != "" {
		return profMode
	}
	if cfgMode != "" {
		return cfgMode
	}
	if recovery != "" && recovery != "none" {
		return "always"
	}
	return "off"
}

// writeTeeArtifact performs the §9.0 'artifact' stage write when the active
// profile + dispatch config dictate it. Returns nil when no write should
// happen; returns an error only for unrecoverable write failures (caller logs
// and continues — tee failures must never poison a successful response).
func (d *dispatcher) writeTeeArtifact(inv *Invocation, rv *ResolvedVariant, creds *Credentials, resp *Response) (*teeArtifact, error) {
	if d.teeConfig.ProfileDir == "" {
		return nil, nil
	}
	if inv == nil || rv == nil || rv.Variant == nil || resp == nil || len(resp.Body) == 0 {
		return nil, nil
	}
	prof := inv.OutputProfile
	mode := effectiveTeeMode(prof, d.teeConfig.Mode)
	switch mode {
	case "off":
		return nil, nil
	case "failures":
		// Spec §9: "failures" writes only when the upstream HTTP response is
		// 4xx/5xx (or a structured-error envelope). v0.1.0 minimal slice
		// keys off StatusCode; structured-error mapping is handled before
		// this stage and would have early-returned, so falling through to a
		// 2xx here is a healthy execution and must be skipped.
		if resp.StatusCode < 400 {
			return nil, nil
		}
	case "always":
		// fall through
	default:
		// Unknown mode strings are treated as "off" so a misconfigured
		// profile never silently writes artifacts.
		return nil, nil
	}
	return d.teeWrite(inv, rv, creds, resp.Body)
}

// teeWrite computes the §9.0 artifact hash and writes payload. The success path
// and the failure path share it so both land under one directory layout with one
// hash rule.
func (d *dispatcher) teeWrite(inv *Invocation, rv *ResolvedVariant, creds *Credentials, payload []byte) (*teeArtifact, error) {
	secret, err := tee.LoadOrCreateSecret(d.teeConfig.ProfileDir)
	if err != nil {
		return nil, fmt.Errorf("dispatch: tee secret: %w", err)
	}
	fingerprint := ""
	if creds != nil {
		fingerprint = creds.SubjectFingerprint
	}
	if fingerprint == "" {
		fingerprint = inv.AuthSubjectFingerprint
	}
	hash, err := tee.ComputeHash(secret, tee.HashInput{
		OpID:                   inv.OpID,
		VariantIDResolved:      rv.Variant.VariantID,
		Args:                   d.canonicalArgs(inv.Args),
		AuthSubjectFingerprint: fingerprint,
	})
	if err != nil {
		return nil, fmt.Errorf("dispatch: tee hash: %w", err)
	}
	path, err := tee.Write(d.teeConfig.ProfileDir, time.Now().UTC(), inv.OpID, hash, payload)
	if err != nil {
		return nil, fmt.Errorf("dispatch: tee write: %w", err)
	}
	recovery := ""
	if inv.OutputProfile != nil {
		recovery = inv.OutputProfile.Recovery
	}
	return &teeArtifact{
		Path:     path,
		Hash:     hash,
		Recovery: recovery,
		Size:     int64(len(payload)),
	}, nil
}

// writeFailureTee performs the tee write for a failed upstream call. It exists
// because writeTeeArtifact runs at lifecycle step 7c, which the step-7 error
// path returns before reaching, so tee_mode = "failures" never fired in the one
// case the mode is for.
//
// The caller must invoke this only from the step-7 error path.
// docs/expression-profile-dsl.md excludes every error raised in steps 1-6: they
// have no upstream payload, and a zero-byte artifact would corrupt the
// gum://results/{hash} reverse lookup.
//
// "always" writes here too. The enum is ordered off < failures < always, and a
// profile that wants every result artifacted wants the failure artifacted.
//
// Errors are logged, not returned: a tee failure must never replace the upstream
// error the caller needs to see. The artifact path is not surfaced on the error
// envelope because the §7 envelopes are closed; the artifact is found by the
// same directory scan gum://results/{hash} uses.
func (d *dispatcher) writeFailureTee(inv *Invocation, rv *ResolvedVariant, creds *Credentials, resp *Response, execErr error) {
	if d.teeConfig.ProfileDir == "" || inv == nil || rv == nil || rv.Variant == nil {
		return
	}
	switch effectiveTeeMode(inv.OutputProfile, d.teeConfig.Mode) {
	case "failures", "always":
	default:
		return
	}
	payload := failureTeePayload(resp, execErr)
	if len(payload) == 0 {
		return
	}
	art, err := d.teeWrite(inv, rv, creds, payload)
	if err != nil {
		slog.Warn("failure tee artifact write failed", "op_id", inv.OpID, "err", err)
		return
	}
	slog.Info("failure tee artifact written", "op_id", inv.OpID, "path", art.Path, "hash", art.Hash)
}

// failureTeePayload picks the bytes to artifact for a failed call: the upstream
// response body when the adapter kept it, otherwise the mapped error envelope.
//
// A transport failure (DNS, TLS, timeout, body-read EOF) is a listed
// failures-tee trigger and carries no upstream bytes at all, so the envelope is
// the only non-empty payload available.
func failureTeePayload(resp *Response, execErr error) []byte {
	if resp != nil && len(resp.Body) > 0 {
		return resp.Body
	}
	var carrier UpstreamBodyCarrier
	if errors.As(execErr, &carrier) {
		if b := carrier.UpstreamBody(); len(b) > 0 {
			return b
		}
	}
	mapped := mapRateLimited(execErr)
	if mapped == nil {
		return nil
	}
	var se *StructuredError
	if errors.As(mapped, &se) {
		if b, err := json.Marshal(se); err == nil {
			return b
		}
	}
	b, err := json.Marshal(map[string]string{"error": mapped.Error()})
	if err != nil {
		return nil
	}
	return b
}
