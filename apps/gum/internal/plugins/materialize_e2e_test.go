package plugins_test

// End-to-end §8.7 package installs, one per source class, all offline:
// pypi against an httptest index with a stub python3 through the real exec
// runner, github_release against an injected fetch, git against a real
// file:// repository, and local for the dev-untrusted inventory marking.
// Each install asserts the plugins.lock row: normalized argv, source, ref,
// checksum, and risk.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// writePackageManifest writes a manifest-only source dir whose [package]
// block is pkg. Remote sources carry no code in the manifest dir; the
// materializer fetches it.
func writePackageManifest(t *testing.T, pluginID, executable string, command []string, pkg map[string]any) string {
	t.Helper()

	src := t.TempDir()
	man := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    pluginID,
		"version":                 "1.2.0",
		"namespace_owner":         "acme",
		"shape":                   "mcp-plugin",
		"executable":              executable,
		"advertised_tools": []map[string]any{{
			"name":       "ping",
			"risk_class": "read",
		}},
	}
	if command != nil {
		man["command"] = command
	}
	if pkg != nil {
		man["package"] = pkg
	}
	b, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

func installPackagePlugin(t *testing.T, src string, dev bool, remote plugins.RemoteOptions) (string, *registry.Registry, error) {
	t.Helper()

	installRoot := t.TempDir()
	reg := registry.New(t.TempDir())
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
	_, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{
		Registry:  reg,
		Namespace: plugins.NamespaceOptions{ProfileIsDev: dev},
		Remote:    remote,
	})
	return installRoot, reg, err
}

func sha256HexOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestInstallPyPIPackage(t *testing.T) {
	artifact := []byte("fake sdist bytes")
	artifactHex := sha256HexOf(artifact)

	// A stub python3 that builds venv/bin/pip; the pip stub verifies the
	// offline flags and lands the console script. Both run through the
	// real exec runner with cwd = install dir.
	stubDir := t.TempDir()
	pipStub := `#!/bin/sh
set -e
[ "$1" = "install" ] || exit 64
[ "$2" = "--no-index" ] || exit 64
[ "$3" = "--no-deps" ] || exit 64
[ -f "$4" ] || exit 65
printf '#!/bin/sh\necho fli-runs\n' > venv/bin/fli
chmod 755 venv/bin/fli
`
	pythonStub := `#!/bin/sh
set -e
[ "$1" = "-m" ] || exit 64
[ "$2" = "venv" ] || exit 64
mkdir -p "$3/bin"
cat > "$3/bin/pip" <<'PIPEOF'
` + pipStub + `PIPEOF
chmod 755 "$3/bin/pip"
`
	pythonPath := filepath.Join(stubDir, "python3")
	if err := os.WriteFile(pythonPath, []byte(pythonStub), 0o755); err != nil {
		t.Fatal(err)
	}

	newIndex := func(t *testing.T, digestHex string, artifactBody []byte) *httptest.Server {
		t.Helper()
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/pypi/fli/1.2.0/json":
				meta := map[string]any{"urls": []map[string]any{
					{
						"filename": "fli-1.2.0.tar.gz",
						"url":      srv.URL + "/artifact/fli-1.2.0.tar.gz",
						"digests":  map[string]string{"sha256": digestHex},
					},
				}}
				_ = json.NewEncoder(w).Encode(meta)
			case "/artifact/fli-1.2.0.tar.gz":
				_, _ = w.Write(artifactBody)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)
		return srv
	}

	pkg := map[string]any{
		"source":   "pypi",
		"ref":      "fli==1.2.0",
		"checksum": "sha256:" + artifactHex,
	}

	t.Run("uvx command binds to the venv console script", func(t *testing.T) {
		srv := newIndex(t, artifactHex, artifact)
		src := writePackageManifest(t, "fli", "venv/bin/fli", []string{"uvx", "fli", "mcp"}, pkg)
		installRoot, reg, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{
			PythonPath:  pythonPath,
			PyPIBaseURL: srv.URL,
		})
		if err != nil {
			t.Fatalf("install: %v", err)
		}

		installDir := filepath.Join(installRoot, "fli")
		row := lockRow(t, reg, "fli")
		assertArgv(t, row, []string{filepath.Join(installDir, "venv", "bin", "fli"), "mcp"})
		if got := row["source"]; got != "pypi" {
			t.Errorf("source = %v; want pypi", got)
		}
		if got := row["ref"]; got != "fli==1.2.0" {
			t.Errorf("ref = %v; want fli==1.2.0", got)
		}
		if got := row["checksum"]; got != "sha256:"+artifactHex {
			t.Errorf("checksum = %v; want the manifest pin", got)
		}
		if _, marked := row["risk"]; marked {
			t.Errorf("risk = %v; a pinned pypi install carries no marking", row["risk"])
		}

		// The runtime executable is the venv console script, verified and
		// digest-bound like any other plugin executable.
		script, err := os.ReadFile(filepath.Join(installDir, "venv", "bin", "fli"))
		if err != nil {
			t.Fatalf("console script missing: %v", err)
		}
		if got := row["executable_sha256"]; got != sha256HexOf(script) {
			t.Errorf("executable_sha256 = %v; want the console script digest", got)
		}
	})

	t.Run("served bytes that differ from the pin are refused", func(t *testing.T) {
		// Index metadata claims the pinned digest, but the download serves
		// different bytes: the post-download hash catches the swap.
		srv := newIndex(t, artifactHex, []byte("swapped bytes"))
		src := writePackageManifest(t, "fli", "venv/bin/fli", []string{"uvx", "fli", "mcp"}, pkg)
		_, reg, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{
			PythonPath:  pythonPath,
			PyPIBaseURL: srv.URL,
		})
		if !errors.Is(err, plugins.ErrArtifactChecksumMismatch) {
			t.Fatalf("install err = %v; want PLUGIN_ARTIFACT_CHECKSUM_MISMATCH", err)
		}
		assertNoLockRows(t, reg)
	})

	t.Run("an index with no matching artifact is refused", func(t *testing.T) {
		srv := newIndex(t, strings.Repeat("0", 64), artifact)
		src := writePackageManifest(t, "fli", "venv/bin/fli", []string{"uvx", "fli", "mcp"}, pkg)
		_, _, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{
			PythonPath:  pythonPath,
			PyPIBaseURL: srv.URL,
		})
		if !errors.Is(err, plugins.ErrArtifactChecksumMismatch) {
			t.Fatalf("install err = %v; want PLUGIN_ARTIFACT_CHECKSUM_MISMATCH", err)
		}
	})

	t.Run("a console script the package never produced is refused", func(t *testing.T) {
		// pip lands venv/bin/fli; the manifest declares venv/bin/other, so
		// the declared-executable check fails before any registry write.
		srv := newIndex(t, artifactHex, artifact)
		src := writePackageManifest(t, "fli", "venv/bin/other", []string{"uvx", "other", "mcp"}, pkg)
		_, reg, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{
			PythonPath:  pythonPath,
			PyPIBaseURL: srv.URL,
		})
		if !errors.Is(err, plugins.ErrExecutableUntrusted) {
			t.Fatalf("install err = %v; want PLUGIN_EXECUTABLE_UNTRUSTED", err)
		}
		assertNoLockRows(t, reg)
	})
}

