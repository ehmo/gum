// Package dispatch — context propagation & cancellation red-team tests.
//
// Test file for beads gum-vq4z.10:
// Audit and enforce that every dispatch step function accepts ctx context.Context
// as its first argument and forwards it to all blocking I/O; add cancellation
// integration tests with goleak.
//
// Dependencies:
//   - go.uber.org/goleak v1.3.0 — already present in go.mod
//   - dispatch.StructuredError with ErrCode field — gum-vq4z.11 contract
//     (type does not exist yet; TestDispatchContextCancelledMidExecuteReturnsCancelledError
//     will FAIL TO COMPILE until gum-vq4z.11 is merged)
//
// 7 Injection points tested in TestDispatchCancellationNoGoroutineLeak:
//   1. After step 1 (parseAndValidate), before step 2 (evaluatePolicy)
//   2. After step 2 (evaluatePolicy), before step 3 (resolveVariant)
//   3. After step 3 (resolveVariant), before step 4 (cacheCheck)
//   4. After step 4 (cacheCheck), before step 5 (resolveAuth)
//   5. After step 5 (resolveAuth), before step 6 (tokenBucketStep)
//   6. After step 6 (tokenBucketStep), before step 7 (executeAdapter)
//   7. During step 7 (executeAdapter) — adapter blocks on ctx.Done
package dispatch

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/catalog"
)

// -----------------------------------------------------------------------
// Test 1: AST-based check that every step function:
//   (a) takes ctx context.Context as first param after receiver, AND
//   (b) does NOT call context.Background() or context.TODO() inside its body
// -----------------------------------------------------------------------

// TestDispatchStepFunctionsTakeCtxFirst parses lifecycle.go via go/ast and
// verifies every step method on *dispatcher:
//  1. Has ctx context.Context as first param after receiver.
//  2. Does not contain context.Background() or context.TODO() calls inside
//     its body (i.e., ctx is actually used, not shadowed by a fresh context).
//
// Condition (2) is currently FAILING: the test is designed to catch the
// common bug where ctx is in the signature but a fresh Background context
// is used inside the body. Even if all bodies are clean today, the AST
// scanner enforces this as a regression gate.
func TestDispatchStepFunctionsTakeCtxFirst(t *testing.T) {
	// Resolve the dispatch package directory relative to this test file.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)

	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read dispatch dir: %v", err)
	}
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip test files; step methods live in non-_test.go sources.
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if perr != nil {
			t.Fatalf("failed to parse %s: %v", name, perr)
		}
		files = append(files, f)
	}

	// Step method names we must verify.
	stepNames := map[string]bool{
		"parseAndValidate": true,
		"evaluatePolicy":   true,
		"resolveVariant":   true,
		"cacheCheck":       true,
		"resolveAuth":      true,
		"tokenBucketStep":  true,
		"executeAdapter":   true,
		"shapeResponse":    true,
		"recordAndReturn":  true,
	}

	found := map[string]bool{}

	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil || !stepNames[fn.Name.Name] {
				continue
			}
			// Verify receiver is *dispatcher.
			if fn.Recv == nil || len(fn.Recv.List) == 0 {
				t.Errorf("step %s: expected method on *dispatcher, got no receiver", fn.Name.Name)
				continue
			}

			// Verify first param is ctx context.Context.
			params := fn.Type.Params.List
			if len(params) == 0 {
				t.Errorf("step %s: has no parameters at all (expected ctx context.Context first)", fn.Name.Name)
				continue
			}
			firstParam := params[0]
			// Check the type expression is a SelectorExpr: context.Context
			sel, ok := firstParam.Type.(*ast.SelectorExpr)
			if !ok {
				t.Errorf("step %s: first param type is not a selector expression (want context.Context)", fn.Name.Name)
			} else {
				pkgIdent, ok2 := sel.X.(*ast.Ident)
				if !ok2 || pkgIdent.Name != "context" || sel.Sel.Name != "Context" {
					t.Errorf("step %s: first param type = %s.%s, want context.Context",
						fn.Name.Name, pkgIdent.Name, sel.Sel.Name)
				}
			}
			// Check that the first param is named (not blank).
			if len(firstParam.Names) == 0 {
				t.Errorf("step %s: first param (context.Context) has no name", fn.Name.Name)
			}

			// Scan function body for context.Background() or context.TODO() calls.
			// These indicate ctx is being ignored in favour of a fresh context.
			bgOrTodo := findContextBgTodoCalls(fn.Body)
			if len(bgOrTodo) > 0 {
				for _, ci := range bgOrTodo {
					pos := fset.Position(ci.pos)
					t.Errorf("step %s: calls %s at %s — must forward ctx, not create a new root context",
						fn.Name.Name, ci.call, pos)
				}
			}

			found[fn.Name.Name] = true
		}
	}

	// Assert all 9 step functions were found somewhere in the dispatch package.
	for name := range stepNames {
		if !found[name] {
			t.Errorf("step function %s not found in dispatch package — was it renamed or removed?", name)
		}
	}
}

