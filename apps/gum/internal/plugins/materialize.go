package plugins

// Spec §8.7 package materialization: after the manifest validates, the
// declared [package] source is fetched, verified against the manifest's
// pin, and landed inside the plugin's install root. The manifest directory
// itself is copied over the result afterward, so the curated manifest and
// its schemas always win over anything a fetched artifact carries.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RemoteOptions carries the injectable edges of package materialization.
// Production uses the zero value; tests point Fetch at an httptest server,
// PyPIBaseURL at a fake index, and the tool paths at stub scripts.
type RemoteOptions struct {
	// Fetch retrieves one URL. Nil means net/http with the request context.
	Fetch func(ctx context.Context, url string) (io.ReadCloser, error)
	// RunCommand runs one subprocess in dir (empty dir inherits the
	// caller's) and returns its combined output. Nil means os/exec.
	RunCommand func(ctx context.Context, dir, name string, args ...string) ([]byte, error)
	// PythonPath is the interpreter that builds the venv. Empty means
	// "python3".
	PythonPath string
	// GitPath is the git binary. Empty means "git".
	GitPath string
	// PyPIBaseURL is the index root for the PyPI JSON API. Empty means
	// "https://pypi.org".
	PyPIBaseURL string
}

func (r RemoteOptions) withDefaults() RemoteOptions {
	if r.Fetch == nil {
		r.Fetch = defaultFetch
	}
	if r.RunCommand == nil {
		r.RunCommand = defaultRunCommand
	}
	if r.PythonPath == "" {
		r.PythonPath = "python3"
	}
	if r.GitPath == "" {
		r.GitPath = "git"
	}
	if r.PyPIBaseURL == "" {
		r.PyPIBaseURL = "https://pypi.org"
	}
	return r
}

func defaultFetch(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("fetch %s: status %s", rawURL, resp.Status)
	}
	return resp.Body, nil
}

func defaultRunCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// materializePackage lands the declared package source inside installDir.
// local and bundled have nothing to fetch: their code is the manifest
// directory the caller copies afterward.
func materializePackage(ctx context.Context, m *Manifest, installDir string, profileIsDev bool, remote RemoteOptions) error {
	switch m.Package.Kind() {
	case SourceLocal, SourceBundled:
		return nil
	case SourcePyPI:
		return materializePyPI(ctx, m, installDir, remote.withDefaults())
	case SourceGitHubRelease:
		return materializeGitHubRelease(ctx, m, installDir, remote.withDefaults())
	case SourceGit:
		return materializeGit(ctx, m, installDir, profileIsDev, remote.withDefaults())
	default:
		// LoadManifest already refused unknown kinds; keep the guard.
		return fmt.Errorf("%w: unknown package source %q", ErrManifestInvalid, m.Package.Source)
	}
}

// pypiArtifact is the slice of the PyPI JSON API response the resolver
// consumes: one downloadable file with its digests.
type pypiArtifact struct {
	Filename string            `json:"filename"`
	URL      string            `json:"url"`
	Digests  map[string]string `json:"digests"`
}

