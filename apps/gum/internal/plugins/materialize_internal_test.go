package plugins

// Unit coverage for the §8.7 materializer primitives: archive unpack
// hardening (traversal, links, mode pinning) and the default fetch/exec
// edges RemoteOptions falls back to.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildTarGz assembles a gzip'd tarball from entries. A nil body means a
// directory; a non-nil linkname means a symlink entry.
func buildTarGz(t *testing.T, entries []tarEntry) string {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o777}
		switch {
		case e.linkname != "":
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = e.linkname
		case e.body == nil:
			hdr.Typeflag = tar.TypeDir
		default:
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type tarEntry struct {
	name     string
	body     []byte
	linkname string
}

func TestUnpackTarGzHardening(t *testing.T) {
	t.Parallel()

	t.Run("pins modes and lands only declared executable as 0755", func(t *testing.T) {
		artifact := buildTarGz(t, []tarEntry{
			{name: "bin", body: nil},
			{name: "bin/mcp", body: []byte("#!/bin/sh\n")},
			{name: "README.md", body: []byte("docs")},
		})
		dest := t.TempDir()
		if err := unpackTarGz(artifact, dest, "bin/mcp"); err != nil {
			t.Fatalf("unpackTarGz: %v", err)
		}

		execInfo, err := os.Stat(filepath.Join(dest, "bin", "mcp"))
		if err != nil {
			t.Fatal(err)
		}
		if got := execInfo.Mode().Perm(); got != 0o755 {
			t.Errorf("executable mode = %o; want 0755", got)
		}
		readmeInfo, err := os.Stat(filepath.Join(dest, "README.md"))
		if err != nil {
			t.Fatal(err)
		}
		// The tar header claimed 0777; the pin strips it to 0644.
		if got := readmeInfo.Mode().Perm(); got != 0o644 {
			t.Errorf("README mode = %o; want 0644", got)
		}
	})

	t.Run("refuses traversal", func(t *testing.T) {
		artifact := buildTarGz(t, []tarEntry{{name: "../evil", body: []byte("x")}})
		err := unpackTarGz(artifact, t.TempDir(), "bin/mcp")
		if !errors.Is(err, ErrPackageSourceUntrusted) {
			t.Fatalf("err = %v; want PLUGIN_PACKAGE_SOURCE_UNTRUSTED", err)
		}
	})

	t.Run("refuses absolute entry", func(t *testing.T) {
		artifact := buildTarGz(t, []tarEntry{{name: "/etc/evil", body: []byte("x")}})
		err := unpackTarGz(artifact, t.TempDir(), "bin/mcp")
		if !errors.Is(err, ErrPackageSourceUntrusted) {
			t.Fatalf("err = %v; want PLUGIN_PACKAGE_SOURCE_UNTRUSTED", err)
		}
	})

	t.Run("refuses symlink entry", func(t *testing.T) {
		artifact := buildTarGz(t, []tarEntry{{name: "link", linkname: "/etc/passwd"}})
		err := unpackTarGz(artifact, t.TempDir(), "bin/mcp")
		if !errors.Is(err, ErrExecutableUntrusted) {
			t.Fatalf("err = %v; want PLUGIN_EXECUTABLE_UNTRUSTED", err)
		}
	})

	t.Run("refuses a non-gzip artifact", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.tar.gz")
		if err := os.WriteFile(path, []byte("not gzip"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := unpackTarGz(path, t.TempDir(), "bin/mcp"); err == nil {
			t.Fatal("unpackTarGz accepted a non-gzip artifact")
		}
	})
}

func TestUnpackZipHardening(t *testing.T) {
	t.Parallel()

	build := func(t *testing.T, add func(*zip.Writer)) string {
		t.Helper()
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		add(zw)
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "artifact.zip")
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("lands regular files with pinned modes", func(t *testing.T) {
		artifact := build(t, func(zw *zip.Writer) {
			if _, err := zw.Create("bin/"); err != nil {
				t.Fatal(err)
			}
			w, err := zw.Create("bin/mcp")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte("#!/bin/sh\n")); err != nil {
				t.Fatal(err)
			}
		})
		dest := t.TempDir()
		if err := unpackZip(artifact, dest, "bin/mcp"); err != nil {
			t.Fatalf("unpackZip: %v", err)
		}
		info, err := os.Stat(filepath.Join(dest, "bin", "mcp"))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o755 {
			t.Errorf("executable mode = %o; want 0755", got)
		}
	})

	t.Run("refuses traversal", func(t *testing.T) {
		artifact := build(t, func(zw *zip.Writer) {
			w, err := zw.Create("../evil")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
		})
		err := unpackZip(artifact, t.TempDir(), "bin/mcp")
		if !errors.Is(err, ErrPackageSourceUntrusted) {
			t.Fatalf("err = %v; want PLUGIN_PACKAGE_SOURCE_UNTRUSTED", err)
		}
	})

	t.Run("refuses symlink entry", func(t *testing.T) {
		artifact := build(t, func(zw *zip.Writer) {
			hdr := &zip.FileHeader{Name: "link"}
			hdr.SetMode(os.ModeSymlink | 0o777)
			w, err := zw.CreateHeader(hdr)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte("/etc/passwd")); err != nil {
				t.Fatal(err)
			}
		})
		err := unpackZip(artifact, t.TempDir(), "bin/mcp")
		if !errors.Is(err, ErrExecutableUntrusted) {
			t.Fatalf("err = %v; want PLUGIN_EXECUTABLE_UNTRUSTED", err)
		}
	})

	t.Run("refuses a non-zip artifact", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.zip")
		if err := os.WriteFile(path, []byte("not zip"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := unpackZip(path, t.TempDir(), "bin/mcp"); err == nil {
			t.Fatal("unpackZip accepted a non-zip artifact")
		}
	})
}

func TestFetchVerified(t *testing.T) {
	t.Parallel()

	body := []byte("artifact bytes")
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])

	fetch := func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	remote := RemoteOptions{Fetch: fetch}

	t.Run("matching digest lands the file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "a")
		if err := fetchVerified(context.Background(), remote, "u", path, want); err != nil {
			t.Fatalf("fetchVerified: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("landed bytes = %q, err = %v", got, err)
		}
	})

	t.Run("digest mismatch is the checksum sentinel", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "a")
		err := fetchVerified(context.Background(), remote, "u", path, strings.Repeat("0", 64))
		if !errors.Is(err, ErrArtifactChecksumMismatch) {
			t.Fatalf("err = %v; want PLUGIN_ARTIFACT_CHECKSUM_MISMATCH", err)
		}
	})

	t.Run("fetch failure propagates", func(t *testing.T) {
		failing := RemoteOptions{Fetch: func(context.Context, string) (io.ReadCloser, error) {
			return nil, errors.New("boom")
		}}
		err := fetchVerified(context.Background(), failing, "u", filepath.Join(t.TempDir(), "a"), want)
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("err = %v; want the fetch failure", err)
		}
	})
}

