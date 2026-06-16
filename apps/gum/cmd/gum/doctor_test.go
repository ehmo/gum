package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/auth"
	"github.com/spf13/cobra"
)

// TestDoctorCache verifies the cache probe:
//   - XDG_CACHE_HOME wins for the cache root selection.
//   - A writable dir → OK summary mentions the resolved path.
//   - The probe file is removed after the writability check.
func TestDoctorCache(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	got := doctorCache("alpha")
	if !got.OK {
		t.Fatalf("doctorCache OK=false, summary=%q hint=%q", got.Summary, got.Hint)
	}
	dir := filepath.Join(tmp, "gum", "alpha")
	if !strings.Contains(got.Summary, dir) {
		t.Errorf("summary = %q, want resolved dir %q", got.Summary, dir)
	}
	// Probe file must have been cleaned up.
	if _, err := os.Stat(filepath.Join(dir, ".doctor-probe")); !os.IsNotExist(err) {
		t.Errorf("probe file not cleaned up: err = %v", err)
	}
}

// TestDoctorCache_NonWritable forces the cache dir to be unwritable by
// pointing it at an existing regular file path. MkdirAll then fails.
func TestDoctorCache_NonWritable(t *testing.T) {
	tmp := t.TempDir()
	// Create a regular file where the cache subtree would land — MkdirAll
	// can't create gum/alpha because tmp/file is a file, not a dir.
	blocker := filepath.Join(tmp, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv("XDG_CACHE_HOME", blocker)

	got := doctorCache("alpha")
	if got.OK {
		t.Errorf("doctorCache should fail when cache root is a file; got OK=true: %+v", got)
	}
	if got.Hint == "" {
		t.Errorf("expected a hint with the OS error")
	}
}

func TestDoctorAudit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	t.Run("clean_profile_ok", func(t *testing.T) {
		got := doctorAudit("alpha")
		if !got.OK {
			t.Fatalf("doctorAudit OK=false, summary=%q hint=%q", got.Summary, got.Hint)
		}
		dir := filepath.Join(tmp, "gum", "alpha")
		if !strings.Contains(got.Summary, dir) {
			t.Errorf("summary = %q, want resolved dir %q", got.Summary, dir)
		}
		if _, err := os.Stat(filepath.Join(dir, ".audit-doctor-probe")); !os.IsNotExist(err) {
			t.Errorf("probe file not cleaned up: err = %v", err)
		}
	})

	t.Run("broken_sentinel_fails", func(t *testing.T) {
		dir := filepath.Join(tmp, "gum", "broken")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "audit.broken"), []byte("rotate: permission denied\n"), 0o600); err != nil {
			t.Fatalf("write sentinel: %v", err)
		}
		got := doctorAudit("broken")
		if got.OK {
			t.Fatalf("doctorAudit with sentinel OK=true: %+v", got)
		}
		if got.Name != "audit" {
			t.Errorf("Name = %q, want audit", got.Name)
		}
		if !strings.Contains(got.Hint, "rotate: permission denied") {
			t.Errorf("Hint = %q, want sentinel reason", got.Hint)
		}
	})
}

// TestDoctorConfig covers the config-load probe. Missing config is OK
// (defaults apply); a malformed config fails with the parser message.
func TestDoctorConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	t.Run("missing_is_ok", func(t *testing.T) {
		got := doctorConfig("doesnotexist")
		if !got.OK {
			t.Errorf("doctorConfig missing profile: OK=false: %+v", got)
		}
	})

	t.Run("malformed_fails", func(t *testing.T) {
		dir := filepath.Join(tmp, "gum", "bad")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("setup: %v", err)
		}
		// Line without '=' is rejected by parser.
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("garbage line"), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
		got := doctorConfig("bad")
		if got.OK {
			t.Errorf("doctorConfig malformed: OK=true, want failure: %+v", got)
		}
		if got.Hint == "" {
			t.Errorf("expected hint with parser message")
		}
	})

	t.Run("valid_reports_key_count", func(t *testing.T) {
		dir := filepath.Join(tmp, "gum", "good")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.toml"),
			[]byte("notify.enabled = \"true\"\n"), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
		got := doctorConfig("good")
		if !got.OK {
			t.Fatalf("doctorConfig good: OK=false: %+v", got)
		}
		if !strings.Contains(got.Summary, "loaded") {
			t.Errorf("summary should mention key count, got %q", got.Summary)
		}
		// Audit fix: the summary must show the config PATH, not the Go
		// representation of the warnings slice (the second Load return value).
		if !strings.Contains(got.Summary, "config.toml") {
			t.Errorf("summary should contain the config path, got %q", got.Summary)
		}
		if strings.Contains(got.Summary, "[]") || strings.Contains(got.Summary, "Warning") {
			t.Errorf("summary leaked the warnings slice instead of the path, got %q", got.Summary)
		}
	})
}

