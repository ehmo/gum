package plugins

// Spec §8.2 "`source`, `ref` and `checksum`": the manifest, not the CLI
// argument, names where the plugin's code comes from. `gum plugin install`
// still takes a local manifest directory; a remote source is fetched from
// the [package] declaration after the manifest validates, so the curated
// manifest stays the trust anchor for the artifact checksum and the pinned
// ref, and the artifact can never attest to itself.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrArtifactChecksumMismatch is the stable envelope for a fetched artifact
// whose SHA-256 does not match the manifest's declared checksum (§8.7
// verification step 1).
var ErrArtifactChecksumMismatch = errors.New("PLUGIN_ARTIFACT_CHECKSUM_MISMATCH")

// ErrPackageSourceUntrusted is the stable envelope for a package declaration
// a non-dev profile must not install from: an unpinned git ref, or a fetch
// that resolves outside the declaration. The manifest itself is well-formed;
// the profile's trust policy refuses it (§8.7 "Cloning an unpinned default
// branch is dev-only").
var ErrPackageSourceUntrusted = errors.New("PLUGIN_PACKAGE_SOURCE_UNTRUSTED")

// Package source kinds, the spec §8.7 closed enum. An absent [package] block
// means local, which is how every manifest written before the block existed
// loads.
const (
	SourceLocal         = "local"
	SourceBundled       = "bundled"
	SourcePyPI          = "pypi"
	SourceGitHubRelease = "github_release"
	SourceGit           = "git"
)

// PackageDecl is the manifest's [package] block: where the plugin's code
// comes from, pinned. Source is the §8.7 enum; Ref and Checksum are
// source-specific and validated at load time.
type PackageDecl struct {
	Source   string `json:"source,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Checksum string `json:"checksum,omitempty"`
}

// Kind returns the normalized source kind: an absent source is local.
func (p PackageDecl) Kind() string {
	if p.Source == "" {
		return SourceLocal
	}
	return p.Source
}

var (
	// sha256ChecksumRe matches the manifest checksum format the lock file
	// records verbatim: "sha256:" plus 64 lower-case hex digits.
	sha256ChecksumRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

	// pypiRefRe matches an exact-version PyPI requirement, `name==version`.
	// Ranges and extras are refused: the checksum pins one artifact, so the
	// ref must resolve to one artifact too.
	pypiRefRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?==[A-Za-z0-9!+._-]+$`)

	// gitPinRe matches a full 40-hex commit pin. Abbreviated hashes are
	// refused: a prefix can be reassigned by growing the repository.
	gitPinRe = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// validatePackageDecl enforces the intrinsic shape of the [package] block.
// Profile-dependent policy (an unpinned git ref outside a dev profile) is
// enforced at install time, where the profile is known.
func validatePackageDecl(p PackageDecl) error {
	switch p.Kind() {
	case SourceLocal, SourceBundled:
		return nil

	case SourcePyPI:
		if !pypiRefRe.MatchString(p.Ref) {
			return fmt.Errorf("%w: pypi ref %q is not name==version", ErrManifestInvalid, p.Ref)
		}
		if !sha256ChecksumRe.MatchString(p.Checksum) {
			return fmt.Errorf("%w: pypi package requires checksum sha256:<64 hex>", ErrManifestInvalid)
		}
		return nil

	case SourceGitHubRelease:
		if !strings.HasPrefix(p.Ref, "https://") || !hasArchiveExtension(p.Ref) {
			return fmt.Errorf("%w: github_release ref %q is not an https .tar.gz/.tgz/.zip artifact URL", ErrManifestInvalid, p.Ref)
		}
		if !sha256ChecksumRe.MatchString(p.Checksum) {
			return fmt.Errorf("%w: github_release package requires checksum sha256:<64 hex>", ErrManifestInvalid)
		}
		return nil

	case SourceGit:
		url, _ := splitGitRef(p.Ref)
		if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "file://") {
			return fmt.Errorf("%w: git ref %q is not an https:// or file:// repository URL", ErrManifestInvalid, p.Ref)
		}
		return nil

	default:
		return fmt.Errorf("%w: unknown package source %q", ErrManifestInvalid, p.Source)
	}
}

// splitGitRef splits `<url>@<commit>` into its halves. The pin is the part
// after the last "@" only when it is a full 40-hex commit; anything else
// stays in the URL, so an https URL whose path contains "@" still parses.
// An unpinned ref returns pin == "".
func splitGitRef(ref string) (url, pin string) {
	idx := strings.LastIndex(ref, "@")
	if idx < 0 {
		return ref, ""
	}
	if tail := ref[idx+1:]; gitPinRe.MatchString(tail) {
		return ref[:idx], tail
	}
	return ref, ""
}

// checksumHex returns the hex half of a validated "sha256:<hex>" checksum.
func checksumHex(checksum string) string {
	return strings.TrimPrefix(checksum, "sha256:")
}

// hasArchiveExtension reports whether the artifact URL names a format the
// unpacker implements.
func hasArchiveExtension(ref string) bool {
	return strings.HasSuffix(ref, ".tar.gz") || strings.HasSuffix(ref, ".tgz") || strings.HasSuffix(ref, ".zip")
}