func TestRemoteDefaults(t *testing.T) {
	t.Parallel()

	t.Run("withDefaults fills every edge", func(t *testing.T) {
		r := RemoteOptions{}.withDefaults()
		if r.Fetch == nil || r.RunCommand == nil || r.PythonPath != "python3" || r.GitPath != "git" || r.PyPIBaseURL != "https://pypi.org" {
			t.Errorf("withDefaults left a zero edge: %+v", r)
		}
	})

	t.Run("defaultFetch returns a 200 body and refuses others", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/ok" {
				_, _ = w.Write([]byte("payload"))
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		rc, err := defaultFetch(context.Background(), srv.URL+"/ok")
		if err != nil {
			t.Fatalf("defaultFetch: %v", err)
		}
		got, _ := io.ReadAll(rc)
		_ = rc.Close()
		if string(got) != "payload" {
			t.Errorf("body = %q", got)
		}

		if _, err := defaultFetch(context.Background(), srv.URL+"/missing"); err == nil {
			t.Error("defaultFetch accepted a 404")
		}
		if _, err := defaultFetch(context.Background(), "http://\x00bad"); err == nil {
			t.Error("defaultFetch accepted an unparsable URL")
		}
	})

	t.Run("defaultRunCommand runs in dir and returns combined output", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("sh-based")
		}
		dir := t.TempDir()
		out, err := defaultRunCommand(context.Background(), dir, "sh", "-c", "pwd")
		if err != nil {
			t.Fatalf("defaultRunCommand: %v", err)
		}
		got, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
		if err != nil {
			t.Fatal(err)
		}
		want, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("cwd = %q; want %q", got, want)
		}

		if _, err := defaultRunCommand(context.Background(), "", "sh", "-c", "exit 3"); err == nil {
			t.Error("defaultRunCommand swallowed a non-zero exit")
		}
	})
}

// --- error-arm coverage for the materializers -------------------------------

