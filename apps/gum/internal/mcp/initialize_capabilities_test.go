package mcp_test

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	gummcp "github.com/ehmo/gum/internal/mcp"
)

// connectInitializeClient brings up the gum MCP server on an in-memory
// transport, drives the initialize handshake, and returns the resulting
// session plus a cleanup that tears the goroutine down.
func connectInitializeClient(t *testing.T) (*sdkmcp.ClientSession, func()) {
	t.Helper()
	srv := gummcp.NewServer(stubDispatcher{})
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "init-test", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		<-done
		t.Fatalf("client.Connect: %v", err)
	}
	cleanup := func() {
		_ = cs.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Server.Run did not return within 2s after cancel")
		}
	}
	return cs, cleanup
}

// TestMCPInitializeCapabilities asserts the spec §13.2 capability matrix at
// the wire level: every server-owned capability marked **Implemented** is
// present, every **Deferred** capability is absent. The advertised set is
// what the v0.1.0 contract actually delivers — prompts/completion/logging
// remain deferred per docs/known-divergences.md until v0.2.0/v0.3.0 lands.
//
// This test is the SDK-upgrade canary: bumping go-sdk to a version that
// silently flips a default capability on (or off) breaks here.
func TestMCPInitializeCapabilities(t *testing.T) {
	defer goleak.VerifyNone(t)
	cs, cleanup := connectInitializeClient(t)
	defer cleanup()

	res := cs.InitializeResult()
	if res == nil {
		t.Fatal("InitializeResult is nil after Connect")
	}
	caps := res.Capabilities
	if caps == nil {
		t.Fatal("server advertised nil capabilities; want non-nil with tools+resources")
	}

	// Tools: advertised, ListChanged=false (spec §4.1 line 383).
	if caps.Tools == nil {
		t.Errorf("Tools capability absent; want advertised")
	} else if caps.Tools.ListChanged {
		t.Errorf("Tools.ListChanged = true; want false in v0.1.0 (spec §13.2 / known-divergences)")
	}

	// Resources: advertised, ListChanged=false, Subscribe=false.
	if caps.Resources == nil {
		t.Errorf("Resources capability absent; want advertised")
	} else {
		if caps.Resources.ListChanged {
			t.Errorf("Resources.ListChanged = true; want false in v0.1.0")
		}
		if caps.Resources.Subscribe {
			t.Errorf("Resources.Subscribe = true; want false (deferred to v0.3.0)")
		}
	}

	// Prompts: advertised in v0.1.0 (gum-z6w landed the two static zero-argument
	// templates gum.summarize_workspace_for_today + gum.audit_recent_writes).
	// ListChanged stays false because the roster is closed at compile time.
	if caps.Prompts == nil {
		t.Errorf("Prompts capability absent; want advertised (v0.1.0 static roster)")
	} else if caps.Prompts.ListChanged {
		t.Errorf("Prompts.ListChanged = true; want false (closed v0.1.0 roster)")
	}

	// Completions: advertised in v0.1.0 (gum-vok landed the help-topic completion
	// source; op_id/variant_id/plugin-name/closed-enum sources extend the dispatch
	// table in v0.2.0 without changing the capability bit).
	if caps.Completions == nil {
		t.Errorf("Completions capability absent; want advertised (v0.1.0 ref/resource gum://help/{topic})")
	}

	// Logging: NEVER advertised in v0.1.0 (spec §13.2 line 3290).
	if caps.Logging != nil {
		t.Errorf("Logging capability advertised; want nil (spec §13.2 line 3290 — deferred to v0.3.0)")
	}

	// Experimental / Extensions: nothing claimed in v0.1.0.
	if len(caps.Experimental) > 0 {
		t.Errorf("Experimental capabilities = %v; want empty in v0.1.0", caps.Experimental)
	}
	if len(caps.Extensions) > 0 {
		t.Errorf("Extensions = %v; want empty in v0.1.0", caps.Extensions)
	}
}

// TestNoTaskCapabilityV01 asserts spec §13.2 line 3298-3303: task
// augmentation, tasks/get, tasks/result, tasks/list, tasks/cancel are all
// deferred to v0.3.0 and the corresponding capability MUST NOT be advertised.
// The MCP SDK does not yet model a typed `tasks` field on ServerCapabilities;
// the spec specifies that experimental task negotiation MUST be absent. This
// test guards against a future SDK upgrade that introduces a Tasks field or
// against a stray Experimental["tasks"]/Extensions["mcp/tasks"] settings map.
func TestNoTaskCapabilityV01(t *testing.T) {
	defer goleak.VerifyNone(t)
	cs, cleanup := connectInitializeClient(t)
	defer cleanup()

	caps := cs.InitializeResult().Capabilities
	for key := range caps.Experimental {
		if containsFold(key, "task") {
			t.Errorf("Experimental[%q] advertised; tasks deferred to v0.3.0 (spec §13.2)", key)
		}
	}
	for key := range caps.Extensions {
		if containsFold(key, "task") {
			t.Errorf("Extensions[%q] advertised; tasks deferred to v0.3.0 (spec §13.2)", key)
		}
	}
}

// TestNoIconMetadataV01 enforces spec §13.2 line 3309: icons metadata is
// deferred to v0.2.0 and MUST NOT appear in tools/resources/templates. A
// regression that decorates a Tier A tool with an icon adds metadata bytes
// to every tools/list response, breaking the token-budget claim of §4.1.
func TestNoIconMetadataV01(t *testing.T) {
	defer goleak.VerifyNone(t)
	cs, cleanup := connectInitializeClient(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := cs.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range tools.Tools {
		if len(tool.Icons) != 0 {
			t.Errorf("tool %q has %d icons; want 0 in v0.1.0", tool.Name, len(tool.Icons))
		}
	}

	resources, err := cs.ListResources(ctx, &sdkmcp.ListResourcesParams{})
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	for _, r := range resources.Resources {
		if len(r.Icons) != 0 {
			t.Errorf("resource %q has %d icons; want 0", r.URI, len(r.Icons))
		}
	}

	templates, err := cs.ListResourceTemplates(ctx, &sdkmcp.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	for _, tmpl := range templates.ResourceTemplates {
		if len(tmpl.Icons) != 0 {
			t.Errorf("template %q has %d icons; want 0", tmpl.URITemplate, len(tmpl.Icons))
		}
	}
}

// containsFold is a case-insensitive substring check that avoids a stdlib
// dep on strings.EqualFold's broader behaviour. We only need ASCII matching
// for capability keys, which are always ASCII per the MCP spec.
func containsFold(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}
	hs := toLowerASCII(haystack)
	nd := toLowerASCII(needle)
	for i := 0; i+len(nd) <= len(hs); i++ {
		if hs[i:i+len(nd)] == nd {
			return true
		}
	}
	return false
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
