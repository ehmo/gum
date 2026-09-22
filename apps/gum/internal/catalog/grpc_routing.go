package catalog

import (
	"fmt"
	"regexp"
	"strings"
)

// routingHeaderPath is the closed alphabet from the catalog-abi.md
// `routing_headers` invariant rule 1: a non-empty dotted field path, lowercase
// first letter, no array indexing syntax.
var routingHeaderPath = regexp.MustCompile(`^[a-z][a-zA-Z0-9_.]{0,127}$`)

// validateRoutingHeaders enforces rules 1 to 4 of the catalog-abi.md
// `routing_headers` invariant for one grpc-sdk variant. Rule 5 (stability
// under regeneration) is an override-diffing concern and is not checked here.
//
// The rules apply to grpc-sdk variants only. `routing_headers` is a grpc-sdk
// binding field: no other backend kind reads it, and the ABI defines the three
// build failures for grpc-sdk alone.
func (op *Op) validateRoutingHeaders(v Variant) error {
	if v.Binding == nil || v.BackendKind != BackendKindGRPCSDK {
		return nil
	}

	headers := v.Binding.RoutingHeaders
	if headers == nil {
		return nil
	}

	// Rule 4: absence is the correct encoding for a variant that needs no
	// routing headers. An empty array is a curator mistake.
	if len(headers) == 0 {
		return fmt.Errorf("op %s: variant %s: %w", op.OpID, v.VariantID, ErrGRPCRoutingHeaderNotRequired)
	}

	schema, err := RequestSchema(*op)
	if err != nil {
		return fmt.Errorf("op %s: variant %s: routing_headers: %w", op.OpID, v.VariantID, err)
	}

	seen := make(map[string]struct{}, len(headers))
	for _, h := range headers {
		// Rule 1. The alphabet alone still admits `a..b` and `a.`, which name no
		// field, so the two degenerate forms are rejected with it.
		if !routingHeaderPath.MatchString(h) || strings.Contains(h, "..") || strings.HasSuffix(h, ".") {
			return fmt.Errorf("op %s: variant %s: routing_headers %q: %w", op.OpID, v.VariantID, h, ErrGRPCRoutingHeaderInvalid)
		}

		// Rule 3.
		if _, dup := seen[h]; dup {
			return fmt.Errorf("op %s: variant %s: routing_headers %q: %w", op.OpID, v.VariantID, h, ErrGRPCRoutingHeaderDuplicate)
		}
		seen[h] = struct{}{}

		// Rule 2.
		if !requestSchemaHasPath(schema, h) {
			return fmt.Errorf("op %s: variant %s: routing_headers %q: %w", op.OpID, v.VariantID, h, ErrGRPCRoutingHeaderNotFound)
		}
	}

	return nil
}

// requestSchemaHasPath reports whether a dotted path resolves to a field in the
// derived request schema. A nil schema (an op that declares no request fields)
// resolves nothing.
//
// RequestFields is flat, so the schema has exactly one level of properties.
// That fixes what a multi-segment path can mean: the head must name an object
// field, and the tail is accepted unchecked, because the schema records that
// the field is an object but not what is inside it. Rejecting every nested
// path instead would fail variants the ABI allows. Any other field kind is
// terminal, including an array: gRPC routing headers do not address into
// repeated fields.
func requestSchemaHasPath(schema map[string]any, path string) bool {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return false
	}

	head, _, nested := strings.Cut(path, ".")
	field, ok := props[head].(map[string]any)
	if !ok {
		return false
	}
	if !nested {
		return true
	}

	fieldType, _ := field["type"].(string)
	return fieldType == "object"
}
