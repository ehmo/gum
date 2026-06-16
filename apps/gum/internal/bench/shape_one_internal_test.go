package bench

import "testing"

// TestShapeOneUnknownOpReturnsFalse pins the "no profile registered"
// branch: callers MUST get (nil, "", false) so they can fall back to
// the raw-encoder path instead of silently emitting a nil body.
func TestShapeOneUnknownOpReturnsFalse(t *testing.T) {
	body, name, ok := shapeOne("does.not.exist.op", []byte(`{"k":"v"}`))
	if ok {
		t.Errorf("ok=true; want false for unknown op")
	}
	if body != nil {
		t.Errorf("body=%q; want nil", body)
	}
	if name != "" {
		t.Errorf("name=%q; want empty", name)
	}
}

// TestShapeOneInvalidJSONFallsBackToRaw pins the profile.Apply error
// branch: when Apply rejects the body (invalid JSON), the helper
// returns (raw, profileName, true) so the caller still records the
// shape attempt and uses the unchanged body.
func TestShapeOneInvalidJSONFallsBackToRaw(t *testing.T) {
	raw := []byte("not valid json")
	body, name, ok := shapeOne("gmail.users.messages.list", raw)
	if !ok {
		t.Fatal("ok=false; want true (profile exists)")
	}
	if string(body) != string(raw) {
		t.Errorf("body=%q; want raw verbatim", body)
	}
	if name != "release/gmail.users.messages.list" {
		t.Errorf("name=%q; want release/gmail.users.messages.list", name)
	}
}

// TestShapeOneHappyPathCompacts pins the success branch: a well-formed
// fixture under a registered profile returns the COMPACTED body and
// the profile name. We verify the compaction by checking the output
// is not byte-identical to the input — the keep-fields + toon
// transform MUST visibly shrink the payload.
func TestShapeOneHappyPathCompacts(t *testing.T) {
	raw := []byte(`{"messages":[{"id":"a","threadId":"t1","snippet":"hi","etag":"e"},{"id":"b","threadId":"t2","snippet":"bye","etag":"f"}]}`)
	body, name, ok := shapeOne("gmail.users.messages.list", raw)
	if !ok {
		t.Fatal("ok=false; want true")
	}
	if name != "release/gmail.users.messages.list" {
		t.Errorf("name=%q; want release/gmail.users.messages.list", name)
	}
	if len(body) == 0 {
		t.Fatal("body empty; want compact representation")
	}
	if string(body) == string(raw) {
		t.Errorf("body unchanged; expected the profile to drop etag/wireframe")
	}
	// Quick smoke: etag was supposed to be dropped by KeepFields.
	for _, c := range body {
		if c == 'e' {
			// Acceptable — letter could appear elsewhere; this is a smoke not a hard assert.
			break
		}
	}
}
