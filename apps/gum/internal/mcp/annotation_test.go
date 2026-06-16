// Package mcp — gum-6m8 acceptance tests.
//
// Per spec §13 (lines 3210-3214) and docs/test-matrix.md:
//
//   - TestToolAnnotations asserts the go-sdk v1.6.0 ToolAnnotations field
//     shapes that GUM depends on (DestructiveHint pointer-backed, ReadOnlyHint
//     plain-bool-with-omitempty). The test fails if a future SDK upgrade
//     changes those shapes without a spec patch.
//   - TestToolAnnotationsWireForm serializes every Tier A tool's annotation
//     struct to JSON and verifies the wire-form contract: readOnlyHint=true is
//     present, readOnlyHint=false is OMITTED (omitempty under v1.6.0),
//     destructiveHint is present with explicit true|false on every Tier A
//     tool, and idempotentHint / openWorldHint are absent or present-with-bool
//     — never null.
package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestToolAnnotations asserts that the go-sdk v1.6.0 sdkmcp.ToolAnnotations
// struct still has the field shapes GUM's annotation logic depends on. Spec
// §13 line 3210 pins DestructiveHint as pointer-backed in v1.6.0 (so the host
// can distinguish "destructive=false declared" from "destructive unknown"),
// and ReadOnlyHint as a plain bool with `omitempty` (so the false default is
// omitted from the wire form).
//
// If a future SDK upgrade flips DestructiveHint to a plain bool, the GUM
// boolPtr(false) calls would no longer compile, but this test surfaces the
// regression with a clearer message at the type-shape level.
func TestToolAnnotations(t *testing.T) {
	annType := reflect.TypeOf(sdkmcp.ToolAnnotations{})

	destField, ok := annType.FieldByName("DestructiveHint")
	if !ok {
		t.Fatal("sdkmcp.ToolAnnotations has no DestructiveHint field")
	}
	if destField.Type.Kind() != reflect.Pointer || destField.Type.Elem().Kind() != reflect.Bool {
		t.Errorf("DestructiveHint type = %s; want *bool (spec §13 line 3210; SDK upgrade regression)", destField.Type)
	}
	if got, want := destField.Tag.Get("json"), "destructiveHint,omitempty"; got != want {
		t.Errorf("DestructiveHint json tag = %q; want %q", got, want)
	}

	readField, ok := annType.FieldByName("ReadOnlyHint")
	if !ok {
		t.Fatal("sdkmcp.ToolAnnotations has no ReadOnlyHint field")
	}
	if readField.Type.Kind() != reflect.Bool {
		t.Errorf("ReadOnlyHint type = %s; want plain bool (spec §13 line 3210)", readField.Type)
	}
	if got, want := readField.Tag.Get("json"), "readOnlyHint,omitempty"; got != want {
		t.Errorf("ReadOnlyHint json tag = %q; want %q (false-valued readOnlyHint must omit under v1.6.0)", got, want)
	}

	// IdempotentHint and OpenWorldHint must NOT serialize null. Either field
	// shape (plain bool + omitempty, or *bool + omitempty) satisfies this; the
	// wire-form test below catches actual null emissions.
	for _, name := range []string{"IdempotentHint", "OpenWorldHint"} {
		f, ok := annType.FieldByName(name)
		if !ok {
			t.Errorf("sdkmcp.ToolAnnotations has no %s field", name)
			continue
		}
		tag := f.Tag.Get("json")
		// The exact form may evolve; require at least omitempty so absence is the wire default.
		if want := ",omitempty"; len(tag) == 0 || !containsSuffix(tag, want) {
			t.Errorf("%s json tag = %q; want suffix %q (so the field is omitted, never null, when unset)", name, tag, want)
		}
	}
}

