package bench

import "testing"

// TestReleaseShaperUnknownOpReturnsEmptyResult pins releaseShaper's
// `!ok → return gain.ShapeResult{}, nil` arm
// (release_savings.go:122-124). When shapeOne reports no profile is
// registered for opID (and the op isn't the special-cased
// gum_parallel), releaseShaper MUST surface an empty ShapeResult so
// gain.ComputeReleaseSavings treats the op as "raw-only" rather than
// blow up. The test is the only signal a release-blog pipeline has
// to keep that contract intact across registry refactors.
func TestReleaseShaperUnknownOpReturnsEmptyResult(t *testing.T) {
	got, err := releaseShaper("nonexistent.op", "json", []byte(`{"k":"v"}`))
	if err != nil {
		t.Errorf("releaseShaper err=%v; want nil", err)
	}
	if got.Body != nil {
		t.Errorf("Body=%q; want nil (no profile registered)", got.Body)
	}
	if got.OutputProfile != "" {
		t.Errorf("OutputProfile=%q; want empty", got.OutputProfile)
	}
	if got.FieldMaskStatus != "" {
		t.Errorf("FieldMaskStatus=%q; want empty", got.FieldMaskStatus)
	}
}
