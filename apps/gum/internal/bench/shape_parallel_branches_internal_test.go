package bench

import (
	"bytes"
	"testing"
)

// TestShapeParallelInvalidEnvelopeJSONReturnsEmpty pins the
// `json.Unmarshal err → empty ShapeResult, nil` arm. Spec: shapeParallel
// MUST NOT propagate decode errors for the gum_parallel envelope —
// callers treat an empty ShapeResult as "no shaping applied" and fall
// back to raw bytes. An error here would noisily corrupt the bench
// telemetry the function feeds.
func TestShapeParallelInvalidEnvelopeJSONReturnsEmpty(t *testing.T) {
	got, err := shapeParallel([]byte("definitely not json {{{"))
	if err != nil {
		t.Fatalf("want err=nil even on decode-fail; got %v", err)
	}
	if got.Body != nil || got.OutputProfile != "" || got.FieldMaskStatus != "" {
		t.Errorf("want zero ShapeResult on decode-fail; got %+v", got)
	}
}

// TestShapeParallelUnknownOpFallsBackToEncodeCompact pins the
// `shapeOne !ok → body = encodeCompact(r.Data)` arm. A batch result
// whose op_id has no registered ReleaseOp profile MUST still surface
// in the body (as the compact-encoded raw block) rather than being
// silently dropped — operators rely on the bench output to enumerate
// every dispatched op, even unprofiled ones.
func TestShapeParallelUnknownOpFallsBackToEncodeCompact(t *testing.T) {
	// op_id "no.such.op.in.registry" is not registered with
	// ProfileForReleaseOp → shapeOne returns ok=false → shapeParallel
	// must reach encodeCompact(r.Data) and emit the block.
	envelope := []byte(`{
		"batch_id": "b-unprofiled",
		"results": [
			{"op_id": "no.such.op.in.registry", "status": "ok", "data": {"k":"v"}}
		]
	}`)

	got, err := shapeParallel(envelope)
	if err != nil {
		t.Fatalf("shapeParallel: %v", err)
	}
	if !bytes.Contains(got.Body, []byte("batch_id=b-unprofiled")) {
		t.Errorf("body missing batch_id header: %q", got.Body)
	}
	if !bytes.Contains(got.Body, []byte("no.such.op.in.registry")) {
		t.Errorf("body missing op_id of unprofiled result: %q", got.Body)
	}
	// encodeCompact must have rendered the data block — at minimum the
	// value "v" from {"k":"v"} should show up.
	if !bytes.Contains(got.Body, []byte("v")) {
		t.Errorf("body missing rendered data block: %q", got.Body)
	}
}
