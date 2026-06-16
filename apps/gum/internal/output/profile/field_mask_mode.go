// Package profile — field_mask_mode runtime gate (spec §9.1).
//
// The catalog generator rejects field_mask_mode="dual_fetch" on every
// write/destructive or non-idempotent variant at build time. This file pins
// the equivalent runtime defense: a profile authored independently from the
// catalog (e.g. a user-global override) MUST NOT activate dual_fetch on a
// variant the gate doesn't allow. ValidateDualFetchGate is the pure helper;
// internal/dispatch calls it after resolveVariant when the resolved profile
// declares FieldMaskMode == "dual_fetch".

package profile

import (
	"errors"
	"fmt"

	"github.com/ehmo/gum/internal/catalog"
)

// FieldMaskMode constants pin the closed enum exposed by spec §9.1. The empty
// string is treated as the default ("upstream") by all downstream consumers.
const (
	FieldMaskModeUpstream  = "upstream"
	FieldMaskModeDualFetch = "dual_fetch"
	FieldMaskModeNone      = "none"
)

// ErrDualFetchGateRejected fires when ValidateDualFetchGate refuses
// field_mask_mode="dual_fetch" for a variant that is not read+idempotent.
// Callers in the dispatch kernel wrap this into a structured INVALID_ARGS
// error so the MCP envelope carries the failing field name.
var ErrDualFetchGateRejected = errors.New("PROFILE_DUAL_FETCH_GATE_REJECTED")

// ValidateDualFetchGate returns nil when mode is empty / not dual_fetch, OR
// when the variant satisfies the spec §9.1 gate (risk_class="read" AND
// annotations.idempotent=true). Any other combination returns
// ErrDualFetchGateRejected wrapped with the concrete reason so operators can
// see which constraint failed.
//
// The variant must be non-nil when mode == "dual_fetch"; callers guard against
// passing a nil variant before the gate is evaluated (resolveVariant always
// populates it on the success path).
func ValidateDualFetchGate(mode string, variant *catalog.Variant) error {
	if mode == "" || mode != FieldMaskModeDualFetch {
		return nil
	}
	if variant == nil {
		return fmt.Errorf("%w: nil variant rejected before gate check", ErrDualFetchGateRejected)
	}
	if variant.RiskClass != catalog.RiskClassRead {
		return fmt.Errorf("%w: variant %q has risk_class=%q (dual_fetch requires risk_class=read)",
			ErrDualFetchGateRejected, variant.VariantID, variant.RiskClass)
	}
	if variant.Annotations == nil || !variant.Annotations.Idempotent {
		return fmt.Errorf("%w: variant %q has annotations.idempotent=false (dual_fetch requires idempotent=true)",
			ErrDualFetchGateRejected, variant.VariantID)
	}
	return nil
}
