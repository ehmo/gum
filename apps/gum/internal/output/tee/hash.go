package tee

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"github.com/ehmo/gum/internal/output/jcs"
)

// HashInput is the principal-scoped tuple over which the tee artifact hash
// is computed. Spec §9.0 line 1846:
//
//	hash = HMAC-SHA-256(tee_secret,
//	    op_id + ":" + variant_id_resolved + ":" + args_canonical +
//	    ":" + auth_subject_fingerprint)
//
// Two calls with the same op, resolved variant, args, and credential subject
// in the same profile on the same day share a path (profile+principal-
// scoped content-addressed dedup); cross-profile and cross-principal handles
// are not reusable.
type HashInput struct {
	OpID                    string
	VariantIDResolved       string
	Args                    any // arbitrary tree; serialised via JCS canonical form
	AuthSubjectFingerprint  string
}

// ComputeHash returns the hex-encoded (lowercase) SHA-256 HMAC of the
// canonical hash input. The secret MUST be the raw 32-byte tee.secret key
// returned by LoadOrCreateSecret.
//
// The args sub-string is the JCS-canonical (RFC 8785) serialization of
// HashInput.Args. JCS guarantees byte-identical output across runs for any
// JSON-isomorphic Go tree, which is the property §10.0 calls out as
// "canonical serialization algorithm".
func ComputeHash(secret []byte, in HashInput) (string, error) {
	argsCanon, err := jcs.Marshal(in.Args)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	// Spec §9.0: literal colon separator between each component. Components
	// are written in the documented order; we never re-encode them as JSON
	// to avoid quoting differences leaking into the hash.
	_, _ = mac.Write([]byte(in.OpID))
	_, _ = mac.Write([]byte{':'})
	_, _ = mac.Write([]byte(in.VariantIDResolved))
	_, _ = mac.Write([]byte{':'})
	_, _ = mac.Write(argsCanon)
	_, _ = mac.Write([]byte{':'})
	_, _ = mac.Write([]byte(in.AuthSubjectFingerprint))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
