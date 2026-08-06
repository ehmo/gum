package dispatch_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
)

// upstreamBody is the executor response used by the shaping tests below: two
// kept fields and one field the whitelist removes.
const upstreamBody = `{"results":[{"text":"a","metrics":{"avg":1,"monthly":[{"m":"JULY"}]}}]}`

// fixedAdapter returns upstreamBody and counts its calls, so a test can tell a
// cache hit from a second upstream call.
type fixedAdapter struct{ calls int }

func (a *fixedAdapter) Execute(_ context.Context, _ *dispatch.Invocation, _ *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	a.calls++
	return &dispatch.Response{
		Body:       []byte(upstreamBody),
		Format:     "json",
		StatusCode: 200,
		BytesOut:   len(upstreamBody),
	}, nil
}

func keepTextAndAvg() *profile.Profile {
	return &profile.Profile{KeepFields: []string{"results.text", "results.metrics.avg"}}
}

// TestShapedResponseCarriesDroppedPaths: the kernel must hand the presentation
// layer the list of fields the profile removed. Without it the CLI and the MCP
// server cannot tell the caller that the body is incomplete (gum-bpx0).
func TestShapedResponseCarriesDroppedPaths(t *testing.T) {
	const opID = "test.shape.dropped"
	const adapterKey = "test.adapter.dropped"
	adapter := &fixedAdapter{}
	disp := dispatch.NewDispatcherWithConfig(
		minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{},
	)

	shaped, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:          opID,
		Args:          map[string]any{},
		Format:        "json",
		RequestID:     "dropped-1",
		OutputProfile: keepTextAndAvg(),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	want := []string{"results.metrics.monthly"}
	if len(shaped.DroppedPaths) != 1 || shaped.DroppedPaths[0] != want[0] {
		t.Fatalf("DroppedPaths = %v; want %v", shaped.DroppedPaths, want)
	}
}

// TestRawFormatReportsNoDroppedPaths: --format raw is the escape hatch, so it
// returns the executor body untouched and has nothing to report.
func TestRawFormatReportsNoDroppedPaths(t *testing.T) {
	const opID = "test.shape.raw"
	const adapterKey = "test.adapter.raw"
	disp := dispatch.NewDispatcherWithConfig(
		minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: &fixedAdapter{}},
		dispatch.DispatcherConfig{},
	)

	shaped, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:          opID,
		Args:          map[string]any{},
		Format:        "raw",
		RequestID:     "raw-1",
		OutputProfile: keepTextAndAvg(),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(shaped.DroppedPaths) != 0 {
		t.Errorf("DroppedPaths = %v; want none for --format raw", shaped.DroppedPaths)
	}
	if string(shaped.Body) != upstreamBody {
		t.Errorf("raw body = %s; want the executor body verbatim", shaped.Body)
	}
}

