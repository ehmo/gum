package dispatch_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
)

// gum-9l5c: the shaping notice offers `--format raw` to recover a dropped
// field, and raw bypasses step 8's annotator. ShapedResponse.AnnotationPaths is
// what lets the notice say which adapter-added fields that trade costs, so the
// kernel has to carry the annotator's paths out of step 8 — minus any the
// profile then removed again, which raw does not cost because the shaped body
// never had them either.

// annotatingFixedAdapter returns upstreamBody and adds two fields: one the
// test profile keeps and one it drops.
type annotatingFixedAdapter struct{ fixedAdapter }

func (a *annotatingFixedAdapter) AnnotateResponse(_ *dispatch.Invocation, _ *dispatch.ResolvedVariant, body []byte) ([]byte, []string) {
	const annotated = `{"results":[{"text":"a","matchedInputs":["a","A"],"metrics":{"avg":1,"monthly":[{"m":"JULY"}]}}],"unmatchedInputs":["b"]}`
	return []byte(annotated), []string{"results.matchedInputs", "unmatchedInputs"}
}

func dispatchAnnotated(t *testing.T, format string, keep []string) *dispatch.ShapedResponse {
	t.Helper()

	const opID = "test.shape.annotated"
	const adapterKey = "test.adapter.annotated"
	disp := dispatch.NewDispatcherWithConfig(
		minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: &annotatingFixedAdapter{}},
		dispatch.DispatcherConfig{},
	)

	shaped, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:          opID,
		Args:          map[string]any{},
		Format:        format,
		RequestID:     "annotated-1",
		OutputProfile: &profile.Profile{KeepFields: keep},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	return shaped
}

func TestShapedResponseCarriesSurvivingAnnotationPaths(t *testing.T) {
	shaped := dispatchAnnotated(t, "json", []string{"results.text", "results.metrics.avg", "unmatchedInputs"})

	if want := []string{"unmatchedInputs"}; !reflect.DeepEqual(shaped.AnnotationPaths, want) {
		t.Fatalf("AnnotationPaths = %v; want %v", shaped.AnnotationPaths, want)
	}
	// The dropped annotation is reported as dropped, not as an annotation: the
	// caller who switches to raw loses nothing it did not already lose.
	found := false
	for _, p := range shaped.DroppedPaths {
		if p == "results.matchedInputs" {
			found = true
		}
	}
	if !found {
		t.Errorf("DroppedPaths = %v; want results.matchedInputs among them", shaped.DroppedPaths)
	}
}

// Every annotation survives when the profile keeps them, which is the arm that
// makes the notice name both fields.
func TestShapedResponseKeepsEveryAnnotationPath(t *testing.T) {
	shaped := dispatchAnnotated(t, "json",
		[]string{"results.text", "results.matchedInputs", "results.metrics.avg", "unmatchedInputs"})

	want := []string{"results.matchedInputs", "unmatchedInputs"}
	if !reflect.DeepEqual(shaped.AnnotationPaths, want) {
		t.Errorf("AnnotationPaths = %v; want %v", shaped.AnnotationPaths, want)
	}
}

// The profile dropped both annotations, so there is nothing for the notice to
// warn about and the hint stays the plain one.
func TestShapedResponseReportsNoAnnotationPathsWhenAllDropped(t *testing.T) {
	shaped := dispatchAnnotated(t, "json", []string{"results.text", "results.metrics.avg"})

	if len(shaped.AnnotationPaths) != 0 {
		t.Errorf("AnnotationPaths = %v; want none when the profile removed every annotation", shaped.AnnotationPaths)
	}
}

// --format raw never reaches the annotator, so it reports no annotation paths.
func TestRawFormatReportsNoAnnotationPaths(t *testing.T) {
	shaped := dispatchAnnotated(t, "raw", []string{"results.text"})

	if len(shaped.AnnotationPaths) != 0 {
		t.Errorf("AnnotationPaths = %v; want none for --format raw", shaped.AnnotationPaths)
	}
	if string(shaped.Body) != upstreamBody {
		t.Errorf("raw body = %s; want the executor body verbatim", shaped.Body)
	}
}
