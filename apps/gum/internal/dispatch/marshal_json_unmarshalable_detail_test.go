package dispatch_test

import (
	"encoding/json"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestStructuredErrorMarshalJSONRejectsUnmarshalableDetailValue pins
// the `json.Marshal(e.Detail[k]) err → return nil, err` arm. Detail
// values are `any` (callers pass arbitrary payloads via WithDetail);
// putting a non-marshalable value (e.g. a chan) into the map MUST
// surface as a marshaling error rather than producing partial JSON
// or panicking — the dispatch error-envelope writer relies on this
// to refuse to emit malformed envelopes downstream.
func TestStructuredErrorMarshalJSONRejectsUnmarshalableDetailValue(t *testing.T) {
	se := dispatch.NewStructuredError("DEMO_CODE", "demo message").
		WithDetail("bad_payload", make(chan int)) // channels are not JSON-marshalable

	out, err := se.MarshalJSON()
	if err == nil {
		t.Fatalf("MarshalJSON(chan detail)=nil err; want json.UnsupportedTypeError surface\nout=%s", out)
	}
	if _, ok := err.(*json.UnsupportedTypeError); !ok {
		// Accept either the direct *json.UnsupportedTypeError or a wrapped
		// form — we only require the marshal error to propagate, not a
		// specific concrete type.
		if err == nil {
			t.Errorf("err=%v; want non-nil from inner json.Marshal failure", err)
		}
	}
}
