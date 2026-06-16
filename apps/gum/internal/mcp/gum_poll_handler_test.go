// Package mcp — Red Team failing tests for gum-9vuq.7.
//
// Covers: gum.poll handler: progress notifications (with/without token, integer
// vs string type preservation), LRO_TIMEOUT envelope, INVALID_ARGS, goroutine
// leak safety.
//
// Spec anchors:
//   - spec.md §4.1/§5.7 polling-loop contract.
//   - spec.md §1421 — LRO_TIMEOUT stable error code.
//   - spec.md §3304-3306 — notifications/progress + _meta.progressToken wiring.
//
// These tests FAIL until Green:
//
//	(a) creates internal/lro with Poller, Status, TimeoutError etc.
//	(b) adds a pollerFactory (or equivalent) seam to Server so this package
//	    (package mcp, internal) can inject a fake poller.
//	(c) implements handlePoll to drive the poller and emit progress.
//
// Server seam Green MUST add (recommended shape):
//
//	type lroPoller interface {
//	    Poll(ctx context.Context, operationName string) (any, error)
//	}
//	type lroPollerFactory func(onTick func(elapsed time.Duration)) lroPoller
//
//	// On Server struct:
//	//   pollerFactory lroPollerFactory
//
// Tests use NewServerWithCatalog then assign s.pollerFactory before calling
// handlePoll. Any equivalent unexported seam reachable from package mcp is
// acceptable — the exact field/method name is up to Green.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/lro"
)

// --- fake poller helpers -------------------------------------------------------

// fakePollResult represents one scheduled result from fakePoller.
type fakePollResult struct {
	result any
	err    error
}

// fakePoller simulates the lroPoller seam injected into Server.
// It calls onTick with the supplied elapsed sequence before returning the
// terminal result.
type fakePoller struct {
	// ticks are emitted (via onTick) before the terminal result.
	ticks  []time.Duration
	result fakePollResult
	onTick func(elapsed time.Duration)
}

func (f *fakePoller) Poll(_ context.Context, _ string) (any, error) {
	for _, d := range f.ticks {
		if f.onTick != nil {
			f.onTick(d)
		}
	}
	return f.result.result, f.result.err
}

// blockingPoller blocks in Poll until its ctx is cancelled.
type blockingPoller struct{}

func (b *blockingPoller) Poll(ctx context.Context, _ string) (any, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// makePollRequest builds a CallToolRequest for gum.poll with the given args.
func makePollRequest(args map[string]any) *sdkmcp.CallToolRequest {
	raw, _ := json.Marshal(args)
	return &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name:      "gum.poll",
			Arguments: raw,
		},
	}
}

// makePollRequestWithToken builds a CallToolRequest with a progress token set
// on the _meta field.
func makePollRequestWithToken(args map[string]any, token any) *sdkmcp.CallToolRequest {
	req := makePollRequest(args)
	req.Params.SetProgressToken(token)
	return req
}

// --- in-memory transport pair helpers -----------------------------------------

// sessionPair boots a single in-memory server/client transport pair and
// returns the *server-side* session that req.Session can be set to so that
// NotifyProgress(req.Session, ...) reaches the listening client. Caller must
// t.Cleanup(closeFn) to tear down.
//
// progressCh receives *sdkmcp.ProgressNotificationParams for each
// notifications/progress the server sends to the client.
//
// Uses one transport pair (was two pre-gum-t71g): server.Connect returns the
// ServerSession directly, and the client's ProgressNotificationHandler
// captures notifications from that same session.
func sessionPair(t *testing.T, srv *Server) (
	ss *sdkmcp.ServerSession,
	progressCh <-chan *sdkmcp.ProgressNotificationParams,
	closeFn func(),
) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan *sdkmcp.ProgressNotificationParams, 64)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, &sdkmcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *sdkmcp.ProgressNotificationClientRequest) {
			select {
			case ch <- req.Params:
			default:
			}
		},
	})

	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	// Direct server.Connect (not Run) so we get the ServerSession handle.
	ssVal, err := srv.sdkSrv.Connect(ctx, srvTransport)
	if err != nil {
		cancel()
		t.Fatalf("srv.sdkSrv.Connect: %v", err)
	}

	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		_ = ssVal.Close()
		t.Fatalf("client.Connect: %v", err)
	}

	closeFn = func() {
		_ = cs.Close()
		_ = ssVal.Close()
		cancel()
	}

	return ssVal, ch, closeFn
}

