package catalog

import (
	"fmt"
	"slices"
	"strings"
)

// The spec §5.8 closed capabilities enum, split by how an atom executes.
//
// GenericCapabilities are the atoms the long-tail dispatcher runs itself.
// TypedExecutorCapabilities are executable, but only through a purpose-built
// adapter. UnsupportedCapabilityClasses are cataloged for search and describe
// and are not claimed executable through raw dispatch.
const (
	CapabilityJSONRequest  Capability = "json_request"
	CapabilityJSONResponse Capability = "json_response"
	CapabilityQueryParams  Capability = "query_params"
	CapabilityPathParams   Capability = "path_params"
	CapabilityPagination   Capability = "pagination"
	CapabilityFieldMask    Capability = "field_mask"

	// CapabilityCodeExecution marks a variant whose body is a script the
	// `code.risor` adapter runs in-process. It never reaches generic dispatch.
	CapabilityCodeExecution Capability = "code_execution"

	CapabilityMediaUploadSimple    Capability = "media_upload_simple"
	CapabilityMediaUploadResumable Capability = "media_upload_resumable"
	CapabilityMediaDownload        Capability = "media_download"
	CapabilityStreaming            Capability = "streaming"
	CapabilityWebsocket            Capability = "websocket"
	CapabilityBatch                Capability = "batch"
	CapabilityPatchNullFields      Capability = "patch_null_fields"
	CapabilityForceSendFields      Capability = "force_send_fields"
	CapabilityCustomCallOptions    Capability = "custom_call_options"
)

// ExperimentalCapabilityPrefix namespaces an atom that is not in the enum yet.
// Spec §5.8 allows one only on a `schema_only` variant.
const ExperimentalCapabilityPrefix = "x-"

// GenericCapabilities are executable through the generic long-tail dispatcher.
var GenericCapabilities = []Capability{
	CapabilityJSONRequest,
	CapabilityJSONResponse,
	CapabilityQueryParams,
	CapabilityPathParams,
	CapabilityPagination,
	CapabilityFieldMask,
	CapabilityLROReturn,
}

// TypedExecutorCapabilities are executable only through a typed executor.
var TypedExecutorCapabilities = []Capability{
	CapabilityCodeExecution,
}

// UnsupportedCapabilityClasses are cataloged but not executable through raw
// long-tail dispatch. A variant declaring one cannot be "full".
var UnsupportedCapabilityClasses = []Capability{
	CapabilityMediaUploadSimple,
	CapabilityMediaUploadResumable,
	CapabilityMediaDownload,
	CapabilityStreaming,
	CapabilityWebsocket,
	CapabilityBatch,
	CapabilityPatchNullFields,
	CapabilityForceSendFields,
	CapabilityCustomCallOptions,
}

// KnownCapabilities is the whole closed enum, in the order §5.8 lists it.
var KnownCapabilities = slices.Concat(GenericCapabilities, TypedExecutorCapabilities, UnsupportedCapabilityClasses)

// CapabilityExecutable reports whether the generic dispatcher or a typed
// executor runs the atom. It answers false for every unsupported class and for
// every atom outside the enum, so an unvalidated atom is never treated as
// runnable.
func CapabilityExecutable(atom Capability) bool {
	return slices.Contains(GenericCapabilities, atom) || slices.Contains(TypedExecutorCapabilities, atom)
}

// CapabilityKnown reports whether the atom is in the closed §5.8 enum.
func CapabilityKnown(atom Capability) bool {
	return slices.Contains(KnownCapabilities, atom)
}

// experimentalCapability reports whether the atom uses the `x-` escape hatch
// with a non-empty suffix. A bare "x-" names nothing and is rejected as
// unknown, matching AuthComponentKind.Valid.
func experimentalCapability(atom Capability) bool {
	return strings.HasPrefix(atom, ExperimentalCapabilityPrefix) && len(atom) > len(ExperimentalCapabilityPrefix)
}

// validateCapabilities enforces the spec §5.8 closed-enum rule. Nothing
// checked atom names before this: an unknown atom passed catalog validation,
// reached `gum.describe_op`, and told the caller about a capability class no
// executor implements.
//
// The `x-` escape hatch is narrow on purpose. An experimental atom is
// describable metadata, so the variant that carries it must be `schema_only`,
// which the §5.8 dispatch gate refuses before any upstream request.
func (v Variant) validateCapabilities(opID string) error {
	for _, atom := range v.Capabilities {
		if CapabilityKnown(atom) {
			continue
		}
		if !experimentalCapability(atom) {
			return fmt.Errorf("op %s: variant %s: capability %q: %w", opID, v.VariantID, atom, ErrUnknownCapability)
		}
		if v.ExecutionSupport != ExecutionSupportSchemaOnly {
			return fmt.Errorf("op %s: variant %s: capability %q: %w", opID, v.VariantID, atom, ErrExperimentalCapabilityNotSchemaOnly)
		}
	}
	return nil
}
