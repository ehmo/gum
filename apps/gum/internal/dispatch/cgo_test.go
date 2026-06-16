package dispatch_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestNoCGoInDispatch ensures the internal/dispatch closure has no CGo
// dependency under release conditions (CGO_ENABLED=0, matching spec §14).
// Without the explicit env pin, stdlib packages like net surface CgoFiles
// on Linux hosts that ship with CGO_ENABLED=1 by default, even though the
// release binary never compiles those files.
func TestNoCGoInDispatch(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-json", "github.com/ehmo/gum/internal/dispatch")
	cmd.Env = append(cmd.Environ(), "CGO_ENABLED=0")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list failed: %v\nstdout: %s", err, out)
	}

	// go list -deps -json emits a stream of JSON objects, not an array.
	dec := json.NewDecoder(strings.NewReader(string(out)))
	type pkg struct {
		ImportPath string   `json:"ImportPath"`
		CgoFiles   []string `json:"CgoFiles"`
	}
	var offenders []string
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decode package JSON: %v", err)
		}
		if len(p.CgoFiles) > 0 {
			offenders = append(offenders, p.ImportPath)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("internal/dispatch closure contains CGo package(s): %s", strings.Join(offenders, ", "))
	}
}
