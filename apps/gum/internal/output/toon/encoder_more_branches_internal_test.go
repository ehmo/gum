package toon

import (
	"testing"
)

// TestHomogeneousKeysEmptyArrayReturnsNil pins homogeneousKeys's
// `len(arr) == 0 → return nil` arm (encoder.go:253-255). The header-
// CSV optimization downstream depends on a non-nil key set; an empty
// array has nothing to homogenize, so the function MUST signal that
// via a nil return rather than emit `{}` or panic.
func TestHomogeneousKeysEmptyArrayReturnsNil(t *testing.T) {
	t.Parallel()
	if got := homogeneousKeys([]any{}); got != nil {
		t.Errorf("homogeneousKeys([])=%v; want nil", got)
	}
}

// TestHomogeneousKeysMismatchedKeyCountReturnsNil pins
// homogeneousKeys's `len(m) != len(keys) → return nil` arm
// (encoder.go:279-281). When element[1+] has a different number of
// keys than element[0], the array is NOT homogeneous and the headers-
// CSV optimization must NOT fire — otherwise we'd silently drop or
// duplicate cells.
func TestHomogeneousKeysMismatchedKeyCountReturnsNil(t *testing.T) {
	t.Parallel()
	arr := []any{
		map[string]any{"a": 1, "b": 2},
		map[string]any{"a": 1}, // missing "b"
	}
	if got := homogeneousKeys(arr); got != nil {
		t.Errorf("homogeneousKeys(mismatched len)=%v; want nil", got)
	}
}

// TestHomogeneousKeysMismatchedKeyNameReturnsNil pins the symmetric
// `!keySet[k] → return nil` arm (encoder.go:282-286). Same len, but
// the keys differ — also non-homogeneous, also a nil return.
func TestHomogeneousKeysMismatchedKeyNameReturnsNil(t *testing.T) {
	t.Parallel()
	arr := []any{
		map[string]any{"a": 1, "b": 2},
		map[string]any{"a": 1, "c": 3}, // "c" not in first element
	}
	if got := homogeneousKeys(arr); got != nil {
		t.Errorf("homogeneousKeys(mismatched key)=%v; want nil", got)
	}
}

// TestHomogeneousKeysNonMapElementReturnsNil pins the
// `!ok → return nil` arm (encoder.go:276-278) for the loop over
// arr[1:]. arr[0] is a map but arr[1] is a scalar; homogeneousKeys
// MUST refuse the headers-CSV form rather than crash on the type
// assertion.
func TestHomogeneousKeysNonMapElementReturnsNil(t *testing.T) {
	t.Parallel()
	arr := []any{
		map[string]any{"a": 1},
		"not-a-map",
	}
	if got := homogeneousKeys(arr); got != nil {
		t.Errorf("homogeneousKeys(scalar second elem)=%v; want nil", got)
	}
}