// buildReleaseTarGz assembles the release artifact: bin/mcp plus a README.
func buildReleaseTarGz(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := []struct {
		name string
		body string
	}{
		{"bin/mcp", "#!/bin/sh\necho release-mcp\n"},
		{"README.md", "release docs"},
	}
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o777, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallGitHubReleasePackage(t *testing.T) {
	artifact := buildReleaseTarGz(t)
	artifactHex := sha256HexOf(artifact)
	const refURL = "https://github.com/acme/fli/releases/download/v1.2.0/fli.tar.gz"

	fetchFrom := func(served []byte) func(context.Context, string) (io.ReadCloser, error) {
		return func(_ context.Context, url string) (io.ReadCloser, error) {
			if url != refURL {
				return nil, fmt.Errorf("unexpected fetch %q", url)
			}
			return io.NopCloser(bytes.NewReader(served)), nil
		}
	}

	pkg := map[string]any{
		"source":   "github_release",
		"ref":      refURL,
		"checksum": "sha256:" + artifactHex,
	}

	t.Run("verified artifact unpacks and binds", func(t *testing.T) {
		src := writePackageManifest(t, "fli", "bin/mcp", []string{"bin/mcp", "serve"}, pkg)
		installRoot, reg, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{Fetch: fetchFrom(artifact)})
		if err != nil {
			t.Fatalf("install: %v", err)
		}

		installDir := filepath.Join(installRoot, "fli")
		row := lockRow(t, reg, "fli")
		assertArgv(t, row, []string{filepath.Join(installDir, "bin", "mcp"), "serve"})
		if got := row["source"]; got != "github_release" {
			t.Errorf("source = %v; want github_release", got)
		}
		if got := row["ref"]; got != refURL {
			t.Errorf("ref = %v; want the artifact URL", got)
		}
		if _, marked := row["risk"]; marked {
			t.Errorf("risk = %v; a checksummed release carries no marking", row["risk"])
		}

		info, err := os.Stat(filepath.Join(installDir, "bin", "mcp"))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o755 {
			t.Errorf("executable mode = %o; want 0755", got)
		}
	})

	t.Run("tampered artifact is refused before any registry write", func(t *testing.T) {
		tampered := append(append([]byte{}, artifact...), 0x00)
		src := writePackageManifest(t, "fli", "bin/mcp", []string{"bin/mcp", "serve"}, pkg)
		installRoot, reg, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{Fetch: fetchFrom(tampered)})
		if !errors.Is(err, plugins.ErrArtifactChecksumMismatch) {
			t.Fatalf("install err = %v; want PLUGIN_ARTIFACT_CHECKSUM_MISMATCH", err)
		}
		assertNoLockRows(t, reg)
		// Nothing unpacked: the artifact never verified.
		if _, statErr := os.Stat(filepath.Join(installRoot, "fli", "bin", "mcp")); !os.IsNotExist(statErr) {
			t.Errorf("tampered artifact left an executable (stat err = %v)", statErr)
		}
	})

	t.Run("zip artifact unpacks through the zip path", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, err := zw.Create("bin/mcp")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("#!/bin/sh\necho zip-mcp\n")); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		zipBytes := buf.Bytes()
		zipURL := strings.TrimSuffix(refURL, ".tar.gz") + ".zip"

		zpkg := map[string]any{
			"source":   "github_release",
			"ref":      zipURL,
			"checksum": "sha256:" + sha256HexOf(zipBytes),
		}
		src := writePackageManifest(t, "fli", "bin/mcp", nil, zpkg)
		installRoot, _, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{
			Fetch: func(_ context.Context, url string) (io.ReadCloser, error) {
				if url != zipURL {
					return nil, fmt.Errorf("unexpected fetch %q", url)
				}
				return io.NopCloser(bytes.NewReader(zipBytes)), nil
			},
		})
		if err != nil {
			t.Fatalf("install: %v", err)
		}
		if _, err := os.Stat(filepath.Join(installRoot, "fli", "bin", "mcp")); err != nil {
			t.Errorf("zip executable missing: %v", err)
		}
	})
}