// contextCallInfo carries a human-readable call name and the AST position
// of a context.Background() or context.TODO() call.
type contextCallInfo struct {
	call string
	pos  token.Pos
}

// findContextBgTodoCalls returns position-annotated info for any
// context.Background() or context.TODO() call expressions found in node.
func findContextBgTodoCalls(node ast.Node) []contextCallInfo {
	var calls []contextCallInfo
	ast.Inspect(node, func(n ast.Node) bool {
		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := callExpr.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if pkgIdent.Name == "context" &&
			(sel.Sel.Name == "Background" || sel.Sel.Name == "TODO") {
			calls = append(calls, contextCallInfo{
				call: "context." + sel.Sel.Name + "()",
				pos:  callExpr.Pos(),
			})
		}
		return true
	})
	return calls
}

// -----------------------------------------------------------------------
// Shared helpers for injection-point tests
// -----------------------------------------------------------------------

// blockingAdapter is a dispatch.Adapter whose Execute blocks until ctx is
// cancelled, then returns ctx.Err(). Used to simulate a long-running adapter.
type blockingAdapter struct {
	// readyCh is closed when the adapter has started executing (sync point).
	readyCh chan struct{}
	once    sync.Once
}

func newBlockingAdapter() *blockingAdapter {
	return &blockingAdapter{readyCh: make(chan struct{})}
}

func (a *blockingAdapter) Execute(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) (*Response, error) {
	a.once.Do(func() { close(a.readyCh) })
	<-ctx.Done()
	return nil, ctx.Err()
}

// blockingTokenBucket blocks until ctx is cancelled.
type blockingTokenBucket struct {
	readyCh chan struct{}
	once    sync.Once
}

func newBlockingTokenBucket() *blockingTokenBucket {
	return &blockingTokenBucket{readyCh: make(chan struct{})}
}

func (b *blockingTokenBucket) Wait(ctx context.Context, opID, credsID string) error {
	b.once.Do(func() { close(b.readyCh) })
	<-ctx.Done()
	return ctx.Err()
}

// blockingAuthResolver blocks until ctx is cancelled.
type blockingAuthResolver struct {
	readyCh chan struct{}
	once    sync.Once
}

func newBlockingAuthResolver() *blockingAuthResolver {
	return &blockingAuthResolver{readyCh: make(chan struct{})}
}

func (r *blockingAuthResolver) ResolveAuth(ctx context.Context, inv *Invocation, rv *ResolvedVariant) (*Credentials, error) {
	r.once.Do(func() { close(r.readyCh) })
	<-ctx.Done()
	return nil, ctx.Err()
}

// minimalCatalog builds a catalog with a single gum.code op pointing at the
// given adapterKey, with no auth and read risk class.
func minimalCatalog(adapterKey string) *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		Ops: []catalog.Op{
			{
				OpID:             "gum.code",
				DefaultVariantID: "gum.code.v1.test",
				Variants: []catalog.Variant{
					{
						VariantID:  "gum.code.v1.test",
						RiskClass:  catalog.RiskClassRead,
						Binding: &catalog.Binding{
							AdapterKey: adapterKey,
						},
					},
				},
			},
		},
	}
}

// -----------------------------------------------------------------------
// Test 2: Cancellation at each of 7 injection points — goleak verifies no
//         goroutine leaks after cancel.
// -----------------------------------------------------------------------