// TestDoctorPlugin only verifies the probe completes without panic and
// returns a result with a non-empty Name. We don't assert OK because the
// plugin host's behavior depends on env state outside this test's control.
func TestDoctorPlugin(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	got := doctorPlugin()
	if got.Name != "plugin" {
		t.Errorf("Name = %q, want plugin", got.Name)
	}
	if got.Summary == "" {
		t.Errorf("Summary empty")
	}
}

// TestDoctorAuth verifies the probe returns the auth check with the expected
// shape. ADC presence depends on env; we only assert the result has Name=auth
// and the OK/Hint pair is consistent (OK=false → Hint non-empty).
func TestDoctorAuth(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	// Ensure ADC env vars do not leak from the host.
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())

	got := doctorAuth(cmd)
	if got.Name != "auth" {
		t.Errorf("Name = %q, want auth", got.Name)
	}
	if !got.OK && got.Hint == "" {
		t.Errorf("OK=false but Hint is empty: %+v", got)
	}
}

type blockingDoctorKeyring struct {
	release <-chan struct{}
}

func (b blockingDoctorKeyring) Get(string) (string, error) {
	<-b.release
	return "", nil
}
func (blockingDoctorKeyring) Set(string, string) error { return nil }
func (blockingDoctorKeyring) Delete(string) error      { return nil }

func TestDoctorAuth_KeyringTimeoutDoesNotHang(t *testing.T) {
	oldFactory := doctorKeyringFactory
	oldTimeout := doctorKeyringTimeout
	release := make(chan struct{})
	doctorKeyringFactory = func() auth.KeyringBackend { return blockingDoctorKeyring{release: release} }
	doctorKeyringTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		close(release)
		doctorKeyringFactory = oldFactory
		doctorKeyringTimeout = oldTimeout
	})

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GUM_API_KEY", "")
	t.Setenv("GUM_SERVICE_ACCOUNT_KEY", "")
	t.Setenv("HOME", t.TempDir())

	start := time.Now()
	got := doctorAuth(cmd)
	if time.Since(start) > time.Second {
		t.Fatalf("doctorAuth hung despite keyring timeout")
	}
	if got.Name != "auth" {
		t.Errorf("Name=%q, want auth", got.Name)
	}
	if got.OK {
		t.Fatalf("doctorAuth OK=true with blocked keyring and no env credentials: %+v", got)
	}
	if !strings.Contains(got.Hint, "OS keychain lookup timed out") {
		t.Errorf("Hint=%q, want keychain timeout", got.Hint)
	}
}

// TestRunDoctorChecks verifies the orchestrator aggregates exactly five
// checks (auth/audit/cache/plugin/config), reports OK only when all are OK, and
// surfaces the profile in the envelope.
func TestRunDoctorChecks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	rep := runDoctorChecks(cmd, "doctortest")
	if rep.Profile != "doctortest" {
		t.Errorf("Profile = %q, want doctortest", rep.Profile)
	}
	if len(rep.Checks) != 5 {
		t.Fatalf("len(Checks) = %d, want 5", len(rep.Checks))
	}
	wantNames := []string{"auth", "audit", "cache", "plugin", "config"}
	for i, want := range wantNames {
		if rep.Checks[i].Name != want {
			t.Errorf("Checks[%d].Name = %q, want %q", i, rep.Checks[i].Name, want)
		}
	}
	// Overall OK iff every individual check is OK.
	allOK := true
	for _, c := range rep.Checks {
		if !c.OK {
			allOK = false
		}
	}
	if rep.OK != allOK {
		t.Errorf("OK aggregation broken: rep.OK=%v allOK=%v checks=%+v", rep.OK, allOK, rep.Checks)
	}
}

