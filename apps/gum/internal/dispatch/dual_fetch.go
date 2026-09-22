package dispatch

import (
	"context"

	"github.com/ehmo/gum/internal/output/profile"
)

// dualFetchArg is the universal Google upstream projection argument. Stage 1
// (lifecycle step 3c) injects the profile's field_mask under this key, so
// deleting it is what makes the recovery request unmasked.
const dualFetchArg = "fields"

// dualFetchAuditKey marks the second, unmasked request in the §11 audit log.
// Spec §9.1 reserves it for that request alone: the shaped first request is
// an ordinary masked call and carries no such key.
const dualFetchAuditKey = "dual_fetch"

// dualFetchWanted reports whether this invocation asked for the §9.1 second
// unmasked fetch, has a mask on the wire worth undoing, and has somewhere to
// put the result.
//
// Each clause is a reason the request would cost a rate-limit token, upstream
// quota and an audit entry for a body nobody reads:
//
//   - No `fields` arg. Stage 1 may leave none: field_mask_mode="none",
//     --no-field-mask, or neither profile nor variant supplied a default. The
//     "unmasked" request would return the same bytes as the first.
//   - No artifact. §9.1 exists to feed §9.0 stage 9. With no profile dir, or
//     tee_mode="off", the unmasked body is fetched and dropped.
//   - tee_mode="failures". That mode writes only for a 4xx/5xx upstream, and
//     a failed first request returns before the second fetch is reached, so
//     the artifact slot this fetch fills is never written.
//
// The §9.1 eligibility gate (risk_class=read AND annotations.idempotent=true)
// is separate and runs first, at lifecycle step 3b.
func (d *dispatcher) dualFetchWanted(inv *Invocation) bool {
	if inv == nil || inv.OutputProfile == nil {
		return false
	}
	if inv.OutputProfile.FieldMaskMode != profile.FieldMaskModeDualFetch {
		return false
	}
	if mask, _ := inv.Args[dualFetchArg].(string); mask == "" {
		return false
	}
	if d.teeConfig.ProfileDir == "" {
		return false
	}
	return effectiveTeeMode(inv.OutputProfile, d.teeConfig.Mode) == "always"
}

// dualFetch performs the §9.1 second, unmasked upstream request.
//
// It is a full request, not a replay: it takes its own rate-limit token and
// writes its own audit entry, because spec §9.1 counts both requests against
// rate limits, quota and the audit log. The returned entry is the caller's to
// append, so the log keeps request order (masked first, unmasked second) even
// though the masked request's entry is only written at step 9.
//
// The invocation is cloned rather than edited. inv.Args feeds the §10.3 cache
// key, the §11 args_hash and the §9.0 tee hash; dropping `fields` in place
// would have re-pointed all three at an invocation the caller never made.
func (d *dispatcher) dualFetch(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) (*Response, map[string]any, error) {
	unmasked := *inv
	unmasked.Args = make(map[string]any, len(inv.Args))
	for k, v := range inv.Args {
		if k == dualFetchArg {
			continue
		}
		unmasked.Args[k] = v
	}

	if err := d.tokenBucketStep(ctx, &unmasked, rv); err != nil {
		return nil, nil, mapRateLimited(err)
	}
	resp, err := d.executeAdapter(ctx, &unmasked, rv, creds)
	if err != nil {
		return nil, nil, err
	}

	entry := successAuditEntry(&unmasked, rv, d.canonicalArgs(unmasked.Args))
	entry[dualFetchAuditKey] = true
	return resp, entry, nil
}

// dualFetchResult carries one completed second fetch between the lifecycle's
// execute step and its artifact + audit steps.
type dualFetchResult struct {
	// Resp is the unmasked body that feeds §9.0 stage 9 in place of the
	// shaped first-fetch tree. Nil when the fetch failed.
	Resp *Response

	// Audit is the §11 entry for the unmasked request, appended after the
	// masked request's entry. Nil when the fetch failed.
	Audit map[string]any

	// Warning is non-empty when the second fetch failed. The first request
	// succeeded, so the caller still gets its response; the warning says the
	// promised pre-mask recovery handle is missing and why.
	Warning string
}

// runDualFetch wraps dualFetch with the failure policy.
//
// A failed recovery fetch does not fail the invocation: the shaped request
// already succeeded and the caller can use it. It does not silently fall back
// to teeing the masked body either. Writing the masked tree under a
// field_mask_mode="dual_fetch" profile would hand back a `full_result_path`
// that claims to hold pre-mask data and does not, which is worse than having
// no handle: the caller cannot tell the difference by reading it.
func (d *dispatcher) runDualFetch(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) *dualFetchResult {
	resp, entry, err := d.dualFetch(ctx, inv, rv, creds)
	if err != nil {
		d.log().Warn("dual_fetch recovery request failed; no artifact written",
			"op_id", inv.OpID, "err", err)
		return &dualFetchResult{
			Warning: "field_mask_mode=\"dual_fetch\": the unmasked recovery request failed (" +
				err.Error() + "); no full_result artifact was written for this call",
		}
	}
	return &dualFetchResult{Resp: resp, Audit: entry}
}

// teeSource picks the payload §9.0 stage 9 writes. A successful second fetch
// replaces the masked response, per spec §9.1: "the artifact captures the
// UNMASKED second fetch, not the shaped first-fetch tree".
//
// A failed second fetch returns nil, which writeTeeArtifact treats as "write
// nothing". Teeing the masked body instead would answer a pre-mask recovery
// promise with post-mask data, and the caller cannot tell the two apart by
// reading the artifact. No artifact plus a warning is the honest answer.
func (r *dualFetchResult) teeSource(masked *Response) *Response {
	if r == nil {
		return masked
	}
	return r.Resp
}

// warn projects a failed second fetch onto the outgoing response, so a caller
// that asked for pre-mask recovery learns the handle is missing instead of
// finding no full_result_path and guessing why.
func (r *dualFetchResult) warn(shaped *ShapedResponse) {
	if r == nil || shaped == nil || r.Warning == "" {
		return
	}
	shaped.ValidationWarnings = append(shaped.ValidationWarnings, r.Warning)
}

// appendDualFetchAudit writes the unmasked request's §11 entry. Call it after
// the masked request's entry so the log reads in request order.
func (d *dispatcher) appendDualFetchAudit(r *dualFetchResult) {
	if r == nil || r.Audit == nil || d.auditSink == nil {
		return
	}
	d.auditSink.Append(r.Audit)
}