// initGitFixture builds a real repository with one committed plugin tree
// and returns its file:// URL and HEAD commit.
func initGitFixture(t *testing.T) (string, string) {
	t.Helper()

	repo := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	run("init", "--quiet", "--initial-branch=main")
	if err := os.MkdirAll(filepath.Join(repo, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "bin", "mcp"), []byte("#!/bin/sh\necho git-mcp\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "plugin tree")
	head := run("rev-parse", "HEAD")
	return "file://" + repo, head
}

func TestInstallGitPackage(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repoURL, head := initGitFixture(t)

	t.Run("pinned clone verifies and binds", func(t *testing.T) {
		pkg := map[string]any{"source": "git", "ref": repoURL + "@" + head}
		src := writePackageManifest(t, "gitp", "bin/mcp", []string{"bin/mcp", "serve"}, pkg)
		installRoot, reg, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{})
		if err != nil {
			t.Fatalf("install: %v", err)
		}

		installDir := filepath.Join(installRoot, "gitp")
		row := lockRow(t, reg, "gitp")
		assertArgv(t, row, []string{filepath.Join(installDir, "bin", "mcp"), "serve"})
		if got := row["source"]; got != "git" {
			t.Errorf("source = %v; want git", got)
		}
		if _, marked := row["risk"]; marked {
			t.Errorf("risk = %v; a pinned clone carries no marking", row["risk"])
		}
		// The clone's .git never lands in the install root.
		if _, statErr := os.Stat(filepath.Join(installDir, ".git")); !os.IsNotExist(statErr) {
			t.Errorf(".git landed in the install root (stat err = %v)", statErr)
		}
	})

	t.Run("unpinned ref is refused outside dev", func(t *testing.T) {
		pkg := map[string]any{"source": "git", "ref": repoURL}
		src := writePackageManifest(t, "gitp", "bin/mcp", nil, pkg)
		_, reg, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{})
		if !errors.Is(err, plugins.ErrPackageSourceUntrusted) {
			t.Fatalf("install err = %v; want PLUGIN_PACKAGE_SOURCE_UNTRUSTED", err)
		}
		assertNoLockRows(t, reg)
	})

	t.Run("dev profile clones unpinned and is marked dev-untrusted", func(t *testing.T) {
		pkg := map[string]any{"source": "git", "ref": repoURL}
		src := writePackageManifest(t, "gitp", "bin/mcp", nil, pkg)
		_, reg, err := installPackagePlugin(t, src, true, plugins.RemoteOptions{})
		if err != nil {
			t.Fatalf("dev install: %v", err)
		}
		if got := lockRow(t, reg, "gitp")["risk"]; got != "dev-untrusted" {
			t.Errorf("risk = %v; want dev-untrusted", got)
		}
	})

	t.Run("a pin the repository cannot resolve is refused", func(t *testing.T) {
		pkg := map[string]any{"source": "git", "ref": repoURL + "@" + strings.Repeat("d", 40)}
		src := writePackageManifest(t, "gitp", "bin/mcp", nil, pkg)
		_, reg, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{})
		if err == nil {
			t.Fatal("install accepted a nonexistent commit pin")
		}
		assertNoLockRows(t, reg)
	})
}

