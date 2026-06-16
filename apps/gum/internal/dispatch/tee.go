package dispatch

import (
	"fmt"
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
	path, err := tee.Write(d.teeConfig.ProfileDir, time.Now().UTC(), inv.OpID, hash, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dispatch: tee write: %w", err)
	}
	recovery := ""
	if prof != nil {
		recovery = prof.Recovery
	}
	return &teeArtifact{
		Path:     path,
		Hash:     hash,
		Recovery: recovery,
		Size:     int64(len(resp.Body)),
	}, nil
}