// drainProgress reads progress notifications from ch until either `deadline`
// elapses overall or no notification arrives for `inactivity` (quiet window).
// Replaces the older non-blocking select/default drain that raced with the SDK's
// background delivery goroutine — see gum-uca9.
func drainProgress(
	ch <-chan *sdkmcp.ProgressNotificationParams,
	deadline, inactivity time.Duration,
) []*sdkmcp.ProgressNotificationParams {
	var out []*sdkmcp.ProgressNotificationParams
	overall := time.After(deadline)
	for {
		select {
		case n := <-ch:
			out = append(out, n)
		case <-time.After(inactivity):
			return out
		case <-overall:
			return out
		}
	}
}

// --- Test I: progress with integer token ----------------------------------------

// TestHandlePollEmitsProgressWithIntegerToken asserts that when _meta.progressToken
// is an integer (int64(42)), the server emits progress notifications and the
// captured ProgressToken on the client side is numeric (int64 or float64(42))
// — NEVER the string "42".
//
// JSON round-trip note: the MCP wire format encodes ProgressToken as a JSON
// number; on decode it may arrive as float64(42) due to json.Unmarshal default
// behaviour. Either int64(42) or float64(42) is accepted; string "42" is not.
func TestHandlePollEmitsProgressWithIntegerToken(t *testing.T) {
	srv := newTestServerWithPoller(t, &fakePoller{
		ticks:  []time.Duration{2 * time.Second, 5 * time.Second, 9*time.Second + 500*time.Millisecond},
		result: fakePollResult{result: map[string]any{"done": true, "name": "ops/integer"}},
	})

	ss, progressCh, closeFn := sessionPair(t, srv)
	t.Cleanup(closeFn)

	req := makePollRequestWithToken(map[string]any{"operation_name": "ops/integer"}, int64(42))
	req.Session = ss

	res, err := srv.handlePoll(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePoll returned error: %v", err)
	}
	if res != nil && res.IsError {
		t.Fatalf("handlePoll returned error result: %v", firstText(res))
	}

	// Bounded blocking drain: wait up to deadline for first notification, then
	// drain until inactivityWindow of quiet elapses. Avoids racing with the
	// SDK's progress-delivery goroutine (gum-uca9).
	notifications := drainProgress(progressCh, 500*time.Millisecond, 50*time.Millisecond)

	if len(notifications) == 0 {
		t.Fatal("no progress notifications received; want ≥1 (spec §3304)")
	}

	// ProgressToken must be numeric — int64(42) or float64(42). Must NOT be "42".
	tok := notifications[0].ProgressToken
	switch v := tok.(type) {
	case int64:
		if v != 42 {
			t.Errorf("ProgressToken int64=%d; want 42", v)
		}
	case float64:
		if v != 42 {
			t.Errorf("ProgressToken float64=%f; want 42", v)
		}
	case int:
		if v != 42 {
			t.Errorf("ProgressToken int=%d; want 42", v)
		}
	default:
		t.Errorf("ProgressToken type=%T value=%v; want int64(42) or float64(42), NOT string %q", tok, tok, "42")
	}
}

// --- Test J: progress with string token -----------------------------------------