func pypiManifest() *Manifest {
	return &Manifest{
		PluginID:   "fli",
		Executable: "venv/bin/fli",
		Package: PackageDecl{
			Source:   SourcePyPI,
			Ref:      "fli==1.2.0",
			Checksum: "sha256:" + strings.Repeat("a", 64),
		},
	}
}

func TestMaterializePyPIErrorArms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fetchJSON := func(payload string) func(context.Context, string) (io.ReadCloser, error) {
		return func(context.Context, string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(payload)), nil
		}
	}
	noExec := func(context.Context, string, string, ...string) ([]byte, error) {
		t.Fatal("RunCommand reached; the arm under test precedes it")
		return nil, nil
	}

	t.Run("ref without == is refused", func(t *testing.T) {
		m := pypiManifest()
		m.Package.Ref = "fli"
		err := materializePyPI(ctx, m, t.TempDir(), RemoteOptions{Fetch: fetchJSON(`{}`), RunCommand: noExec}.withDefaults())
		if !errors.Is(err, ErrManifestInvalid) {
			t.Fatalf("err = %v; want ErrManifestInvalid", err)
		}
	})

	t.Run("metadata fetch failure propagates", func(t *testing.T) {
		remote := RemoteOptions{Fetch: func(context.Context, string) (io.ReadCloser, error) {
			return nil, errors.New("index down")
		}, RunCommand: noExec}.withDefaults()
		err := materializePyPI(ctx, pypiManifest(), t.TempDir(), remote)
		if err == nil || !strings.Contains(err.Error(), "index down") {
			t.Fatalf("err = %v; want the fetch failure", err)
		}
	})

	t.Run("unparsable metadata is refused", func(t *testing.T) {
		remote := RemoteOptions{Fetch: fetchJSON("not json"), RunCommand: noExec}.withDefaults()
		err := materializePyPI(ctx, pypiManifest(), t.TempDir(), remote)
		if err == nil || !strings.Contains(err.Error(), "pypi metadata") {
			t.Fatalf("err = %v; want the metadata decode failure", err)
		}
	})

	t.Run("venv and pip failures surface their output", func(t *testing.T) {
		artifact := []byte("sdist")
		sum := sha256.Sum256(artifact)
		m := pypiManifest()
		m.Package.Checksum = "sha256:" + hex.EncodeToString(sum[:])

		meta := `{"urls":[{"filename":"fli.tar.gz","url":"u","digests":{"sha256":"` + hex.EncodeToString(sum[:]) + `"}}]}`
		calls := 0
		fetch := func(_ context.Context, url string) (io.ReadCloser, error) {
			if url == "u" {
				return io.NopCloser(bytes.NewReader(artifact)), nil
			}
			return io.NopCloser(strings.NewReader(meta)), nil
		}

		failAt := func(n int) func(context.Context, string, string, ...string) ([]byte, error) {
			calls = 0
			return func(context.Context, string, string, ...string) ([]byte, error) {
				calls++
				if calls == n {
					return []byte("tool exploded"), errors.New("exit 1")
				}
				return nil, nil
			}
		}

		err := materializePyPI(ctx, m, t.TempDir(), RemoteOptions{Fetch: fetch, RunCommand: failAt(1)}.withDefaults())
		if err == nil || !strings.Contains(err.Error(), "venv") || !strings.Contains(err.Error(), "tool exploded") {
			t.Fatalf("venv arm err = %v", err)
		}

		err = materializePyPI(ctx, m, t.TempDir(), RemoteOptions{Fetch: fetch, RunCommand: failAt(2)}.withDefaults())
		if err == nil || !strings.Contains(err.Error(), "pip install") {
			t.Fatalf("pip arm err = %v", err)
		}
	})
}

