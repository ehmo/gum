package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLiveFlightsSearchViaFli pins gum-ikg acceptance line 3 ("flights.search
// dispatches via subprocess") against a real fli executable. Skipped unless
// GUM_LIVE_FLIGHTS=1 and a fli-backed bin/flights-mcp is present in PATH or
// at the path named by GUM_FLIGHTS_MCP_BIN — both gates protect CI machines
// that don't have Python/uvx + the `fli` package installed and prevent
// flights.google.com from blocking deterministic test runs from datacenter
// IPs (see spec §1692 canary failure handling).
//
// The test:
//  1. Builds gum from source via `go run ./cmd/gum`.
//  2. Resolves a temp profile dir so install/run mutations don't leak into
//     the operator's real ~/.local/share/gum.
//  3. Installs the bundled apps/gum/plugins/google-flights manifest pointing
//     at the discovered fli executable.
//  4. Invokes `gum plugin run google-flights flights_search '{...}'` for a
//     single-leg SFO→LAX query 4 weeks out (matching spec §1664 canary
//     guidance: low-volume synthetic request for a well-known route).
//  5. Asserts the response envelope's `data.itineraries` array is non-empty.
//
// Failure modes that surface as t.Skip rather than t.Fatal:
//   - GUM_LIVE_FLIGHTS unset (the default for `go test ./...`).
//   - flights-mcp executable not discoverable (operator hasn't run `uvx
//     install fli` or set GUM_FLIGHTS_MCP_BIN).
//
// Failure modes that surface as t.Fatal:
//   - gum binary fails to build or exec.
//   - plugin install/run returns a non-zero exit with a real error envelope.
//   - response envelope decodes but itineraries is empty (could indicate
//     google.com is blocking the test source IP — re-run from a residential
//     IP or accept the soft canary failure per §1692).
func TestLiveFlightsSearchViaFli(t *testing.T) {
	if os.Getenv("GUM_LIVE_FLIGHTS") != "1" {
		t.Skip("set GUM_LIVE_FLIGHTS=1 to enable; this test makes a live request to flights.google.com")
	}
	bin := os.Getenv("GUM_FLIGHTS_MCP_BIN")
	if bin == "" {
		var err error
		bin, err = exec.LookPath("flights-mcp")
		if err != nil {
			t.Skip("flights-mcp not on PATH; install fli (uvx install fli) or set GUM_FLIGHTS_MCP_BIN")
		}
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("GUM_FLIGHTS_MCP_BIN=%q does not exist: %v", bin, err)
	}

	profileDir := t.TempDir()
	t.Setenv("GUM_PROFILE_DIR", profileDir)

	manifestDir := filepath.Join(t.TempDir(), "google-flights")
	if err := os.MkdirAll(filepath.Join(manifestDir, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir manifest tree: %v", err)
	}
	if err := os.Symlink(bin, filepath.Join(manifestDir, "bin", "flights-mcp")); err != nil {
		t.Fatalf("symlink bin/flights-mcp -> %s: %v", bin, err)
	}
	manifestJSON, err := os.ReadFile("../plugins/google-flights/manifest.json")
	if err != nil {
		t.Fatalf("read bundled manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "manifest.json"), manifestJSON, 0o644); err != nil {
		t.Fatalf("write manifest copy: %v", err)
	}

	install := exec.Command("go", "run", "./cmd/gum", "plugin", "install", manifestDir, "--yes")
	install.Dir = ".."
	install.Env = append(os.Environ(), "GUM_PROFILE_DIR="+profileDir)
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("plugin install failed: %v\n%s", err, out)
	}

	argsJSON := `{"origin":"SFO","destination":"LAX","departure_date":"2026-06-15","passengers":1}`
	run := exec.Command("go", "run", "./cmd/gum", "plugin", "run", "google-flights", "flights_search", argsJSON)
	run.Dir = ".."
	run.Env = append(os.Environ(), "GUM_PROFILE_DIR="+profileDir)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin run flights_search failed: %v\n%s", err, out)
	}

	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Itineraries []json.RawMessage `json:"itineraries"`
		} `json:"data"`
		ErrorCode string `json:"error_code"`
		Error     string `json:"error"`
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	dec.DisallowUnknownFields()
	for {
		if err := dec.Decode(&env); err != nil {
			break
		}
		if env.Success || len(env.Data.Itineraries) > 0 {
			break
		}
	}
	if !env.Success {
		t.Fatalf("flights_search envelope reported failure: code=%s error=%s\nfull output:\n%s",
			env.ErrorCode, env.Error, out)
	}
	if len(env.Data.Itineraries) == 0 {
		t.Fatalf("itineraries array is empty (datacenter IP may be blocked by Google; per spec §1692 this is a soft canary failure)\nfull output:\n%s", out)
	}
}