// TestCacheHitIsShapedLikeCacheMiss pins gum-n110. The cache stores the raw
// executor body, so the hit path has to run step 8 too. It used to return
// cached.Body verbatim: an identical warm call answered with unshaped upstream
// JSON in a format the caller never asked for, and reported no dropped fields.
func TestCacheHitIsShapedLikeCacheMiss(t *testing.T) {
	const opID = "test.cache.shaped"
	const adapterKey = "test.adapter.cache.shaped"
	adapter := &fixedAdapter{}
	disp := dispatch.NewDispatcherWithConfig(
		minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{Cache: cache.NewMemCache(100, time.Minute)},
	)

	call := func(reqID string) *dispatch.ShapedResponse {
		t.Helper()
		shaped, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
			OpID:          opID,
			Args:          map[string]any{},
			Format:        "json",
			RequestID:     reqID,
			OutputProfile: keepTextAndAvg(),
		})
		if err != nil {
			t.Fatalf("Dispatch(%s): %v", reqID, err)
		}
		return shaped
	}

	cold := call("cold")
	warm := call("warm")

	if adapter.calls != 1 {
		t.Fatalf("adapter.calls = %d; want 1 (second call must be a cache hit)", adapter.calls)
	}
	if string(cold.Body) != string(warm.Body) {
		t.Errorf("warm body  = %s\ncold body = %s\nthe two must match", warm.Body, cold.Body)
	}
	if warm.Format != cold.Format {
		t.Errorf("warm Format = %q; cold Format = %q", warm.Format, cold.Format)
	}
	if len(warm.DroppedPaths) != len(cold.DroppedPaths) {
		t.Errorf("warm DroppedPaths = %v; cold = %v", warm.DroppedPaths, cold.DroppedPaths)
	}

	// And the shaping actually happened: the dropped field is absent from both.
	for name, body := range map[string][]byte{"cold": cold.Body, "warm": warm.Body} {
		var v map[string]any
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatalf("%s body is not JSON: %v", name, err)
		}
		rows, _ := v["results"].([]any)
		if len(rows) != 1 {
			t.Fatalf("%s: results = %v", name, v["results"])
		}
		metrics, _ := rows[0].(map[string]any)["metrics"].(map[string]any)
		if _, present := metrics["monthly"]; present {
			t.Errorf("%s: profile did not run — %s", name, body)
		}
	}
}

// TestOpaqueResponsesAreNotCached: the cache stores bytes with no format, so a
// hit on an opaque (Format "raw") body would come back labelled "json" and take
// the step-8 fallback on every warm call. Such responses stay out of the cache
// and are returned verbatim on every call (gum-n110).
func TestOpaqueResponsesAreNotCached(t *testing.T) {
	const opID = "test.cache.opaque"
	const adapterKey = "test.adapter.opaque"
	adapter := &opaqueAdapter{}
	disp := dispatch.NewDispatcherWithConfig(
		minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{Cache: cache.NewMemCache(100, time.Minute)},
	)

	for _, reqID := range []string{"cold", "warm"} {
		shaped, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
			OpID:      opID,
			Args:      map[string]any{},
			Format:    "json",
			RequestID: reqID,
		})
		if err != nil {
			t.Fatalf("Dispatch(%s): %v", reqID, err)
		}
		if string(shaped.Body) != opaquePayload {
			t.Errorf("%s body = %q; want %q", reqID, shaped.Body, opaquePayload)
		}
	}
	if adapter.calls != 2 {
		t.Fatalf("adapter.calls = %d; want 2 (opaque bodies must not be cached)", adapter.calls)
	}
}

const opaquePayload = "printed output, not JSON\n"

// opaqueAdapter reports Format "raw", the signal gum.code uses for printed
// output. The cache does not carry the format, so a warm call sees "json".
type opaqueAdapter struct{ calls int }

func (a *opaqueAdapter) Execute(_ context.Context, _ *dispatch.Invocation, _ *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	a.calls++
	return &dispatch.Response{
		Body:       []byte(opaquePayload),
		Format:     "raw",
		StatusCode: 200,
		BytesOut:   len(opaquePayload),
	}, nil
}

// TestCacheHitRejectsBadFormatLikeCacheMiss: a bogus --format is the caller's
// error on both paths. The warm path must not swallow it via the verbatim
// fallback.
func TestCacheHitRejectsBadFormatLikeCacheMiss(t *testing.T) {
	const opID = "test.cache.badformat"
	const adapterKey = "test.adapter.badformat"
	disp := dispatch.NewDispatcherWithConfig(
		minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: &fixedAdapter{}},
		dispatch.DispatcherConfig{Cache: cache.NewMemCache(100, time.Minute)},
	)

	if _, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID: opID, Args: map[string]any{}, Format: "json", RequestID: "warm-seed",
	}); err != nil {
		t.Fatalf("seed Dispatch: %v", err)
	}
	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID: opID, Args: map[string]any{}, Format: "yaml", RequestID: "warm-bad",
	})
	if err == nil {
		t.Fatal("cache hit accepted format=yaml; cold path rejects it with INVALID_ARGS")
	}
}
