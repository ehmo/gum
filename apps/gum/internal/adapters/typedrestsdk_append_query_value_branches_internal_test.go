package adapters

import (
	"net/url"
	"testing"
)

// TestAppendQueryValueNilSkips pins the `case nil → return` arm.
// A typed-rest-sdk Invocation may carry a parameter whose value is nil
// (e.g. an optional field the caller left unset); appendQueryValue MUST
// NOT add an empty "?key=" pair for it — that would change the wire-form
// of the request and could trip server-side strict parsers.
func TestAppendQueryValueNilSkips(t *testing.T) {
	vals := url.Values{}
	appendQueryValue(vals, "optional", nil)

	if got := vals.Encode(); got != "" {
		t.Errorf("vals.Encode()=%q; want empty (nil value MUST be skipped, no ?optional= emitted)", got)
	}
	if _, ok := vals["optional"]; ok {
		t.Errorf("key 'optional' present despite nil value: %v", vals["optional"])
	}
}