func TestMaterializeGitErrorArms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pin := strings.Repeat("a", 40)

	gitManifest := func(ref string) *Manifest {
		return &Manifest{
			PluginID:   "gitp",
			Executable: "bin/mcp",
			Package:    PackageDecl{Source: SourceGit, Ref: ref},
		}
	}

	t.Run("clone failure surfaces its output", func(t *testing.T) {
		remote := RemoteOptions{RunCommand: func(context.Context, string, string, ...string) ([]byte, error) {
			return []byte("fatal: repository not found"), errors.New("exit 128")
		}}.withDefaults()
		err := materializeGit(ctx, gitManifest("https://x/r.git@"+pin), t.TempDir(), false, remote)
		if err == nil || !strings.Contains(err.Error(), "git clone") {
			t.Fatalf("err = %v; want the clone failure", err)
		}
	})

	t.Run("a HEAD that disagrees with the pin is refused", func(t *testing.T) {
		remote := RemoteOptions{RunCommand: func(_ context.Context, _ string, _ string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "rev-parse" {
				return []byte(strings.Repeat("b", 40) + "\n"), nil
			}
			return nil, nil
		}}.withDefaults()
		err := materializeGit(ctx, gitManifest("https://x/r.git@"+pin), t.TempDir(), false, remote)
		if !errors.Is(err, ErrPackageSourceUntrusted) {
			t.Fatalf("err = %v; want PLUGIN_PACKAGE_SOURCE_UNTRUSTED", err)
		}
	})

	t.Run("rev-parse failure propagates", func(t *testing.T) {
		remote := RemoteOptions{RunCommand: func(_ context.Context, _ string, _ string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "rev-parse" {
				return []byte("boom"), errors.New("exit 1")
			}
			return nil, nil
		}}.withDefaults()
		err := materializeGit(ctx, gitManifest("https://x/r.git@"+pin), t.TempDir(), false, remote)
		if err == nil || !strings.Contains(err.Error(), "rev-parse") {
			t.Fatalf("err = %v; want the rev-parse failure", err)
		}
	})

	t.Run("checkout failure propagates", func(t *testing.T) {
		remote := RemoteOptions{RunCommand: func(_ context.Context, _ string, _ string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "checkout" {
				return []byte("unknown revision"), errors.New("exit 1")
			}
			return nil, nil
		}}.withDefaults()
		err := materializeGit(ctx, gitManifest("https://x/r.git@"+pin), t.TempDir(), false, remote)
		if err == nil || !strings.Contains(err.Error(), "git checkout") {
			t.Fatalf("err = %v; want the checkout failure", err)
		}
	})

	t.Run("a clone that produced nothing fails the copy", func(t *testing.T) {
		// Every git command "succeeds" but the clone dir never appears,
		// so the tree copy has nothing to walk.
		remote := RemoteOptions{RunCommand: func(_ context.Context, _ string, _ string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "rev-parse" {
				return []byte(pin + "\n"), nil
			}
			return nil, nil
		}}.withDefaults()
		err := materializeGit(ctx, gitManifest("https://x/r.git@"+pin), t.TempDir(), false, remote)
		if err == nil {
			t.Fatal("materializeGit succeeded with no clone on disk")
		}
	})
}

func TestMaterializePackageDispatch(t *testing.T) {
	t.Parallel()

	// bundled has nothing to fetch.
	m := &Manifest{PluginID: "b", Executable: "bin/mcp", Package: PackageDecl{Source: SourceBundled}}
	if err := materializePackage(context.Background(), m, t.TempDir(), false, RemoteOptions{}); err != nil {
		t.Errorf("bundled: %v", err)
	}

	// The default guard holds even though LoadManifest refuses the kind
	// first.
	m = &Manifest{PluginID: "n", Executable: "bin/mcp", Package: PackageDecl{Source: "npm"}}
	if err := materializePackage(context.Background(), m, t.TempDir(), false, RemoteOptions{}); !errors.Is(err, ErrManifestInvalid) {
		t.Errorf("unknown kind err = %v; want ErrManifestInvalid", err)
	}
}

type tornReader struct{}

func (tornReader) Read([]byte) (int, error) { return 0, errors.New("read torn") }

func TestFetchVerifiedIOArms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("existing destination refuses O_EXCL", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "a")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		remote := RemoteOptions{Fetch: func(context.Context, string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("y")), nil
		}}
		if err := fetchVerified(ctx, remote, "u", path, strings.Repeat("0", 64)); err == nil {
			t.Error("fetchVerified overwrote an existing file")
		}
	})

	t.Run("torn body read propagates", func(t *testing.T) {
		remote := RemoteOptions{Fetch: func(context.Context, string) (io.ReadCloser, error) {
			return io.NopCloser(tornReader{}), nil
		}}
		err := fetchVerified(ctx, remote, "u", filepath.Join(t.TempDir(), "a"), strings.Repeat("0", 64))
		if err == nil || !strings.Contains(err.Error(), "read torn") {
			t.Errorf("err = %v; want the read failure", err)
		}
	})
}

