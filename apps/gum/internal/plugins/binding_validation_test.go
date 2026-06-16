package plugins_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins"
)

// TestPluginBindingSchema covers docs/test-matrix.md line 89: mcp-plugin
// requires tool_name; grpc-plugin requires rpc_service + rpc_method; missing
// fields return PLUGIN_BINDING_INVALID before subprocess start.
func TestPluginBindingSchema(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		binding *catalog.Binding
		kind    catalog.BackendKind
		wantErr bool
	}{
		{
			name:    "mcp-plugin with tool_name ok",
			binding: &catalog.Binding{ToolName: "search"},
			kind:    catalog.BackendKindMCPPlugin,
		},
		{
			name:    "mcp-plugin missing tool_name rejected",
			binding: &catalog.Binding{},
			kind:    catalog.BackendKindMCPPlugin,
			wantErr: true,
		},
		{
			name:    "grpc-plugin with rpc_service+rpc_method ok",
			binding: &catalog.Binding{RPCService: "Foo", RPCMethod: "Bar"},
			kind:    catalog.BackendKindGRPCPlugin,
		},
		{
			name:    "grpc-plugin missing rpc_service rejected",
			binding: &catalog.Binding{RPCMethod: "Bar"},
			kind:    catalog.BackendKindGRPCPlugin,
			wantErr: true,
		},
		{
			name:    "grpc-plugin missing rpc_method rejected",
			binding: &catalog.Binding{RPCService: "Foo"},
			kind:    catalog.BackendKindGRPCPlugin,
			wantErr: true,
		},
		{
			name:    "nil binding rejected",
			binding: nil,
			kind:    catalog.BackendKindMCPPlugin,
			wantErr: true,
		},
		{
			name:    "non-plugin kind rejected",
			binding: &catalog.Binding{},
			kind:    catalog.BackendKindRawHTTP,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := plugins.ValidateBinding(tc.binding, tc.kind)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateBinding: want error, got nil")
				}
				if !errors.Is(err, plugins.ErrPluginBindingInvalid) {
					t.Fatalf("ValidateBinding: want ErrPluginBindingInvalid, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateBinding: unexpected error: %v", err)
			}
		})
	}
}
