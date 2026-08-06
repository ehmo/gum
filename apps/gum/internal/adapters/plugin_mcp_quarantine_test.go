package adapters_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/plugins"
)

func quarantinedVariant() *dispatch.ResolvedVariant {
	return &dispatch.ResolvedVariant{
		OpID:       "trends.interest_over_time",
		AdapterKey: "plugin.mcp",
		Variant: &catalog.Variant{
			VariantID: "trends.v1.plugin.interest",
			Binding: &catalog.Binding{
				AdapterKey: "plugin.mcp",
				PluginName: "google-trends",
				ToolName:   "interest_over_time",
			},
		},
	}
}

// TestQuarantinedPluginIsNotServiceDown is the adapter half of gum-mmzr. The
// supervisor refuses to spawn a quarantined plugin; reporting that as
// SERVICE_DOWN tells the operator Google is unreachable and sends them chasing
// a network fault, when the fix is two local commands.
func TestQuarantinedPluginIsNotServiceDown(t *testing.T) {
	t.Parallel()
	pm := adapters.NewPluginMCPLazyWithStarter(
		func() *plugins.Host { return plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()}) },
		func(context.Context, *plugins.Host, string) (*plugins.Plugin, error) {
			return nil, fmt.Errorf("supervisor refused: %w", plugins.ErrPluginQuarantined)
		},
	)

	inv := &dispatch.Invocation{OpID: "trends.interest_over_time", Args: map[string]any{}}
	_, err := pm.Execute(context.Background(), inv, quarantinedVariant(), &dispatch.Credentials{})
	if err == nil {
		t.Fatal("Execute on a quarantined plugin returned nil error")
	}

	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T (%v); want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeVariantQuarantined {
		t.Fatalf("ErrCode = %q; want %q", se.ErrCode, dispatch.ErrCodeVariantQuarantined)
	}
	if se.Detail["reason"] != "plugin_quarantined" {
		t.Errorf("Detail[reason] = %v; want plugin_quarantined", se.Detail["reason"])
	}
	if se.Detail["plugin_name"] != "google-trends" {
		t.Errorf("Detail[plugin_name] = %v; want google-trends", se.Detail["plugin_name"])
	}
	if se.Detail["variant_id"] != "trends.v1.plugin.interest" {
		t.Errorf("Detail[variant_id] = %v; want trends.v1.plugin.interest", se.Detail["variant_id"])
	}
	if se.Detail["adapter_key"] != "plugin.mcp" {
		t.Errorf("Detail[adapter_key] = %v; want plugin.mcp", se.Detail["adapter_key"])
	}
	if se.Retryable {
		t.Error("Retryable = true; a quarantine does not clear by retrying the call")
	}
}

// TestQuarantineHintNamesTheRecoveryCommands: the bead asked for the error to
// "name the quarantine, or at least point at `gum plugin list`". The hint has
// to carry all three commands with the plugin ID substituted, because that is
// the only text the CLI prints back to a human.
func TestQuarantineHintNamesTheRecoveryCommands(t *testing.T) {
	t.Parallel()
	pm := adapters.NewPluginMCPLazyWithStarter(
		func() *plugins.Host { return plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()}) },
		func(context.Context, *plugins.Host, string) (*plugins.Plugin, error) {
			return nil, plugins.ErrPluginQuarantined
		},
	)

	inv := &dispatch.Invocation{OpID: "trends.interest_over_time", Args: map[string]any{}}
	_, err := pm.Execute(context.Background(), inv, quarantinedVariant(), &dispatch.Credentials{})

	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T (%v); want *dispatch.StructuredError", err, err)
	}
	hint, _ := se.Detail["hint"].(string)
	for _, want := range []string{
		"gum plugin list",
		"gum plugin reload google-trends",
		"gum plugin unquarantine google-trends",
		"not down upstream",
	} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint missing %q:\n%s", want, hint)
		}
	}
}

// TestNonQuarantineStartFailureStaysServiceDown guards the branch boundary: a
// plugin that fails to spawn for any other reason is still an outage, and
// re-coding it as VARIANT_QUARANTINED would tell the operator to run
// `gum plugin unquarantine` on a plugin that is not quarantined.
func TestNonQuarantineStartFailureStaysServiceDown(t *testing.T) {
	t.Parallel()
	pm := adapters.NewPluginMCPLazyWithStarter(
		func() *plugins.Host { return plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()}) },
		func(context.Context, *plugins.Host, string) (*plugins.Plugin, error) {
			return nil, errors.New("exec: permission denied")
		},
	)

	inv := &dispatch.Invocation{OpID: "trends.interest_over_time", Args: map[string]any{}}
	_, err := pm.Execute(context.Background(), inv, quarantinedVariant(), &dispatch.Credentials{})

	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T (%v); want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeServiceDown {
		t.Errorf("ErrCode = %q; want SERVICE_DOWN", se.ErrCode)
	}
	if detail, _ := se.Detail["detail"].(string); !strings.Contains(detail, "permission denied") {
		t.Errorf("Detail[detail] = %q; want the underlying spawn error", detail)
	}
}
