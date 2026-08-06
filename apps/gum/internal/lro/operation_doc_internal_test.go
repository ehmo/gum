package lro

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestOperationDocRejectsMalformedDone is the silent-hang guard. A `done` that
// is not a boolean used to be swallowed: hasDoneField went true, o.Done stayed
// false, and Fetch reported a finished operation as still running. The poller
// then waited out its whole deadline for a completion that had already
// happened. Unmarshal must fail so tryREST classifies the body as "not an
// Operation" and moves to the next fallback.
func TestOperationDocRejectsMalformedDone(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"name":"operations/123","done":"true"}`,
		`{"name":"operations/123","done":1}`,
		`{"name":"operations/123","done":{}}`,
	} {
		var op operationDoc
		err := json.Unmarshal([]byte(body), &op)
		if err == nil {
			t.Errorf("Unmarshal(%s) err = nil, Done = %v; want an error, not a silent false", body, op.Done)
			continue
		}
		if !strings.Contains(err.Error(), "`done` is not a boolean") {
			t.Errorf("Unmarshal(%s) err = %v; want the `done` type complaint", body, err)
		}
	}
}

// TestOperationDocRejectsMalformedName: a non-string `name` means the document
// is not an Operation. Swallowing it left Name empty, which the caller reads
// as "field absent" and can misroute.
func TestOperationDocRejectsMalformedName(t *testing.T) {
	t.Parallel()
	var op operationDoc
	err := json.Unmarshal([]byte(`{"name":123,"done":true}`), &op)
	if err == nil {
		t.Fatalf("Unmarshal err = nil, Name = %q; want an error", op.Name)
	}
	if !strings.Contains(err.Error(), "`name` is not a string") {
		t.Errorf("err = %v; want the `name` type complaint", err)
	}
}

// TestOperationDocAcceptsWellFormed keeps the fix from over-rejecting: the
// shapes a real Operation takes must still parse, including the absent-vs-false
// distinction hasDoneField exists to preserve.
func TestOperationDocAcceptsWellFormed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		body     string
		wantName string
		wantDone bool
		wantHas  bool
	}{
		{`{"name":"operations/123"}`, "operations/123", false, false},
		{`{"name":"operations/123","done":false}`, "operations/123", false, true},
		{`{"name":"operations/123","done":true}`, "operations/123", true, true},
		{`{"done":true}`, "", true, true},
		{`{}`, "", false, false},
		// An explicit JSON null is "absent" for both fields, and null
		// unmarshals into any Go type without error.
		{`{"name":null,"done":null}`, "", false, true},
	}
	for _, tc := range tests {
		var op operationDoc
		if err := json.Unmarshal([]byte(tc.body), &op); err != nil {
			t.Errorf("Unmarshal(%s) err = %v; want nil", tc.body, err)
			continue
		}
		if op.Name != tc.wantName || op.Done != tc.wantDone || op.hasDoneField != tc.wantHas {
			t.Errorf("Unmarshal(%s) = {Name:%q Done:%v has:%v}; want {%q %v %v}",
				tc.body, op.Name, op.Done, op.hasDoneField, tc.wantName, tc.wantDone, tc.wantHas)
		}
	}
}
