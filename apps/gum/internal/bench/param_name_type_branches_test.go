package bench

import "testing"

// TestParamNameTypeBranches pins every branch of paramNameType: an
// empty slice MUST return ("", "string"), a name-only slice MUST
// default typ to "string", a name+empty-type slice MUST also default
// to "string", and a fully-populated slice MUST pass both through.
//
// The "string" default protects naive-baseline schema emission from
// publishing a JSON Schema with type:"" (which is invalid JSON Schema
// and would derail downstream gain measurement).
func TestParamNameTypeBranches(t *testing.T) {
	cases := []struct {
		name     string
		in       []string
		wantName string
		wantTyp  string
	}{
		{name: "empty_slice", in: nil, wantName: "", wantTyp: "string"},
		{name: "name_only", in: []string{"limit"}, wantName: "limit", wantTyp: "string"},
		{name: "name_and_explicit_type", in: []string{"limit", "integer"}, wantName: "limit", wantTyp: "integer"},
		{name: "name_and_empty_type", in: []string{"limit", ""}, wantName: "limit", wantTyp: "string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotTyp := paramNameType(tc.in)
			if gotName != tc.wantName {
				t.Errorf("name=%q; want %q", gotName, tc.wantName)
			}
			if gotTyp != tc.wantTyp {
				t.Errorf("typ=%q; want %q", gotTyp, tc.wantTyp)
			}
		})
	}
}
