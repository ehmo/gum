package dispatch

import "testing"

// TestStructuredErrorFromEnvelope pins the audit fix: an adapter (the plugin
// path) that packs a JSON error envelope into Response.Body must have its
// error_code / retryable / retry_after_ms reconstructed, and a non-envelope body
// must return nil so the caller falls back to the opaque error.
func TestStructuredErrorFromEnvelope(t *testing.T) {
	body := []byte(`{"error_code":"RATE_LIMITED","message":"slow down","retryable":true,"retry_after_ms":1500}`)
	se := structuredErrorFromEnvelope(body)
	if se == nil {
		t.Fatal("nil; want a StructuredError from a valid envelope")
	}
	if se.ErrCode != ErrCodeRateLimited {
		t.Errorf("ErrCode=%q; want RATE_LIMITED", se.ErrCode)
	}
	if !se.Retryable {
		t.Error("Retryable=false; want true")
	}
	if se.Detail["retry_after_ms"] != int64(1500) {
		t.Errorf("retry_after_ms=%v; want int64(1500)", se.Detail["retry_after_ms"])
	}

	// Bodies that are not error envelopes must return nil (fall back to the
	// opaque error rather than fabricate a code).
	for _, b := range [][]byte{
		nil, []byte(``), []byte(`{}`), []byte(`{"foo":1}`),
		[]byte(`not json`), []byte(`"a bare string"`), []byte(`{"message":"no code"}`),
	} {
		if got := structuredErrorFromEnvelope(b); got != nil {
			t.Errorf("structuredErrorFromEnvelope(%q)=%v; want nil", b, got)
		}
	}
}
