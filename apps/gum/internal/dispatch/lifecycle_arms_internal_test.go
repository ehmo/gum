package dispatch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
)

// countdownCtx returns nil from Err() for the first budget calls and
// context.Canceled from then on. Sweeping the budget walks the cancellation
// checkpoint that sits after every lifecycle step, without needing a real
// racing cancel.
type countdownCtx struct {
	context.Context
	budget int
}

func (c *countdownCtx) Err() error {
	if c.budget > 0 {
		c.budget--
		return nil
	}
	return context.Canceled
}

// jsonAdapter answers every call with the same body.
func jsonAdapter(body string) *funcAdapter {
	return &funcAdapter{execute: func(context.Context, *Invocation, *ResolvedVariant, *Credentials) (*Response, error) {
		return &Response{Body: []byte(body), Format: "json"}, nil
	}}
}

// TestDispatchStopsAtEveryCancellationCheckpoint covers the checkCancelled
// guard after each step. Spec §3.1 requires a cancelled call to stop between
// steps rather than run the executor and discard the result, so every
// checkpoint below the completion budget must answer CANCELLED.
func TestDispatchStopsAtEveryCancellationCheckpoint(t *testing.T) {
	cat := minimalCatalog("ok")
	d := NewDispatcher(cat, map[string]Adapter{"ok": jsonAdapter(`{"a":1}`)})

	completed := -1
	const maxCheckpoints = 12
	for budget := 0; budget <= maxCheckpoints; budget++ {
		ctx := &countdownCtx{Context: context.Background(), budget: budget}
		_, err := d.Dispatch(ctx, &Invocation{OpID: "gum.code", Format: "json"})
		if err == nil {
			completed = budget
			break
		}
		var se *StructuredError
		if !errors.As(err, &se) {
			t.Fatalf("budget %d: error %v is not structured", budget, err)
		}
		if se.ErrCode != ErrCodeCancelled {
			t.Fatalf("budget %d: error_code = %s; want %s", budget, se.ErrCode, ErrCodeCancelled)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("budget %d: error does not wrap context.Canceled", budget)
		}
	}
	if completed < 1 {
		t.Fatalf("no budget under %d completed the dispatch; got %d", maxCheckpoints, completed)
	}
}

// teeBlockedConfig returns a TeeConfig whose ProfileDir cannot be created,
// because a regular file sits where the parent directory would go. Every tee
// write under it fails with ENOTDIR.
func teeBlockedConfig(t *testing.T) TeeConfig {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	return TeeConfig{ProfileDir: filepath.Join(blocker, "profile"), Mode: "always"}
}

// TestDispatchSurvivesAnUnwritableTeeDirectory covers the tee-warn arms on both
// the cold and the warm path. The §9.0 artifact is a recovery aid, so a tee the
// filesystem refuses must degrade to a log line, not fail a call that otherwise
// succeeded.
func TestDispatchSurvivesAnUnwritableTeeDirectory(t *testing.T) {
	cat := minimalCatalog("ok")
	d := NewDispatcherWithConfig(cat, map[string]Adapter{"ok": jsonAdapter(`{"a":1}`)}, DispatcherConfig{
		Tee:   teeBlockedConfig(t),
		Cache: cache.NewMemCache(0, 0),
	})

	cold, err := d.Dispatch(context.Background(), &Invocation{OpID: "gum.code", Format: "json"})
	if err != nil {
		t.Fatalf("cold dispatch with an unwritable tee dir failed: %v", err)
	}
	if cold.FullResultPath != "" {
		t.Errorf("cold full_result_path = %q; want none after a failed write", cold.FullResultPath)
	}

	warm, err := d.Dispatch(context.Background(), &Invocation{OpID: "gum.code", Format: "json"})
	if err != nil {
		t.Fatalf("warm dispatch with an unwritable tee dir failed: %v", err)
	}
	if warm.FullResultPath != "" {
		t.Errorf("warm full_result_path = %q; want none after a failed write", warm.FullResultPath)
	}
}