// TestHandlePollEmitsProgressWithStringToken asserts that when progressToken is
// a string, the captured ProgressToken arrives as the same string.
func TestHandlePollEmitsProgressWithStringToken(t *testing.T) {
	srv := newTestServerWithPoller(t, &fakePoller{
		ticks:  []time.Duration{2 * time.Second, 5 * time.Second},
		result: fakePollResult{result: map[string]any{"done": true, "name": "ops/string"}},
	})

	ss, progressCh, closeFn := sessionPair(t, srv)
	t.Cleanup(closeFn)

	req := makePollRequestWithToken(map[string]any{"operation_name": "ops/string"}, "client-abc")
	req.Session = ss

	res, err := srv.handlePoll(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePoll returned error: %v", err)
	}
	if res != nil && res.IsError {
		t.Fatalf("handlePoll returned error result: %v", firstText(res))
	}

	// Bounded blocking drain (gum-uca9).
	notifications := drainProgress(progressCh, 500*time.Millisecond, 50*time.Millisecond)

	if len(notifications) == 0 {
		t.Fatal("no progress notifications received; want ≥1")
	}
	tok := notifications[0].ProgressToken
	if tok != "client-abc" {
		t.Errorf("ProgressToken=%v (%T); want string \"client-abc\"", tok, tok)
	}
}

// --- Test K: no progress without token ------------------------------------------

// TestHandlePollNoProgressWithoutToken asserts that when _meta.progressToken is
// absent, zero progress notifications are sent.
func TestHandlePollNoProgressWithoutToken(t *testing.T) {
	srv := newTestServerWithPoller(t, &fakePoller{
		ticks:  []time.Duration{2 * time.Second, 5 * time.Second},
		result: fakePollResult{result: map[string]any{"done": true}},
	})

	ss, progressCh, closeFn := sessionPair(t, srv)
	t.Cleanup(closeFn)

	// No progress token set.
	req := makePollRequest(map[string]any{"operation_name": "ops/no-token"})
	req.Session = ss

	res, err := srv.handlePoll(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePoll returned error: %v", err)
	}
	if res != nil && res.IsError {
		t.Fatalf("handlePoll returned error result: %v", firstText(res))
	}

	// Bounded blocking drain — wait long enough that any spurious progress
	// notification has time to arrive (gum-uca9).
	spurious := drainProgress(progressCh, 200*time.Millisecond, 50*time.Millisecond)

	if len(spurious) != 0 {
		t.Errorf("received %d progress notification(s); want 0 (no progressToken set)", len(spurious))
	}
}

// TestHandlePollProgressIsSessionScoped (gum-t71g) asserts that progress
// notifications emitted by a handlePoll call on session B do NOT reach a
// second listener attached to session A. The previous broadcast over
// s.sdkSrv.Sessions() leaked progress across all connected clients; the fix
// routes via req.Session.NotifyProgress so only the requesting client sees it.
func TestHandlePollProgressIsSessionScoped(t *testing.T) {
	srv := newTestServerWithPoller(t, &fakePoller{
		ticks:  []time.Duration{2 * time.Second, 5 * time.Second},
		result: fakePollResult{result: map[string]any{"done": true, "name": "ops/scoped"}},
	})

	// Session A: a second connected client that MUST NOT receive progress
	// belonging to session B's request.
	_, progressChA, closeA := sessionPair(t, srv)
	t.Cleanup(closeA)

	// Session B: the request session that should receive progress.
	ssB, progressChB, closeB := sessionPair(t, srv)
	t.Cleanup(closeB)

	req := makePollRequestWithToken(map[string]any{"operation_name": "ops/scoped"}, "scoped-tok")
	req.Session = ssB

	if _, err := srv.handlePoll(context.Background(), req); err != nil {
		t.Fatalf("handlePoll: %v", err)
	}

	gotB := drainProgress(progressChB, 500*time.Millisecond, 50*time.Millisecond)
	if len(gotB) == 0 {
		t.Error("session B (req.Session) received 0 progress notifications; want ≥1")
	}

	gotA := drainProgress(progressChA, 200*time.Millisecond, 50*time.Millisecond)
	if len(gotA) != 0 {
		t.Errorf("session A received %d progress notification(s); want 0 (gum-t71g session-scoping)", len(gotA))
	}
}