func TestWriteEntryArms(t *testing.T) {
	t.Parallel()

	t.Run("dot entry is a no-op", func(t *testing.T) {
		if err := writeEntry(t.TempDir(), ".", "bin/mcp", strings.NewReader("")); err != nil {
			t.Errorf("dot entry: %v", err)
		}
	})

	t.Run("traversal is refused", func(t *testing.T) {
		err := writeEntry(t.TempDir(), "../evil", "bin/mcp", strings.NewReader("x"))
		if !errors.Is(err, ErrPackageSourceUntrusted) {
			t.Errorf("err = %v; want PLUGIN_PACKAGE_SOURCE_UNTRUSTED", err)
		}
	})

	t.Run("a file blocking the parent dir fails", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bin"), []byte("file, not dir"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writeEntry(dir, "bin/mcp", "bin/mcp", strings.NewReader("x")); err == nil {
			t.Error("writeEntry created a file under a non-directory")
		}
	})

	t.Run("a directory at the destination fails the open", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeEntry(dir, "data", "bin/mcp", strings.NewReader("x")); err == nil {
			t.Error("writeEntry opened a directory for writing")
		}
	})

	t.Run("torn reader propagates", func(t *testing.T) {
		if err := writeEntry(t.TempDir(), "f", "bin/mcp", tornReader{}); err == nil {
			t.Error("writeEntry swallowed a torn read")
		}
	})
}

func TestUnpackEntryDirArms(t *testing.T) {
	t.Parallel()

	t.Run("tar directory traversal is refused", func(t *testing.T) {
		artifact := buildTarGz(t, []tarEntry{{name: "../escape"}})
		err := unpackTarGz(artifact, t.TempDir(), "bin/mcp")
		if !errors.Is(err, ErrPackageSourceUntrusted) {
			t.Errorf("err = %v; want PLUGIN_PACKAGE_SOURCE_UNTRUSTED", err)
		}
	})

	t.Run("tar dot directory is skipped", func(t *testing.T) {
		artifact := buildTarGz(t, []tarEntry{{name: "./"}, {name: "bin"}, {name: "bin/mcp", body: []byte("x")}})
		if err := unpackTarGz(artifact, t.TempDir(), "bin/mcp"); err != nil {
			t.Errorf("unpackTarGz: %v", err)
		}
	})

	t.Run("missing artifact fails the open", func(t *testing.T) {
		if err := unpackTarGz(filepath.Join(t.TempDir(), "absent.tar.gz"), t.TempDir(), "x"); err == nil {
			t.Error("unpackTarGz opened a missing artifact")
		}
	})

	t.Run("zip directory traversal is refused", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		if _, err := zw.Create("../escape/"); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "a.zip")
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		err := unpackZip(path, t.TempDir(), "bin/mcp")
		if !errors.Is(err, ErrPackageSourceUntrusted) {
			t.Errorf("err = %v; want PLUGIN_PACKAGE_SOURCE_UNTRUSTED", err)
		}
	})

	t.Run("zip dot directory is skipped", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		if _, err := zw.Create("./"); err != nil {
			t.Fatal(err)
		}
		w, err := zw.Create("bin/mcp")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "a.zip")
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := unpackZip(path, t.TempDir(), "bin/mcp"); err != nil {
			t.Errorf("unpackZip: %v", err)
		}
	})
}

func TestDefaultFetchConnectionRefused(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	if _, err := defaultFetch(context.Background(), url); err == nil {
		t.Error("defaultFetch reached a closed server")
	}
}

func TestNormalizeArgvNilManifest(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeArgv("/x", nil, false); !errors.Is(err, ErrExecutableUntrusted) {
		t.Errorf("err = %v; want PLUGIN_EXECUTABLE_UNTRUSTED", err)
	}
}

func TestUnpackTarGzStreamArms(t *testing.T) {
	t.Parallel()

	t.Run("gzip of non-tar bytes is refused", func(t *testing.T) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write([]byte("valid gzip, garbage tar")); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "a.tar.gz")
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := unpackTarGz(path, t.TempDir(), "bin/mcp"); err == nil {
			t.Error("unpackTarGz accepted a gzip stream with no tar inside")
		}
	})

	t.Run("a file blocking a directory entry fails", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bin"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		artifact := buildTarGz(t, []tarEntry{{name: "bin"}})
		if err := unpackTarGz(artifact, dir, "bin/mcp"); err == nil {
			t.Error("unpackTarGz created a directory over a file")
		}
	})
}

func TestUnpackZipDirBlocked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bin"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("bin/"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := unpackZip(path, dir, "bin/mcp"); err == nil {
		t.Error("unpackZip created a directory over a file")
	}
}