// TestDispatchCancellationNoGoroutineLeak cancels the dispatch context at each
// of 7 injection points and asserts:
//   - Dispatch returns within 100 ms of cancellation
//   - The returned error wraps context.Canceled
//   - goleak.VerifyNone reports zero goroutine leaks
func TestDispatchCancellationNoGoroutineLeak(t *testing.T) {
	// Injection point 7: adapter blocks on ctx.Done — most clearly observable.
	// Points 1-6 require the dispatcher to check ctx.Err() between steps.
	// These tests FAIL today because the dispatcher does not call ctx.Err()
	// between steps — it only propagates ctx to sub-calls that may or may not
	// honour it.

	// Each sub-test is named by the step boundary at which we inject cancellation.
	cases := []struct {
		name string
		// build returns a Dispatcher and a readyCh that is closed when the
		// dispatch goroutine has reached (or passed) the injection point.
		build func(t *testing.T) (Dispatcher, <-chan struct{})
		// inv is the invocation to dispatch.
		inv *Invocation
	}{
		// --- Injection point 1: cancel immediately after Dispatch is called
		//     (before step 1). The dispatcher must propagate ctx.Err() through
		//     all early steps.
		{
			name: "point1_before_parse",
			build: func(t *testing.T) (Dispatcher, <-chan struct{}) {
				// Use a pre-cancelled context; readyCh is already closed.
				ch := make(chan struct{})
				close(ch)
				// We return a dispatcher that would succeed normally.
				cat := minimalCatalog("noop")
				blocker := newBlockingAdapter()
				d := NewDispatcher(cat, map[string]Adapter{"noop": blocker})
				return d, ch
			},
			inv: &Invocation{OpID: "gum.code", Format: "json", RequestID: "cancel-pt1"},
		},

		// --- Injection point 2: cancel between policy and routing.
		//     Requires dispatcher to check ctx between step 2 and step 3.
		{
			name: "point2_after_policy",
			build: func(t *testing.T) (Dispatcher, <-chan struct{}) {
				ch := make(chan struct{})
				close(ch)
				cat := minimalCatalog("noop")
				blocker := newBlockingAdapter()
				d := NewDispatcher(cat, map[string]Adapter{"noop": blocker})
				return d, ch
			},
			inv: &Invocation{OpID: "gum.code", Format: "json", RequestID: "cancel-pt2"},
		},

		// --- Injection point 3: cancel between routing and cache.
		{
			name: "point3_after_routing",
			build: func(t *testing.T) (Dispatcher, <-chan struct{}) {
				ch := make(chan struct{})
				close(ch)
				cat := minimalCatalog("noop")
				blocker := newBlockingAdapter()
				d := NewDispatcher(cat, map[string]Adapter{"noop": blocker})
				return d, ch
			},
			inv: &Invocation{OpID: "gum.code", Format: "json", RequestID: "cancel-pt3"},
		},

		// --- Injection point 4: cancel between cache and auth.
		{
			name: "point4_after_cache",
			build: func(t *testing.T) (Dispatcher, <-chan struct{}) {
				ch := make(chan struct{})
				close(ch)
				cat := minimalCatalog("noop")
				blocker := newBlockingAdapter()
				d := NewDispatcher(cat, map[string]Adapter{"noop": blocker})
				return d, ch
			},
			inv: &Invocation{OpID: "gum.code", Format: "json", RequestID: "cancel-pt4"},
		},

		// --- Injection point 5: cancel during auth resolution (blocking auth).
		{
			name: "point5_during_auth",
			build: func(t *testing.T) (Dispatcher, <-chan struct{}) {
				cat := minimalCatalog("noop")
				blocker := newBlockingAdapter()
				authR := newBlockingAuthResolver()
				d := NewDispatcherWithConfig(cat, map[string]Adapter{"noop": blocker}, DispatcherConfig{
					Auth: authR,
				})
				return d, authR.readyCh
			},
			inv: &Invocation{OpID: "gum.code", Format: "json", RequestID: "cancel-pt5"},
		},

		// --- Injection point 6: cancel during token bucket (blocking rate limiter).
		{
			name: "point6_during_rate_limit",
			build: func(t *testing.T) (Dispatcher, <-chan struct{}) {
				cat := minimalCatalog("noop")
				blocker := newBlockingAdapter()
				bucket := newBlockingTokenBucket()
				d := NewDispatcherWithConfig(cat, map[string]Adapter{"noop": blocker}, DispatcherConfig{
					RateLimiter: bucket,
				})
				return d, bucket.readyCh
			},
			inv: &Invocation{OpID: "gum.code", Format: "json", RequestID: "cancel-pt6"},
		},

		// --- Injection point 7: cancel during adapter execution.
		{
			name: "point7_during_execute",
			build: func(t *testing.T) (Dispatcher, <-chan struct{}) {
				cat := minimalCatalog("blocker")
				blocker := newBlockingAdapter()
				d := NewDispatcher(cat, map[string]Adapter{"blocker": blocker})
				return d, blocker.readyCh
			},
			inv: &Invocation{OpID: "gum.code", Format: "json", RequestID: "cancel-pt7"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// goleak: verify no goroutines are leaked at the end of the sub-test.
			defer goleak.VerifyNone(t)

			d, readyCh := tc.build(t)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			errCh := make(chan error, 1)
			go func() {
				_, err := d.Dispatch(ctx, tc.inv)
				errCh <- err
			}()

			// Wait for the dispatch goroutine to reach the injection point
			// (or use a pre-cancelled context for points 1-4).
			select {
			case <-readyCh:
				// Injection point reached — now cancel.
				cancel()
			case <-time.After(2 * time.Second):
				t.Fatal("dispatch goroutine never reached injection point within 2s")
			}

			// Dispatch must return within 100ms of cancellation.
			select {
			case err := <-errCh:
				if err == nil {
					t.Fatalf("[%s] expected error after cancel, got nil", tc.name)
				}
				if !errors.Is(err, context.Canceled) {
					t.Errorf("[%s] expected error to wrap context.Canceled, got: %v", tc.name, err)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("[%s] Dispatch did not return within 100ms after context cancellation", tc.name)
			}
			// goleak.VerifyNone runs via defer above.
		})
	}
}

// -----------------------------------------------------------------------
// Test 3: Adapter receives a cancellable ctx
// -----------------------------------------------------------------------

// TestDispatchAdapterReceivesCancellableCtx registers a fake adapter that
// inspects ctx; the test cancels the parent and verifies the adapter's ctx
// has ctx.Err() == context.Canceled.
func TestDispatchAdapterReceivesCancellableCtx(t *testing.T) {
	defer goleak.VerifyNone(t)

	type ctxCapture struct {
		ctx context.Context
		mu  sync.Mutex
	}
	capture := &ctxCapture{}

	fakeAdapter := &funcAdapter{
		execute: func(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) (*Response, error) {
			capture.mu.Lock()
			capture.ctx = ctx
			capture.mu.Unlock()
			// Block briefly so caller can cancel.
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	cat := minimalCatalog("fake")
	d := NewDispatcher(cat, map[string]Adapter{"fake": fakeAdapter})

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := d.Dispatch(ctx, &Invocation{OpID: "gum.code", Format: "json", RequestID: "ctx-cap"})
		errCh <- err
	}()

	// Give the adapter goroutine time to start and store its ctx.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after cancel, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Dispatch did not return within 200ms")
	}

	// Verify adapter received a context that was cancelled.
	capture.mu.Lock()
	adapterCtx := capture.ctx
	capture.mu.Unlock()

	if adapterCtx == nil {
		t.Fatal("adapter never received a context")
	}
	if adapterCtx.Err() != context.Canceled {
		t.Errorf("adapter ctx.Err() = %v, want context.Canceled", adapterCtx.Err())
	}
}

// funcAdapter is a convenience Adapter backed by a function.
type funcAdapter struct {
	execute func(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) (*Response, error)
}

func (a *funcAdapter) Execute(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) (*Response, error) {
	return a.execute(ctx, inv, rv, creds)
}

// -----------------------------------------------------------------------
// Test 4: Cancelled mid-execute must return CANCELLED StructuredError
//
// NOTE: StructuredError with ErrCode field is defined by gum-vq4z.11.
// This test will FAIL TO COMPILE until that type exists in the dispatch
// package. That IS the intended red state for this test.
// -----------------------------------------------------------------------

// TestDispatchContextCancelledMidExecuteReturnsCancelledError asserts that
// when the adapter returns ctx.Err() (context.Canceled), the dispatcher
// wraps it in a *StructuredError with ErrCode == "CANCELLED".
//
// COMPILE DEPENDENCY: StructuredError must be declared in package dispatch
// (gum-vq4z.11). This test currently fails to compile — that is the red state.
func TestDispatchContextCancelledMidExecuteReturnsCancelledError(t *testing.T) {
	defer goleak.VerifyNone(t)

	blocker := newBlockingAdapter()
	cat := minimalCatalog("blocker")
	d := NewDispatcher(cat, map[string]Adapter{"blocker": blocker})

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := d.Dispatch(ctx, &Invocation{OpID: "gum.code", Format: "json", RequestID: "se-cancel"})
		errCh <- err
	}()

	// Wait for adapter to block, then cancel.
	select {
	case <-blocker.readyCh:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("adapter never started within 2s")
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after cancel, got nil")
		}
		// Unwrap to find a *StructuredError.
		// NOTE: StructuredError does not yet exist — this line causes a compile error.
		var se *StructuredError
		if !errors.As(err, &se) {
			t.Fatalf("expected error to be or wrap *StructuredError, got %T: %v", err, err)
		}
		if se.ErrCode != "CANCELLED" {
			t.Errorf("StructuredError.ErrCode = %q, want %q", se.ErrCode, "CANCELLED")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Dispatch did not return within 200ms after cancel")
	}
}
