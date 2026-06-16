package adapters

import (
	"reflect"
	"testing"
)

// TestMarshalBodyShapes exercises the full method-dispatch matrix and
// the raw-type fast paths. The function is responsible for the
// typed-rest-sdk request body — drift here produces malformed PATCH/PUT
// payloads that the JSON parser on the server side rejects.
func TestMarshalBodyShapes(t *testing.T) {
	cases := []struct {
		name   string
		raw    any
		method string
		want   []byte
		err    bool
	}{
		{"nil_raw", nil, "POST", nil, false},
		{"get_with_body_drops", map[string]any{"k": "v"}, "GET", nil, false},
		{"delete_drops_body", "abc", "DELETE", nil, false},
		{"post_empty_bytes_drops", []byte{}, "POST", nil, false},
		{"post_bytes_verbatim", []byte("hello"), "POST", []byte("hello"), false},
		{"put_empty_string_drops", "", "PUT", nil, false},
		{"patch_string_verbatim", "x", "PATCH", []byte("x"), false},
		{"post_map_marshals_json", map[string]any{"k": "v"}, "POST", []byte(`{"k":"v"}`), false},
		{"post_int_marshals_json", 7, "POST", []byte("7"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := marshalBody(tc.raw, tc.method)
			if tc.err && err == nil {
				t.Fatal("want err; got nil")
			}
			if !tc.err && err != nil {
				t.Fatalf("got err %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %s; want %s", got, tc.want)
			}
		})
	}
}
