// Package mcp — gum-6m8 acceptance tests.
//
// Per spec §13 and docs/test-matrix.md:
//
//   - TestToolAnnotations asserts the go-sdk v1.7.0 ToolAnnotations field
//     shapes that GUM depends on (DestructiveHint and OpenWorldHint
//     pointer-backed with omitempty, ReadOnlyHint and IdempotentHint plain
//     bool without omitempty). The test fails if a future SDK upgrade changes
//     those shapes without a spec patch.
//   - TestToolAnnotationsWireForm serializes every Tier A tool's annotation
//     struct to JSON and verifies the wire-form contract: readOnlyHint carries
//     the tool's boolean value, destructiveHint is present with explicit
//     true|false on every Tier A tool, and idempotentHint / openWorldHint are
//     absent or present-with-bool — never null.
package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestToolAnnotations asserts that the go-sdk v1.7.0 sdkmcp.ToolAnnotations
// struct still has the field shapes GUM's annotation logic depends on. Spec
// §13 pins DestructiveHint as pointer-backed (so the host can distinguish
// "destructive=false declared" from "destructive unknown"), and ReadOnlyHint
// as a plain bool without `omitempty` (so the false case reaches the wire as
// an explicit false rather than an absent key).
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
		t.Errorf("ReadOnlyHint type = %s; want plain bool (spec §13)", readField.Type)
	}
	if got, want := readField.Tag.Get("json"), "readOnlyHint"; got != want {
		t.Errorf("ReadOnlyHint json tag = %q; want %q (v1.7.0 dropped omitempty so false reaches the wire)", got, want)
	}

	// IdempotentHint is a plain bool without omitempty in v1.7.0, so it always
	// serializes an explicit boolean. OpenWorldHint stays pointer-backed with
	// omitempty, so it is absent when unset. Neither may serialize null; the
	// wire-form test below catches actual null emissions.
	idemField, ok := annType.FieldByName("IdempotentHint")
	if !ok {
		t.Fatal("sdkmcp.ToolAnnotations has no IdempotentHint field")
	}
	if idemField.Type.Kind() != reflect.Bool {
		t.Errorf("IdempotentHint type = %s; want plain bool", idemField.Type)
	}
	if got, want := idemField.Tag.Get("json"), "idempotentHint"; got != want {
		t.Errorf("IdempotentHint json tag = %q; want %q (v1.7.0 dropped omitempty)", got, want)
	}

	openField, ok := annType.FieldByName("OpenWorldHint")
	if !ok {
		t.Fatal("sdkmcp.ToolAnnotations has no OpenWorldHint field")
	}
	if openField.Type.Kind() != reflect.Pointer || openField.Type.Elem().Kind() != reflect.Bool {
		t.Errorf("OpenWorldHint type = %s; want *bool", openField.Type)
	}
	if got, want := openField.Tag.Get("json"), "openWorldHint,omitempty"; got != want {
		t.Errorf("OpenWorldHint json tag = %q; want %q (absent, never null, when unset)", got, want)
	}
}

// TestToolAnnotationsWireForm asserts the spec §13 wire-form contract for
// every Tier A tool annotation (9 meta + 18 convenience = 27):
//
//	(a) readOnlyHint MUST be present and MUST carry the tool's own boolean
//	    value; go-sdk v1.7.0 dropped the omitempty that used to hide false.
//	(b) destructiveHint MUST be present with explicit true or false for every
//	    Tier A tool (no absent, no null).
//	(c) idempotentHint and openWorldHint are either absent or present with a
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

		// (a) readOnlyHint wire shape: present, carrying the tool's own value.
		readRaw, readPresent := raw["readOnlyHint"]
		want := "false"
		if ann.ReadOnlyHint {
			want = "true"
		}
		switch {
		case !readPresent:
			t.Errorf("%s: readOnlyHint absent on wire; want %s (spec §13 (a); v1.7.0 has no omitempty)", name, want)
		case string(readRaw) != want:
			t.Errorf("%s: readOnlyHint=%s on wire; want %s", name, string(readRaw), want)
		}

		// (b) destructiveHint wire shape: MUST be present with true|false.
		destRaw, destPresent := raw["destructiveHint"]
		if !destPresent {
			t.Errorf("%s: destructiveHint absent on wire; spec §13 (c) requires explicit true|false on every Tier A tool", name)
		} else {
			s := string(destRaw)
			if s != "true" && s != "false" {
				t.Errorf("%s: destructiveHint = %s on wire; want literal true or false (no null)", name, s)
			}
		}

		// (c) idempotentHint / openWorldHint MUST NOT be null on wire.
		for _, key := range []string{"idempotentHint", "openWorldHint"} {
			v, present := raw[key]
			if !present {
				continue
			}
			s := string(v)
			if s != "true" && s != "false" {
				t.Errorf("%s: %s = %s on wire; want absent or literal true/false (never null) per spec §13 (d)", name, key, s)
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