// materializePyPI implements the §8.7 pypi rule: resolve the exact pinned
// release through the index metadata, download the one artifact whose
// SHA-256 equals the manifest checksum, verify the downloaded bytes, then
// build a virtualenv under the install root and install the verified
// artifact into it offline. `uvx` (or any resolver) is never spawned; the
// runtime binds to venv/bin/<script> inside the install root.
func materializePyPI(ctx context.Context, m *Manifest, installDir string, remote RemoteOptions) error {
	name, version, ok := strings.Cut(m.Package.Ref, "==")
	if !ok {
		return fmt.Errorf("%w: pypi ref %q is not name==version", ErrManifestInvalid, m.Package.Ref)
	}
	want := checksumHex(m.Package.Checksum)

	metaURL := fmt.Sprintf("%s/pypi/%s/%s/json", strings.TrimSuffix(remote.PyPIBaseURL, "/"),
		url.PathEscape(name), url.PathEscape(version))
	body, err := remote.Fetch(ctx, metaURL)
	if err != nil {
		return fmt.Errorf("plugin install: pypi metadata: %w", err)
	}
	defer func() { _ = body.Close() }()

	var meta struct {
		URLs []pypiArtifact `json:"urls"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 8<<20)).Decode(&meta); err != nil {
		return fmt.Errorf("plugin install: pypi metadata: %w", err)
	}

	// The checksum, not a packagetype preference, selects the artifact:
	// the manifest pins exactly one file of the release.
	var artifact *pypiArtifact
	for i := range meta.URLs {
		if meta.URLs[i].Digests["sha256"] == want {
			artifact = &meta.URLs[i]
			break
		}
	}
	if artifact == nil {
		return fmt.Errorf("%w: no %s artifact in the index matches sha256:%s",
			ErrArtifactChecksumMismatch, m.Package.Ref, want)
	}

	tmpDir, err := os.MkdirTemp("", "gum-pypi-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	artifactPath := filepath.Join(tmpDir, filepath.Base(artifact.Filename))
	if err := fetchVerified(ctx, remote, artifact.URL, artifactPath, want); err != nil {
		return err
	}

	// venv under the verified install root, then an offline install of the
	// verified artifact only. --no-index forbids network resolution;
	// --no-deps forbids pulling in anything the checksum does not cover.
	if out, err := remote.RunCommand(ctx, installDir, remote.PythonPath, "-m", "venv", "venv"); err != nil {
		return fmt.Errorf("plugin install: venv: %w: %s", err, strings.TrimSpace(string(out)))
	}
	pip := filepath.Join(installDir, "venv", "bin", "pip")
	if out, err := remote.RunCommand(ctx, installDir, pip, "install", "--no-index", "--no-deps", artifactPath); err != nil {
		return fmt.Errorf("plugin install: pip install: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// materializeGitHubRelease downloads the pinned release artifact, verifies
// its SHA-256 against the manifest checksum, and unpacks it into installDir
// with pinned modes (§8.7 verification steps 1-2).
func materializeGitHubRelease(ctx context.Context, m *Manifest, installDir string, remote RemoteOptions) error {
	want := checksumHex(m.Package.Checksum)

	tmpDir, err := os.MkdirTemp("", "gum-release-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	artifactPath := filepath.Join(tmpDir, "artifact"+archiveExtension(m.Package.Ref))
	if err := fetchVerified(ctx, remote, m.Package.Ref, artifactPath, want); err != nil {
		return err
	}

	if strings.HasSuffix(artifactPath, ".zip") {
		return unpackZip(artifactPath, installDir, m.Executable)
	}
	return unpackTarGz(artifactPath, installDir, m.Executable)
}

// materializeGit clones the declared repository, detaches at the pinned
// commit, verifies the resolved HEAD equals the pin, strips .git, and
// copies the tree into installDir with pinned modes. An unpinned ref is a
// moving target with no checksum behind it, so it is dev-only (§8.7).
func materializeGit(ctx context.Context, m *Manifest, installDir string, profileIsDev bool, remote RemoteOptions) error {
	repoURL, pin := splitGitRef(m.Package.Ref)
	if pin == "" && !profileIsDev {
		return fmt.Errorf("%w: git ref %q has no commit pin; unpinned clones are dev-only",
			ErrPackageSourceUntrusted, m.Package.Ref)
	}

	tmpDir, err := os.MkdirTemp("", "gum-git-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cloneDir := filepath.Join(tmpDir, "clone")
	if out, err := remote.RunCommand(ctx, "", remote.GitPath, "clone", "--quiet", repoURL, cloneDir); err != nil {
		return fmt.Errorf("plugin install: git clone: %w: %s", err, strings.TrimSpace(string(out)))
	}

	if pin != "" {
		if out, err := remote.RunCommand(ctx, cloneDir, remote.GitPath, "checkout", "--quiet", "--detach", pin); err != nil {
			return fmt.Errorf("plugin install: git checkout %s: %w: %s", pin, err, strings.TrimSpace(string(out)))
		}
		out, err := remote.RunCommand(ctx, cloneDir, remote.GitPath, "rev-parse", "HEAD")
		if err != nil {
			return fmt.Errorf("plugin install: git rev-parse: %w: %s", err, strings.TrimSpace(string(out)))
		}
		if head := strings.TrimSpace(string(out)); head != pin {
			return fmt.Errorf("%w: resolved commit %s does not match pin %s",
				ErrPackageSourceUntrusted, head, pin)
		}
	}

	if err := os.RemoveAll(filepath.Join(cloneDir, ".git")); err != nil {
		return err
	}
	if err := copyPluginTree(cloneDir, installDir, m.Executable); err != nil {
		return fmt.Errorf("plugin install: %w", err)
	}
	return nil
}

// fetchVerified downloads url to path and fails with
// PLUGIN_ARTIFACT_CHECKSUM_MISMATCH unless the bytes hash to wantHex. The
// hash is computed over the bytes written, so a corrupted or substituted
// download can never land verified.
func fetchVerified(ctx context.Context, remote RemoteOptions, url, path, wantHex string) error {
	body, err := remote.Fetch(ctx, url)
	if err != nil {
		return fmt.Errorf("plugin install: fetch artifact: %w", err)
	}
	defer func() { _ = body.Close() }()

	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, hasher), body)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("plugin install: fetch artifact: %w", copyErr)
	}
	if closeErr != nil {
		return closeErr
	}

	if got := hex.EncodeToString(hasher.Sum(nil)); got != wantHex {
		return fmt.Errorf("%w: artifact sha256 %s, manifest declares %s",
			ErrArtifactChecksumMismatch, got, wantHex)
	}
	return nil
}

// entryDest maps an archive member name to its destination under
// installDir, refusing absolute paths and traversal.
func entryDest(installDir, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." {
		return "", nil
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: artifact entry %q escapes the install root",
			ErrPackageSourceUntrusted, name)
	}
	return filepath.Join(installDir, clean), nil
}

// writeEntry lands one archive member with pinned modes: 0o755 for the
// declared executable, 0o644 for everything else, matching copyPluginTree.
func writeEntry(installDir, name string, executable string, r io.Reader) error {
	dest, err := entryDest(installDir, name)
	if err != nil || dest == "" {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	mode := os.FileMode(0o644)
	rel := filepath.Clean(filepath.FromSlash(name))
	if rel == executable {
		mode = 0o755
	}

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dest, mode)
}

func unpackTarGz(artifactPath, installDir, executable string) error {
	f, err := os.Open(artifactPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("plugin install: unpack: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("plugin install: unpack: %w", err)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			dest, err := entryDest(installDir, hdr.Name)
			if err != nil || dest == "" {
				if err != nil {
					return err
				}
				continue
			}
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeEntry(installDir, hdr.Name, executable, tr); err != nil {
				return err
			}
		default:
			// Symlinks, hardlinks, and devices never land: a link is how
			// an artifact reaches outside the install root or aliases the
			// verified executable.
			return fmt.Errorf("%w: artifact contains non-regular entry %q",
				ErrExecutableUntrusted, hdr.Name)
		}
	}
}

func unpackZip(artifactPath, installDir, executable string) error {
	zr, err := zip.OpenReader(artifactPath)
	if err != nil {
		return fmt.Errorf("plugin install: unpack: %w", err)
	}
	defer func() { _ = zr.Close() }()

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			dest, err := entryDest(installDir, zf.Name)
			if err != nil || dest == "" {
				if err != nil {
					return err
				}
				continue
			}
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if !zf.Mode().IsRegular() {
			return fmt.Errorf("%w: artifact contains non-regular entry %q",
				ErrExecutableUntrusted, zf.Name)
		}

		rc, err := zf.Open()
		if err != nil {
			return fmt.Errorf("plugin install: unpack: %w", err)
		}
		writeErr := writeEntry(installDir, zf.Name, executable, rc)
		_ = rc.Close()
		if writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// archiveExtension returns the archive suffix of a validated release URL.
func archiveExtension(ref string) string {
	switch {
	case strings.HasSuffix(ref, ".zip"):
		return ".zip"
	case strings.HasSuffix(ref, ".tgz"):
		return ".tgz"
	default:
		return ".tar.gz"
	}
}
