package plugins_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins"
)

// TestTransferNamespaceNilLockRejected pins mutateNamespaceRow's
// `lock == nil → ErrPrefixNotInLock` arm (namespace_transfer.go:63-65).
// A nil lock would otherwise panic when range-ing — the guard MUST
// surface a typed error.
func TestTransferNamespaceNilLockRejected(t *testing.T) {
	t.Parallel()
	_, err := plugins.TransferNamespace(nil, "fli", "io.example.new", fixedClockOpts())
	if !errors.Is(err, plugins.ErrPrefixNotInLock) {
		t.Errorf("err=%v; want ErrPrefixNotInLock wrap", err)
	}
}

// TestTransferNamespaceEmptyPrefixRejected pins mutateNamespaceRow's
// `prefix == "" → ErrPrefixNotInLock` arm (namespace_transfer.go:66-68).
func TestTransferNamespaceEmptyPrefixRejected(t *testing.T) {
	t.Parallel()
	lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
	_, err := plugins.TransferNamespace(lock, "", "io.example.new", fixedClockOpts())
	if !errors.Is(err, plugins.ErrPrefixNotInLock) {
		t.Errorf("err=%v; want ErrPrefixNotInLock wrap", err)
	}
}

// TestTransferNamespaceSkipsNonMapAndWrongPrefixRows pins the inner
// loop's two `continue` arms (namespace_transfer.go:71-72 non-map and
// 75-76 wrong-prefix). Reached by a Plugins slice containing a raw
// non-map (typed string) entry plus a map with a different prefix
// before the target row. The function MUST skip both and still
// successfully transfer the target.
func TestTransferNamespaceSkipsNonMapAndWrongPrefixRows(t *testing.T) {
	t.Parallel()
	lock := &catalog.PluginsLock{
		PluginsLockSchemaVersion: 1,
		Plugins: []any{
			"not-a-map", // exercises 71-72
			map[string]any{
				"prefix":          "other",
				"namespace_owner": "io.example.other",
			}, // exercises 75-76 (wrong prefix)
			map[string]any{
				"prefix":          "fli",
				"namespace_owner": "io.example.flights",
			}, // target
		},
	}
	from, err := plugins.TransferNamespace(lock, "fli", "io.example.new", fixedClockOpts())
	if err != nil {
		t.Fatalf("TransferNamespace: %v", err)
	}
	if from != "io.example.flights" {
		t.Errorf("from=%q; want io.example.flights", from)
	}
}
