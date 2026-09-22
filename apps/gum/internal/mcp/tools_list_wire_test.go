package mcp

// Raw-wire tools/list shape gate (bead gum-tq5v).
//
// MCP's Tool.outputSchema is optional, but when the key is present its value
// MUST be a JSON Schema object. gum 2.0.1 put `"outputSchema":null` on the
// wire for skills_get, the one tool registered without a schema, and strict
// clients rejected the whole tools/list response: every gum tool disappeared,
// not just that one.
//
// The defect lives in JSON encoding, so the assertion has to read the bytes.
// go-sdk's Tool.OutputSchema is `any` with omitempty; a nil json.RawMessage
// assigned into it makes the interface non-nil, omitempty never fires, and the
// field marshals to null. After the client unmarshals, both the null and the
// absent key land in the same nil `any`, so a test that asserts `== nil` on a
// decoded Tool passes against the broken build.

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// wireConn drives a Server over IOTransport with newline-delimited JSON the
// test writes itself, so no SDK client sits between the assertion and the
// bytes.
type wireConn struct {
	t      *testing.T
	writer *io.PipeWriter
	reader *bufio.Reader
	nextID int

	// notifications holds every server-initiated frame seen while waiting for
	// a response, in arrival order. Tests that assert gum stays silent on a
	// channel read it after the last call.
	notifications []wireNotification
}

// wireNotification is one server-initiated frame: a JSON-RPC message with a
// method and no id.
type wireNotification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func newWireConn(t *testing.T, srv *Server) *wireConn {
	t.Helper()

	clientToServer, clientWriter := io.Pipe()
	serverReader, serverToClient := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		_ = srv.Run(ctx, &sdkmcp.IOTransport{Reader: clientToServer, Writer: serverToClient})
	}()

	t.Cleanup(func() {
		_ = clientWriter.Close()
		cancel()
		_ = serverReader.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("server did not shut down")
		}
	})

	return &wireConn{t: t, writer: clientWriter, reader: bufio.NewReader(serverReader)}
}

// callRaw sends one request and returns the raw bytes of its result and error
// members. Server-initiated notifications and any other id share the stream,
// so it reads until the matching id arrives. Callers that need to tell a
// JSON-RPC error apart from a tool-level error envelope use this; call is the
// shorthand for requests that must succeed.
func (c *wireConn) callRaw(method string, params any) (result, rpcErr json.RawMessage) {
	c.t.Helper()

	c.nextID++
	id := c.nextID
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})

	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			c.t.Fatalf("read %s response: %v", method, err)
		}

		var envelope struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			c.t.Fatalf("decode wire line %q: %v", line, err)
		}
		if envelope.ID == nil && envelope.Method != "" {
			c.notifications = append(c.notifications, wireNotification{Method: envelope.Method, Params: envelope.Params})
			continue
		}
		if envelope.ID == nil || *envelope.ID != id {
			continue
		}
		return envelope.Result, envelope.Error
	}
}

// call returns the result member and fails the test on a JSON-RPC error.
func (c *wireConn) call(method string, params any) json.RawMessage {
	c.t.Helper()

	result, rpcErr := c.callRaw(method, params)
	if len(rpcErr) > 0 && string(rpcErr) != "null" {
		c.t.Fatalf("%s returned an error: %s", method, rpcErr)
	}
	return result
}

// handshake completes initialize plus notifications/initialized, which every
// method other than initialize needs first.
func (c *wireConn) handshake() {
	c.t.Helper()

	c.call("initialize", map[string]any{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "wire-test", "version": "0"},
	})
	c.notify("notifications/initialized")
}

func (c *wireConn) notify(method string) {
	c.t.Helper()
	c.send(map[string]any{"jsonrpc": "2.0", "method": method})
}

func (c *wireConn) send(msg map[string]any) {
	c.t.Helper()

	encoded, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatalf("marshal %v: %v", msg, err)
	}
	if _, err := c.writer.Write(append(encoded, '\n')); err != nil {
		c.t.Fatalf("write %v: %v", msg, err)
	}
}

// rawToolsList completes the handshake and returns one raw JSON object per
// listed tool, keys intact.
func rawToolsList(t *testing.T) []map[string]json.RawMessage {
	t.Helper()

	isolateAuditSentinel(t)
	t.Setenv("HOME", t.TempDir())

	conn := newWireConn(t, NewServer(pairingDispatcher{}))
	conn.handshake()

	var listed struct {
		Tools []map[string]json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(conn.call("tools/list", map[string]any{}), &listed); err != nil {
		t.Fatalf("decode tools/list result: %v", err)
	}
	if len(listed.Tools) == 0 {
		t.Fatal("tools/list returned nothing")
	}
	return listed.Tools
}

func TestToolsListNeverPutsNullOutputSchemaOnTheWire(t *testing.T) {
	tools := rawToolsList(t)

	sawSchemaless := false
	for _, tool := range tools {
		var name string
		if err := json.Unmarshal(tool["name"], &name); err != nil {
			t.Fatalf("decode tool name: %v", err)
		}

		raw, present := tool["outputSchema"]
		if !present {
			sawSchemaless = true
			continue
		}

		var schema map[string]json.RawMessage
		if err := json.Unmarshal(raw, &schema); err != nil || schema == nil {
			t.Errorf("tool %q put outputSchema %s on the wire; when the key is present it MUST be a schema object, so a tool with no schema must omit the key entirely", name, raw)
		}
	}

	if !sawSchemaless {
		t.Fatal("no tool omitted outputSchema, so this gate proved nothing; skills_get is expected to register without one")
	}
}

func TestSkillsGetOmitsTheOutputSchemaKey(t *testing.T) {
	for _, tool := range rawToolsList(t) {
		var name string
		if err := json.Unmarshal(tool["name"], &name); err != nil {
			t.Fatalf("decode tool name: %v", err)
		}
		if name != "skills_get" {
			continue
		}

		if raw, present := tool["outputSchema"]; present {
			t.Fatalf("skills_get carries outputSchema %s; it registers none, so the key must be absent", raw)
		}
		return
	}

	t.Fatal("skills_get is not in tools/list")
}
