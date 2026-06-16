package plugins

import (
	"errors"
	"fmt"

	"github.com/ehmo/gum/internal/catalog"
)

// ErrPluginBindingInvalid is the host-side rendering of spec §11 sentinel
// PLUGIN_BINDING_INVALID. Spec §8 + docs/catalog-abi.md require that plugin
// variants materialize a binding object whose selector fields match the
// declared backend_kind: `mcp-plugin` needs `tool_name`; the bundled Shape 2
// `grpc-plugin` ABI fixture needs `rpc_service` + `rpc_method`. Missing or
// blank selector fields surface this sentinel before subprocess start.
//
// The third-party Shape 2 install gate (spec §8 line 1510) wins over this
// validator and returns PLUGIN_SHAPE_UNSUPPORTED instead, so callers that
// gate by shape MUST run that check first.
var ErrPluginBindingInvalid = errors.New("PLUGIN_BINDING_INVALID")

// ValidateBinding enforces the spec §8 backend-kind selector contract on a
// resolved plugin binding. It is invoked at plugin install (writing
// plugin-catalog.json) and at dispatch (re-checking the cached binding) to
// stop a malformed entry from reaching the subprocess.
//
// Only plugin backend kinds are handled here; non-plugin kinds (REST, gRPC
// SDK, etc.) carry their own selectors and validate elsewhere.
func ValidateBinding(b *catalog.Binding, kind catalog.BackendKind) error {
	if b == nil {
		return fmt.Errorf("%w: nil binding", ErrPluginBindingInvalid)
	}
	switch kind {
	case catalog.BackendKindMCPPlugin:
		if b.ToolName == "" {
			return fmt.Errorf("%w: mcp-plugin requires tool_name", ErrPluginBindingInvalid)
		}
	case catalog.BackendKindGRPCPlugin:
		if b.RPCService == "" {
			return fmt.Errorf("%w: grpc-plugin requires rpc_service", ErrPluginBindingInvalid)
		}
		if b.RPCMethod == "" {
			return fmt.Errorf("%w: grpc-plugin requires rpc_method", ErrPluginBindingInvalid)
		}
	default:
		return fmt.Errorf("%w: kind %q is not a plugin backend", ErrPluginBindingInvalid, kind)
	}
	return nil
}
