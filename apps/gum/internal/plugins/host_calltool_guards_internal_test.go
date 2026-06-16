package plugins

import (
	"context"
	"strings"
	"testing"
)

// TestCallToolNilPluginReturnsNotRunning pins CallTool's
// `p == nil → return "plugin not running"` arm (host.go:403-405).
// Callers MUST get a clean error rather than a nil-deref when invoking
// CallTool on a zero/nil Plugin (e.g. after Stop()).
func TestCallToolNilPluginReturnsNotRunning(t *testing.T) {
	var p *Plugin
	body, err := p.CallTool(context.Background(), "any.tool", nil)
	if err == nil {
		t.Fatalf("CallTool(nil) err=nil; want 'plugin not running'")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("err=%v; want 'not running' message", err)
	}
	if body != nil {
		t.Errorf("body=%v; want nil on nil-plugin guard", body)
	}
}

// TestCallToolNilClientSessionReturnsNotRunning is the companion arm:
// a non-nil Plugin whose cs has been cleared (post-Stop, or never
// initialized) MUST also fail with the same message.
func TestCallToolNilClientSessionReturnsNotRunning(t *testing.T) {
	p := &Plugin{cs: nil}
	_, err := p.CallTool(context.Background(), "any.tool", nil)
	if err == nil {
		t.Fatalf("CallTool(cs=nil) err=nil; want 'plugin not running'")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("err=%v; want 'not running' message", err)
	}
}
