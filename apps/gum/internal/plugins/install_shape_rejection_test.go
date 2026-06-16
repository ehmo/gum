package plugins_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/plugins"
)

// TestThirdPartyShape2InstallRejected is the bead-named acceptance test for
// spec §8.1: third-party plugin manifests that declare a Shape 2 transport
// (e.g. shape="grpc-subprocess") MUST be rejected with PLUGIN_SHAPE_UNSUPPORTED
// before any registry file is written. This guards against a future Shape 2
// implementation accidentally allowing v0.1.0 catalogs to record half-installed
// plugins that the runtime cannot dispatch.
//
// The test exercises the full install path (Host.Install), not just
// LoadManifest, so it pins the contract on the user-facing entry point.
func TestThirdPartyShape2InstallRejected(t *testing.T) {
	defer goleak.VerifyNone(t)

	installRoot := t.TempDir()
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})

	src := filepath.Join(testdataDir(), "invalid-shape")
	_, err := host.Install(t.Context(), src)
	if !errors.Is(err, plugins.ErrUnsupportedShape) {
		t.Fatalf("Install err = %v; want PLUGIN_SHAPE_UNSUPPORTED (%v)", err, plugins.ErrUnsupportedShape)
	}
	if err.Error() != "PLUGIN_SHAPE_UNSUPPORTED" {
		t.Errorf("err message = %q; want exactly PLUGIN_SHAPE_UNSUPPORTED for stable sentinel surfacing", err.Error())
	}

	// The plugin directory MUST NOT exist after a rejected install: the
	// Shape-2 rejection happens before the filesystem copy, so the install
	// root stays empty.
	entries, err := os.ReadDir(installRoot)
	if err != nil {
		t.Fatalf("ReadDir installRoot: %v", err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("installRoot contains %d entries after rejected install: %v; want empty", len(entries), names)
	}

	// Registry sentinel files must be absent at the install root.
	for _, name := range []string{"plugin-catalog.json", "plugins.lock", "plugin-state.json"} {
		if _, err := os.Stat(filepath.Join(installRoot, name)); !os.IsNotExist(err) {
			t.Errorf("%s exists at install root after rejected install: err=%v; want IsNotExist", name, err)
		}
	}

	// Sanity check: a subsequent List call must not surface the rejected plugin.
	manifests, err := host.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(manifests) != 0 {
		t.Errorf("List returned %d manifests after rejected install; want 0", len(manifests))
	}
}