// TestRenderDoctorText verifies the text renderer prints one line per check,
// surfaces hints when present, and ends with a pass/fail banner.
func TestRenderDoctorText(t *testing.T) {
	t.Run("all_ok", func(t *testing.T) {
		var buf bytes.Buffer
		renderDoctorText(&buf, doctorReport{
			OK:      true,
			Profile: "p",
			Checks: []doctorCheckResult{
				{Name: "auth", OK: true, Summary: "good"},
				{Name: "cache", OK: true, Summary: "writable"},
			},
		})
		out := buf.String()
		for _, want := range []string{"[ok] auth: good", "[ok] cache: writable", "all checks passed"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in output:\n%s", want, out)
			}
		}
	})

	t.Run("with_failures_and_hints", func(t *testing.T) {
		var buf bytes.Buffer
		renderDoctorText(&buf, doctorReport{
			OK:      false,
			Profile: "p",
			Checks: []doctorCheckResult{
				{Name: "auth", OK: false, Summary: "no ADC", Hint: "run gcloud login"},
			},
		})
		out := buf.String()
		for _, want := range []string{"[FAIL] auth: no ADC", "hint: run gcloud login", "FAILED"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in output:\n%s", want, out)
			}
		}
	})
}

// TestDoctorCache_HomeFallback exercises the $HOME fallback when
// XDG_CACHE_HOME is empty. The probe must land at $HOME/.cache/gum/<profile>.
func TestDoctorCache_HomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")

	got := doctorCache("homefallback")
	if !got.OK {
		t.Fatalf("doctorCache OK=false: %+v", got)
	}
	want := filepath.Join(home, ".cache", "gum", "homefallback")
	if !strings.Contains(got.Summary, want) {
		t.Errorf("summary = %q; want path %q", got.Summary, want)
	}
}

// TestDoctorCmd_TextUnhealthyReturnsError drives the text-format !OK
// branch of newDoctorCmd: when any subsystem fails the command must
// return errDoctorUnhealthy so cobra exits non-zero.
func TestDoctorCmd_TextUnhealthyReturnsError(t *testing.T) {
	// Force the cache check to fail by pointing XDG_CACHE_HOME at a file.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv("XDG_CACHE_HOME", blocker)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cmd := newDoctorCmd()
	cmd.SetArgs(nil) // default --format=text
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected errDoctorUnhealthy when a check fails")
	}
	if !strings.Contains(err.Error(), "unhealthy") {
		t.Errorf("err=%q; want 'unhealthy' marker", err)
	}
	if !strings.Contains(out.String(), "FAILED") {
		t.Errorf("text banner missing FAILED:\n%s", out.String())
	}
}

// TestDoctorCmd_JSONFormat exercises the cobra command end-to-end with
// --format=json and decodes the envelope to confirm the shape spec is met.
func TestDoctorCmd_JSONFormat(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cmd := newDoctorCmd()
	cmd.SetArgs([]string{"--format", "json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// Doctor may legitimately exit non-zero (ADC unconfigured in CI); we only
	// care that JSON renders.
	_ = cmd.Execute()

	var rep doctorReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("doctor JSON output not valid: %v\nout:\n%s", err, out.String())
	}
	if len(rep.Checks) != 5 {
		t.Errorf("JSON envelope has %d checks, want 5", len(rep.Checks))
	}
}

// TestDoctorCmd_JSONUnhealthyReturnsError is the audit regression: `gum doctor
// --format=json` MUST exit non-zero when a subsystem fails, matching the text
// path — otherwise a CI pre-flight `gum doctor --format=json && deploy` proceeds
// past a broken subsystem. Before the fix the JSON path returned only writeJSON's
// (nil) error and swallowed the health failure.
func TestDoctorCmd_JSONUnhealthyReturnsError(t *testing.T) {
	// Same forced failure as the text test: point XDG_CACHE_HOME at a file.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv("XDG_CACHE_HOME", blocker)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cmd := newDoctorCmd()
	cmd.SetArgs([]string{"--format=json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected errDoctorUnhealthy on --format=json when a check fails; got nil (exit 0)")
	}
	if !strings.Contains(err.Error(), "unhealthy") {
		t.Errorf("err=%q; want 'unhealthy' marker", err)
	}
	// The JSON envelope must still be written to stdout (ok:false), so a script
	// can both read the report AND see the non-zero exit.
	var rep map[string]any
	if jerr := json.Unmarshal(out.Bytes(), &rep); jerr != nil {
		t.Fatalf("stdout is not the JSON envelope: %v\n%s", jerr, out.String())
	}
	if ok, _ := rep["ok"].(bool); ok {
		t.Errorf("envelope ok=true; want false (a check failed)")
	}
}
