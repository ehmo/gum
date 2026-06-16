package plugins_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// TestValidateCredentialDescriptorsEmptyEnvRejected pins the
// `d.Env == "" → ErrCredentialDescriptorInvalid wrap` arm
// (credentials.go:78-80). An empty Env makes the descriptor
// unkeyable so the validator MUST reject it before alias checks.
func TestValidateCredentialDescriptorsEmptyEnvRejected(t *testing.T) {
	t.Parallel()
	descs := []plugins.CredentialDescriptor{
		{Alias: "api", Env: "", Kind: "api_key", DisplayName: "API"},
	}
	err := plugins.ValidateCredentialDescriptors([]string{"GUM_X"}, descs)
	if !errors.Is(err, plugins.ErrCredentialDescriptorInvalid) {
		t.Errorf("err=%v; want ErrCredentialDescriptorInvalid wrap", err)
	}
}

// TestValidateCredentialDescriptorsDuplicateEnvRejected pins the
// `dup → "duplicate descriptor for env entry"` arm (credentials.go:90-92).
// Two descriptors sharing the same Env break the env→descriptor map's
// 1:1 invariant.
func TestValidateCredentialDescriptorsDuplicateEnvRejected(t *testing.T) {
	t.Parallel()
	descs := []plugins.CredentialDescriptor{
		{Alias: "a1", Env: "GUM_X", Kind: "api_key", DisplayName: "A1"},
		{Alias: "a2", Env: "GUM_X", Kind: "api_key", DisplayName: "A2"},
	}
	err := plugins.ValidateCredentialDescriptors([]string{"GUM_X"}, descs)
	if !errors.Is(err, plugins.ErrCredentialDescriptorInvalid) {
		t.Errorf("err=%v; want ErrCredentialDescriptorInvalid wrap", err)
	}
}

// TestValidateCredentialDescriptorsEmptyDisplayNameRejected pins the
// `d.DisplayName == "" → wrap` arm (credentials.go:107-109).
func TestValidateCredentialDescriptorsEmptyDisplayNameRejected(t *testing.T) {
	t.Parallel()
	descs := []plugins.CredentialDescriptor{
		{Alias: "api", Env: "GUM_X", Kind: "api_key", DisplayName: ""},
	}
	err := plugins.ValidateCredentialDescriptors([]string{"GUM_X"}, descs)
	if !errors.Is(err, plugins.ErrCredentialDescriptorInvalid) {
		t.Errorf("err=%v; want ErrCredentialDescriptorInvalid wrap", err)
	}
}

// TestSafeDescriptorMapsEmptyReturnsNil pins SafeDescriptorMaps's
// `len(descs) == 0 → return nil` arm (credentials.go:143-145). The
// nil-vs-empty-slice distinction matters for JSON output (key omitted
// vs. key with []).
func TestSafeDescriptorMapsEmptyReturnsNil(t *testing.T) {
	t.Parallel()
	if got := plugins.SafeDescriptorMaps(nil); got != nil {
		t.Errorf("got=%v; want nil", got)
	}
}
