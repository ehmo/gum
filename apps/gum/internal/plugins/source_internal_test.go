package plugins

// Unit coverage for the §8.7 [package] declaration primitives: the closed
// source enum, per-source ref/checksum shapes, git ref splitting, and the
// inventory risk marking.

import (
	"errors"
	"strings"
	"testing"
)

const testSHA256 = "sha256:" + "ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12"

func TestValidatePackageDecl(t *testing.T) {
	t.Parallel()

	valid := []PackageDecl{
		{}, // absent block = local
		{Source: SourceLocal},
		{Source: SourceBundled},
		{Source: SourcePyPI, Ref: "flights==1.2.0", Checksum: testSHA256},
		{Source: SourcePyPI, Ref: "a==0.0.1!rc1", Checksum: testSHA256},
		{Source: SourceGitHubRelease, Ref: "https://github.com/acme/fli/releases/download/v1/fli.tar.gz", Checksum: testSHA256},
		{Source: SourceGitHubRelease, Ref: "https://example.com/fli.tgz", Checksum: testSHA256},
		{Source: SourceGitHubRelease, Ref: "https://example.com/fli.zip", Checksum: testSHA256},
		{Source: SourceGit, Ref: "https://github.com/acme/fli.git@" + strings.Repeat("a", 40)},
		{Source: SourceGit, Ref: "file:///srv/fli.git@" + strings.Repeat("0", 40)},
		{Source: SourceGit, Ref: "https://github.com/acme/fli.git"}, // unpinned: refused later, at install
	}
	for _, p := range valid {
		if err := validatePackageDecl(p); err != nil {
			t.Errorf("validatePackageDecl(%+v) = %v; want nil", p, err)
		}
	}

	invalid := []PackageDecl{
		{Source: "npm"},
		{Source: SourcePyPI, Ref: "flights>=1.0", Checksum: testSHA256},
		{Source: SourcePyPI, Ref: "flights", Checksum: testSHA256},
		{Source: SourcePyPI, Ref: "flights==1.2.0"},                                // no checksum
		{Source: SourcePyPI, Ref: "flights==1.2.0", Checksum: "sha256:short"},      // bad hex
		{Source: SourcePyPI, Ref: "flights==1.2.0", Checksum: "md5:" + testSHA256}, // wrong algo
		{Source: SourceGitHubRelease, Ref: "http://example.com/fli.tar.gz", Checksum: testSHA256},
		{Source: SourceGitHubRelease, Ref: "https://example.com/fli.exe", Checksum: testSHA256},
		{Source: SourceGitHubRelease, Ref: "https://example.com/fli.tar.gz"}, // no checksum
		{Source: SourceGit, Ref: "git@github.com:acme/fli.git@" + strings.Repeat("a", 40)},
		{Source: SourceGit, Ref: "ssh://host/fli.git"},
	}
	for _, p := range invalid {
		if err := validatePackageDecl(p); !errors.Is(err, ErrManifestInvalid) {
			t.Errorf("validatePackageDecl(%+v) = %v; want ErrManifestInvalid", p, err)
		}
	}
}

func TestSplitGitRef(t *testing.T) {
	t.Parallel()
	pin := strings.Repeat("a", 40)

	cases := []struct {
		ref, url, pin string
	}{
		{"https://github.com/acme/fli.git@" + pin, "https://github.com/acme/fli.git", pin},
		{"https://github.com/acme/fli.git", "https://github.com/acme/fli.git", ""},
		// A tail that is not 40-hex stays in the URL.
		{"https://github.com/acme/fli.git@main", "https://github.com/acme/fli.git@main", ""},
		{"https://host/x@y/fli.git@" + pin, "https://host/x@y/fli.git", pin},
	}
	for _, tc := range cases {
		url, gotPin := splitGitRef(tc.ref)
		if url != tc.url || gotPin != tc.pin {
			t.Errorf("splitGitRef(%q) = (%q, %q); want (%q, %q)", tc.ref, url, gotPin, tc.url, tc.pin)
		}
	}
}

func TestPackageRisk(t *testing.T) {
	t.Parallel()

	if got := packageRisk(PackageDecl{}); got != "dev-untrusted" {
		t.Errorf("local risk = %q; want dev-untrusted", got)
	}
	unpinned := PackageDecl{Source: SourceGit, Ref: "https://github.com/acme/fli.git"}
	if got := packageRisk(unpinned); got != "dev-untrusted" {
		t.Errorf("unpinned git risk = %q; want dev-untrusted", got)
	}
	pinned := PackageDecl{Source: SourceGit, Ref: "https://github.com/acme/fli.git@" + strings.Repeat("a", 40)}
	if got := packageRisk(pinned); got != "" {
		t.Errorf("pinned git risk = %q; want empty", got)
	}
	pypi := PackageDecl{Source: SourcePyPI, Ref: "fli==1.2.0", Checksum: testSHA256}
	if got := packageRisk(pypi); got != "" {
		t.Errorf("pypi risk = %q; want empty", got)
	}
}

func TestChecksumHelpers(t *testing.T) {
	t.Parallel()

	if got := checksumHex(testSHA256); got != strings.TrimPrefix(testSHA256, "sha256:") {
		t.Errorf("checksumHex = %q", got)
	}
	if hasArchiveExtension("https://x/y.exe") {
		t.Error("hasArchiveExtension accepted .exe")
	}
	if archiveExtension("https://x/y.zip") != ".zip" || archiveExtension("https://x/y.tgz") != ".tgz" || archiveExtension("https://x/y.tar.gz") != ".tar.gz" {
		t.Error("archiveExtension mapped a suffix wrong")
	}
}