// TestDispatchServesAnUnshapeableCachedBodyVerbatim covers two arms that share
// one cause: a cached body the profile pipeline cannot parse. The cold call
// must surface the shaping error, and the warm call must NOT — the bytes are
// already proven to be what upstream sent, so serving them verbatim beats
// failing a call that would have succeeded cold.
func TestDispatchServesAnUnshapeableCachedBodyVerbatim(t *testing.T) {
	cat := minimalCatalog("opaque")
	d := NewDispatcherWithConfig(cat, map[string]Adapter{"opaque": jsonAdapter("not json at all")}, DispatcherConfig{
		Cache: cache.NewMemCache(0, 0),
	})

	if _, err := d.Dispatch(context.Background(), &Invocation{OpID: "gum.code", Format: "json"}); err == nil {
		t.Fatal("cold dispatch of an unparseable body returned no error")
	}

	warm, err := d.Dispatch(context.Background(), &Invocation{OpID: "gum.code", Format: "json"})
	if err != nil {
		t.Fatalf("warm dispatch failed instead of serving the cached body verbatim: %v", err)
	}
	if string(warm.Body) != "not json at all" {
		t.Errorf("warm body = %q; want the cached bytes unchanged", warm.Body)
	}
	if warm.Expression == nil || warm.Expression.Profile != "_raw" {
		t.Errorf("warm expression = %+v; want the _raw sentinel profile", warm.Expression)
	}
	if warm.StructuredContent != nil {
		t.Errorf("warm structured_content = %v; want nil for a non-JSON body", warm.StructuredContent)
	}
}

// TestDispatchCarriesValidationWarningsToTheEnvelope covers the cold-path
// warning append. A request field whose declared default cannot decode to its
// declared type is skipped rather than sent, and the caller only learns the
// default did not apply through this warning.
func TestDispatchCarriesValidationWarningsToTheEnvelope(t *testing.T) {
	cat := minimalCatalog("ok")
	cat.Ops[0].RequestFields = []catalog.RequestField{
		{Name: "pageSize", Location: catalog.RequestFieldQuery, Type: "integer", Default: "not-a-number"},
	}
	d := NewDispatcher(cat, map[string]Adapter{"ok": jsonAdapter(`{"a":1}`)})

	shaped, err := d.Dispatch(context.Background(), &Invocation{OpID: "gum.code", Format: "json"})
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if len(shaped.ValidationWarnings) != 1 {
		t.Fatalf("ValidationWarnings = %v; want exactly one", shaped.ValidationWarnings)
	}
	if !strings.Contains(shaped.ValidationWarnings[0], "pageSize") {
		t.Errorf("warning %q does not name the offending field", shaped.ValidationWarnings[0])
	}
}

// TestDispatchInjectsTheProfileFieldMask covers the field-mask injection that
// feeds a profile's mask to upstream as a `fields` arg. An explicit caller arg
// must win, because it is the narrower instruction.
func TestDispatchInjectsTheProfileFieldMask(t *testing.T) {
	cat := minimalCatalog("spy")
	var seen []map[string]any
	spy := &funcAdapter{execute: func(_ context.Context, inv *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
		captured := map[string]any{}
		for k, v := range inv.Args {
			captured[k] = v
		}
		seen = append(seen, captured)
		return &Response{Body: []byte(`{"a":1}`), Format: "json"}, nil
	}}
	d := NewDispatcher(cat, map[string]Adapter{"spy": spy})
	prof := &profile.Profile{FieldMask: "a,b", FieldMaskMode: profile.FieldMaskModeUpstream}

	if _, err := d.Dispatch(context.Background(), &Invocation{OpID: "gum.code", Format: "json", OutputProfile: prof}); err != nil {
		t.Fatalf("dispatch with a profile mask failed: %v", err)
	}
	if got := seen[0]["fields"]; got != "a,b" {
		t.Errorf("injected fields = %v; want the profile mask", got)
	}

	explicit := &Invocation{OpID: "gum.code", Format: "json", OutputProfile: prof, Args: map[string]any{"fields": "z"}}
	if _, err := d.Dispatch(context.Background(), explicit); err != nil {
		t.Fatalf("dispatch with an explicit fields arg failed: %v", err)
	}
	if got := seen[1]["fields"]; got != "z" {
		t.Errorf("fields = %v; the caller's explicit arg must win", got)
	}
}
