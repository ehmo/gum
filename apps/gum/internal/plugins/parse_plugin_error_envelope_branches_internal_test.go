package plugins

import (
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestParsePluginErrorEnvelopeNilResReturnsZero pins the
// `res == nil → return out` short-circuit. MapPluginError-style call
// sites pass the executor's *CallToolResult through unchecked; if a
// runtime path ever surfaces a nil result (e.g. early-cancellation
// race) this helper MUST tolerate it instead of panicking on
// res.Content indexing.
func TestParsePluginErrorEnvelopeNilResReturnsZero(t *testing.T) {
	got := parsePluginErrorEnvelope(nil)
	if got != (PluginError{}) {
		t.Errorf("got=%+v; want zero PluginError on nil res", got)
	}
}

// TestParsePluginErrorEnvelopeEmptyContentReturnsZero pins the
// `len(Content) == 0 → return out` short-circuit. A plugin can
// legitimately ship IsError=true with an empty Content slice (e.g.
// the SDK ergonomic helper for "unknown failure, no payload"); the
// helper MUST treat that the same as a nil result so dispatch maps
// it to SERVICE_DOWN rather than panicking on Content[0].
func TestParsePluginErrorEnvelopeEmptyContentReturnsZero(t *testing.T) {
	res := &sdkmcp.CallToolResult{IsError: true, Content: nil}
	got := parsePluginErrorEnvelope(res)
	if got != (PluginError{}) {
		t.Errorf("got=%+v; want zero PluginError on empty content", got)
	}
}

// TestParsePluginErrorEnvelopeNonTextContentReturnsZero pins the
// `first Content isn't *TextContent → return out` arm. A misbehaving
// plugin could ship an IsError result whose first block is an image
// or audio content type; the host MUST NOT panic on the type
// assertion. Falling through with a zero PluginError lets the
// upstream MapPluginError surface SERVICE_DOWN, the conservative
// "unknown plugin failure" code.
func TestParsePluginErrorEnvelopeNonTextContentReturnsZero(t *testing.T) {
	res := &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.ImageContent{Data: []byte("ignored"), MIMEType: "image/png"}},
	}
	got := parsePluginErrorEnvelope(res)
	if got != (PluginError{}) {
		t.Errorf("got=%+v; want zero PluginError on non-text first block", got)
	}
}

// TestParsePluginErrorEnvelopeMalformedJSONReturnsZero pins the
// `json.Unmarshal err → return out` arm. A garbled envelope is the
// realistic failure mode for a plugin crashing mid-write or emitting
// a non-JSON shim — the helper MUST swallow the parse error so the
// host's stable error path runs (SERVICE_DOWN via MapPluginError)
// rather than the parse propagating into the dispatch envelope.
func TestParsePluginErrorEnvelopeMalformedJSONReturnsZero(t *testing.T) {
	res := &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "this { is ] not json"}},
	}
	got := parsePluginErrorEnvelope(res)
	if got != (PluginError{}) {
		t.Errorf("got=%+v; want zero PluginError on json.Unmarshal err", got)
	}
}

// TestParsePluginErrorEnvelopeMessageFieldFallback pins the
// `raw.Error == "" → out.Message = raw.Message` arm. Plugins emit
// either `"error"` (preferred per spec §8) or `"message"` (legacy /
// some SDK conveniences). The helper MUST honour the message-field
// fallback so operator dashboards still get human text when a plugin
// only populates the legacy key. Without this arm, RATE_LIMIT events
// from older plugins would surface with an empty Message field even
// though the wire payload carried one.
func TestParsePluginErrorEnvelopeMessageFieldFallback(t *testing.T) {
	res := &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: `{"error_code":"RATE_LIMIT","message":"slow down please","retryable":true,"retry_after_ms":1500}`}},
	}
	got := parsePluginErrorEnvelope(res)
	if got.Code != "RATE_LIMIT" {
		t.Errorf("Code=%q; want RATE_LIMIT", got.Code)
	}
	if got.Message != "slow down please" {
		t.Errorf("Message=%q; want fallback from 'message' field", got.Message)
	}
	if !got.Retryable {
		t.Error("Retryable=false; want true")
	}
	if got.RetryAfterMS != 1500 {
		t.Errorf("RetryAfterMS=%d; want 1500", got.RetryAfterMS)
	}
}