// --- Test L: LRO_TIMEOUT envelope -----------------------------------------------

// TestHandlePollReturnsLROTimeoutEnvelope asserts that when the poller returns
// *lro.TimeoutError, handlePoll returns a structured JSON envelope:
//
//	{"error_code":"LRO_TIMEOUT","operation_name":"ops/abc","resume_handle":"ops/abc","suggestion":<non-empty>}
func TestHandlePollReturnsLROTimeoutEnvelope(t *testing.T) {
	timeoutErr := &lro.TimeoutError{
		OperationName: "ops/abc",
		Elapsed:       10 * time.Minute,
	}
	srv := newTestServerWithPoller(t, &fakePoller{
		result: fakePollResult{err: timeoutErr},
	})

	req := makePollRequest(map[string]any{"operation_name": "ops/abc"})
	res, err := srv.handlePoll(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePoll returned Go error: %v", err)
	}
	if res == nil {
		t.Fatal("handlePoll returned nil result")
	}

	text := firstText(res)
	if text == "" {
		t.Fatal("handlePoll result has no text content")
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("result text is not JSON: %v; text: %s", err, text)
	}

	if code, _ := m["error_code"].(string); code != "LRO_TIMEOUT" {
		t.Errorf("error_code=%q; want \"LRO_TIMEOUT\"", code)
	}
	if opName, _ := m["operation_name"].(string); opName != "ops/abc" {
		t.Errorf("operation_name=%q; want \"ops/abc\"", opName)
	}
	if rh, _ := m["resume_handle"].(string); rh != "ops/abc" {
		t.Errorf("resume_handle=%q; want \"ops/abc\" (spec §1421)", rh)
	}
	if suggestion, _ := m["suggestion"].(string); suggestion == "" {
		t.Error("suggestion is empty; want non-empty human-readable hint (spec §1421)")
	}
}

// --- Test M: missing operation_name ---------------------------------------------

// TestHandlePollRejectsMissingOperationName asserts that handlePoll with empty
// or missing operation_name returns a structured error with error_code=INVALID_ARGS.
func TestHandlePollRejectsMissingOperationName(t *testing.T) {
	srv := newTestServerWithPoller(t, &fakePoller{})

	// Empty args object — operation_name absent.
	req := makePollRequest(map[string]any{})
	res, err := srv.handlePoll(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePoll returned Go error: %v", err)
	}
	if res == nil {
		t.Fatal("result is nil")
	}

	text := firstText(res)
	// Accept either structured JSON or plain "INVALID_ARGS:..." prefix.
	var m map[string]any
	if jsonErr := json.Unmarshal([]byte(text), &m); jsonErr == nil {
		if code, _ := m["error_code"].(string); code != "INVALID_ARGS" {
			t.Errorf("error_code=%q; want \"INVALID_ARGS\"", code)
		}
	} else {
		// Flat string must at least start with INVALID_ARGS.
		const prefix = "INVALID_ARGS"
		if len(text) < len(prefix) || text[:len(prefix)] != prefix {
			t.Errorf("result text %q; want INVALID_ARGS prefix or structured error_code", text)
		}
	}
}

// --- Test N: goroutine leak after cancellation -----------------------------------