func TestLocalInstallMarkedDevUntrusted(t *testing.T) {
	// The pre-existing manifest shape: no [package] block, code in the
	// manifest dir. §8.7 marks it dev-untrusted in the lock row, which is
	// where the §13 inventory reads risk from.
	src := writeCommandPlugin(t, "localp", []string{"bin/mcp", "serve"})
	installRoot := t.TempDir()
	reg := registry.New(t.TempDir())
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
	if _, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{Registry: reg}); err != nil {
		t.Fatalf("install: %v", err)
	}

	row := lockRow(t, reg, "localp")
	if got := row["source"]; got != "local" {
		t.Errorf("source = %v; want local", got)
	}
	if got := row["risk"]; got != "dev-untrusted" {
		t.Errorf("risk = %v; want dev-untrusted", got)
	}
}

func TestLoadManifestPackageValidation(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, pkg map[string]any) string {
		t.Helper()
		return writePackageManifest(t, "fli", "bin/mcp", nil, pkg)
	}

	if _, err := plugins.LoadManifest(write(t, map[string]any{
		"source": "pypi", "ref": "fli==1.2.0",
		"checksum": "sha256:" + strings.Repeat("a", 64),
	})); err != nil {
		t.Errorf("valid pypi block refused: %v", err)
	}

	invalid := []map[string]any{
		{"source": "npm"},
		{"source": "pypi", "ref": "fli>=1.0", "checksum": "sha256:" + strings.Repeat("a", 64)},
		{"source": "pypi", "ref": "fli==1.2.0"},
		{"source": "github_release", "ref": "http://x/y.tar.gz", "checksum": "sha256:" + strings.Repeat("a", 64)},
		{"source": "git", "ref": "ssh://host/repo.git"},
	}
	for _, pkg := range invalid {
		if _, err := plugins.LoadManifest(write(t, pkg)); !errors.Is(err, plugins.ErrManifestInvalid) {
			t.Errorf("LoadManifest(package=%v) err = %v; want ErrManifestInvalid", pkg, err)
		}
	}
}

// assertNoLockRows fails if a refused install left registry rows behind.
func assertNoLockRows(t *testing.T, reg *registry.Registry) {
	t.Helper()
	files, err := reg.Load()
	if err != nil {
		t.Fatalf("registry load: %v", err)
	}
	if len(files.Lock.Plugins) != 0 {
		t.Errorf("refused install wrote %d lock rows; want 0", len(files.Lock.Plugins))
	}
}

// The Lstat gate: an artifact that verifies but never delivers the declared
// executable must fail closed before any hash or registry write.
func TestInstallExecutableAbsentFromArtifact(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("docs only")
	if err := tw.WriteHeader(&tar.Header{Name: "README", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	artifact := buf.Bytes()

	const refURL = "https://github.com/acme/fli/releases/download/v1.2.0/docs.tar.gz"
	pkg := map[string]any{
		"source":   "github_release",
		"ref":      refURL,
		"checksum": "sha256:" + sha256HexOf(artifact),
	}
	fetch := func(_ context.Context, url string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(artifact)), nil
	}

	src := writePackageManifest(t, "fli", "bin/mcp", []string{"bin/mcp"}, pkg)
	_, reg, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{Fetch: fetch})
	if !errors.Is(err, plugins.ErrExecutableUntrusted) {
		t.Fatalf("install err = %v; want PLUGIN_EXECUTABLE_UNTRUSTED", err)
	}
	assertNoLockRows(t, reg)
}

// A symlink in the curated manifest tree fails the post-materialize copy.
func TestInstallManifestTreeSymlinkRefused(t *testing.T) {
	src := writePackageManifest(t, "fli", "bin/mcp", []string{"bin/mcp"}, map[string]any{"source": "local"})
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}

	_, reg, err := installPackagePlugin(t, src, false, plugins.RemoteOptions{})
	if !errors.Is(err, plugins.ErrExecutableUntrusted) {
		t.Fatalf("install err = %v; want PLUGIN_EXECUTABLE_UNTRUSTED", err)
	}
	assertNoLockRows(t, reg)
}