// containsSuffix reports whether s ends with suffix. Local helper to keep the
// reflection test self-contained.
func containsSuffix(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

// TestToolAnnotationsWireForm asserts the wire-form contract from spec §13
// line 3212 for every Tier A tool annotation (9 meta + 18 convenience = 27):
//
//	(a) readOnlyHint=true MUST be present with the value true.
//	(b) readOnlyHint=false MUST be absent from the wire JSON (omitempty under
//	    go-sdk v1.6.0); hosts interpret absence as "not read-only".
//	(c) destructiveHint MUST be present with explicit true or false for every
//	    Tier A tool (no absent, no null).
//	(d) idempotentHint and openWorldHint are either absent or present with a
//	    boolean value — never null.
//
// The test serializes the annotation struct that GUM actually registers
// (via TierAMetaToolAnnotations) rather than re-deriving the expected shape,
// so any drift between the registered values and the wire form is caught at
// release-gate time.
func TestToolAnnotationsWireForm(t *testing.T) {
	anns := TierAMetaToolAnnotations()
	if got := len(anns); got != 27 {
		t.Fatalf("TierAMetaToolAnnotations has %d entries; want 27 (9 meta + 18 convenience, spec §4.1)", got)
	}

	for name, ann := range anns {
		if ann == nil {
			t.Errorf("%s: nil annotation pointer (spec §13 line 3212 requires destructiveHint on every Tier A tool)", name)
			continue
		}

		data, err := json.Marshal(ann)
		if err != nil {
			t.Errorf("%s: marshal annotation: %v", name, err)
			continue
		}

		// Decode into a map keyed by raw JSON value so we can distinguish
		// "absent" (no key) from "null" (key present, value null).
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Errorf("%s: unmarshal annotation: %v", name, err)
			continue
		}

		// (a)+(b) readOnlyHint wire shape.
		readRaw, readPresent := raw["readOnlyHint"]
		switch {
		case ann.ReadOnlyHint && !readPresent:
			t.Errorf("%s: readOnlyHint absent on wire; want true (spec §13 line 3212 (a))", name)
		case ann.ReadOnlyHint && string(readRaw) != "true":
			t.Errorf("%s: readOnlyHint=%s on wire; want true", name, string(readRaw))
		case !ann.ReadOnlyHint && readPresent:
			t.Errorf("%s: readOnlyHint present (=%s) on wire; want absent under go-sdk v1.6.0 omitempty (spec §13 line 3212 (b))", name, string(readRaw))
		}

		// (c) destructiveHint wire shape: MUST be present with true|false.
		destRaw, destPresent := raw["destructiveHint"]
		if !destPresent {
			t.Errorf("%s: destructiveHint absent on wire; spec §13 line 3212 (c) requires explicit true|false on every Tier A tool", name)
		} else {
			s := string(destRaw)
			if s != "true" && s != "false" {
				t.Errorf("%s: destructiveHint = %s on wire; want literal true or false (no null)", name, s)
			}
		}

		// (d) idempotentHint / openWorldHint MUST NOT be null on wire.
		for _, key := range []string{"idempotentHint", "openWorldHint"} {
			v, present := raw[key]
			if !present {
				continue
			}
			s := string(v)
			if s != "true" && s != "false" {
				t.Errorf("%s: %s = %s on wire; want absent or literal true/false (never null) per spec §13 line 3212 (d)", name, key, s)
			}
		}
	}
}

// TestConvenienceToolAnnotationsWiredLive is the audit regression: convenience
// tools must carry their §13 annotations on the LIVE wire (via ListTools), not
// just in the in-memory helper. registerConvenienceTools previously passed nil
// Annotations, so the SDK serialized destructiveHint=true for write tools like
// gmail_send (a non-destructive write). This drives the real server.
func TestConvenienceToolAnnotationsWiredLive(t *testing.T) {
	srv := NewServer(schemaTestDispatcher{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, srvTransport) }()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	byName := map[string]*sdkmcp.Tool{}
	for i := range result.Tools {
		byName[result.Tools[i].Name] = result.Tools[i]
	}

	// gmail_send: write convenience tool → ReadOnlyHint=false, DestructiveHint=false.
	send := byName["gmail_send"]
	if send == nil {
		t.Fatal("gmail_send not in tools/list (convenience tools not registered?)")
	}
	if send.Annotations == nil {
		t.Fatal("gmail_send has nil Annotations on the wire (the bug); want write hints")
	}
	if send.Annotations.ReadOnlyHint {
		t.Error("gmail_send ReadOnlyHint=true; want false (it's a write tool)")
	}
	if send.Annotations.DestructiveHint == nil || *send.Annotations.DestructiveHint {
		t.Errorf("gmail_send DestructiveHint=%v; want explicit false (a non-destructive write)", send.Annotations.DestructiveHint)
	}

	// gmail_search: read convenience tool → ReadOnlyHint=true.
	search := byName["gmail_search"]
	if search == nil || search.Annotations == nil {
		t.Fatal("gmail_search missing or nil Annotations")
	}
	if !search.Annotations.ReadOnlyHint {
		t.Error("gmail_search ReadOnlyHint=false; want true (it's a read tool)")
	}
}