// TestHandlePollGoroutineNoLeak asserts that cancelling the context passed to
// handlePoll causes the blocking poller to exit and leaves no leaked goroutines.
func TestHandlePollGoroutineNoLeak(t *testing.T) {
	defer goleak.VerifyNone(t)

	srv := newTestServerWithPoller(t, &blockingPoller{})

	ctx, cancel := context.WithCancel(context.Background())
	req := makePollRequest(map[string]any{"operation_name": "ops/leak-test"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handlePoll(ctx, req) //nolint:errcheck
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handlePoll did not exit within 2s after context cancellation")
	}
	// goleak.VerifyNone fires in the deferred call above.
}

// --- helpers ------------------------------------------------------------------

// newTestServerWithPoller creates a Server whose pollerFactory always returns p.
// This relies on the Server.pollerFactory seam that Green MUST add.
func newTestServerWithPoller(t *testing.T, p lroPoller) *Server {
	t.Helper()
	srv := NewServerWithCatalog(schemaTestDispatcher{}, minimalCatalog())
	srv.pollerFactory = func(onTick func(elapsed time.Duration)) lroPoller {
		// Wire the onTick callback into the fake poller if it supports it.
		if fp, ok := p.(*fakePoller); ok {
			fp.onTick = onTick
		}
		return p
	}
	return srv
}

// firstText returns the text of the first TextContent in a CallToolResult,
// or empty string if unavailable.
func firstText(res *sdkmcp.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*sdkmcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// lroPoller is the interface Green MUST add to server.go (unexported).
// Redeclared here so this test file compiles independently; the assignment
// srv.pollerFactory = ... will fail at compile time if Green hasn't added the
// field, which is the desired Red-team failure mode.
//
// NOTE: This local re-declaration is intentional. Do NOT remove.
// When Green adds the real lroPoller interface and pollerFactory field to
// server.go, the compiler will unify the two (same package, same name) and
// the tests will compile once the field exists. If the name or shape differs,
// the compiler will report the mismatch — which is the correct Red behaviour.
//
// If Green uses a different field name than pollerFactory, this file must be
// updated accordingly in the Green pass.

// errors for test M — ensure errors package is used.
var _ = errors.New

// TestHandlePollFailedOperationReturnsErrorEnvelope is the audit regression: a
// done LRO carrying an `error` field is a FAILED operation and MUST surface as
// an error envelope (IsError + error_code=LRO_FAILED), not a jsonResult success.
// Before the fix the agent saw IsError=false and could proceed as if the
// operation had completed.
func TestHandlePollFailedOperationReturnsErrorEnvelope(t *testing.T) {
	failed := map[string]any{
		"name": "ops/xyz",
		"done": true,
		"error": map[string]any{
			"code":    float64(7),
			"message": "PERMISSION_DENIED",
		},
	}
	srv := newTestServerWithPoller(t, &fakePoller{
		result: fakePollResult{result: failed},
	})

	req := makePollRequest(map[string]any{"operation_name": "ops/xyz"})
	res, err := srv.handlePoll(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePoll returned Go error: %v", err)
	}
	if res == nil {
		t.Fatal("handlePoll returned nil result")
	}
	if !res.IsError {
		t.Error("res.IsError=false; want true (a failed LRO must not look like success)")
	}
	text := firstText(res)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("result text is not JSON: %v; text: %s", err, text)
	}
	if code, _ := m["error_code"].(string); code != "LRO_FAILED" {
		t.Errorf("error_code=%q; want \"LRO_FAILED\"", code)
	}
	if m["error"] == nil {
		t.Error("error field missing; want the upstream operation error propagated")
	}
}

// TestHandlePollSucceededOperationStillSucceeds guards the inverse: a done LRO
// with a `response` (no `error`) is still a jsonResult success.
func TestHandlePollSucceededOperationStillSucceeds(t *testing.T) {
	ok := map[string]any{
		"name":     "ops/ok",
		"done":     true,
		"response": map[string]any{"id": "created-123"},
	}
	srv := newTestServerWithPoller(t, &fakePoller{
		result: fakePollResult{result: ok},
	})
	req := makePollRequest(map[string]any{"operation_name": "ops/ok"})
	res, err := srv.handlePoll(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePoll: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError=true; want false (a successful LRO must remain a success)")
	}
}
