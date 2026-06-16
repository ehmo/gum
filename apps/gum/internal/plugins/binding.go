package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// ErrExecutableUntrusted is the host-side rendering of the spec §11 sentinel
// PLUGIN_EXECUTABLE_UNTRUSTED. It fires whenever the runtime spawn path
// cannot prove that the binary about to be launched is the one verified at
// install time (digest mismatch, install-root escape, shell interpreter,
// or relative path).
var ErrExecutableUntrusted = errors.New("PLUGIN_EXECUTABLE_UNTRUSTED")

// ExecutableBinding mirrors the plugins.lock entry fields the host needs to
// authorise a spawn (spec §8.7 line 1690). The full lock row carries more
// metadata (`name`, `version`, `source`, `ref`, `checksum`) which is
// irrelevant to digest re-check and therefore omitted here.
type ExecutableBinding struct {
	Name             string
	InstallRoot      string
	ExecutablePath   string
	ExecutableSHA256 string
	ArgvNormalized   []string
}

// VerifyExecutableBinding enforces the spec §8.7 line 1690 contract on every
// plugin spawn:
//
//  1. ExecutablePath MUST be absolute.
//  2. ExecutablePath MUST be inside InstallRoot (no symlink escape).
//  3. The file basename MUST NOT be a shell interpreter (sh/bash/zsh/cmd/powershell).
//  4. The re-hashed sha256 of ExecutablePath MUST equal ExecutableSHA256.
//
// All four failures wrap ErrExecutableUntrusted so callers route them to
// quarantine + PLUGIN_EXECUTABLE_UNTRUSTED reporting.
func VerifyExecutableBinding(b *ExecutableBinding) error {
	if b == nil {
		return fmt.Errorf("%w: nil binding", ErrExecutableUntrusted)
	}
	if b.InstallRoot == "" || b.ExecutablePath == "" || b.ExecutableSHA256 == "" {
		return fmt.Errorf("%w: incomplete binding for %q", ErrExecutableUntrusted, b.Name)
	}
	if !filepath.IsAbs(b.ExecutablePath) {
		return fmt.Errorf("%w: executable_path %q is not absolute", ErrExecutableUntrusted, b.ExecutablePath)
	}
	if base := strings.ToLower(filepath.Base(b.ExecutablePath)); shellInterpreters[base] {
		return fmt.Errorf("%w: executable_path %q is a shell interpreter", ErrExecutableUntrusted, b.ExecutablePath)
	}
	// install-root containment check: resolve real paths so a symlink that
	// escapes the root is caught here, not after the spawn.
	rootResolved, err := filepath.EvalSymlinks(b.InstallRoot)
	if err != nil {
		return fmt.Errorf("%w: install_root resolve: %v", ErrExecutableUntrusted, err)
	}
	execResolved, err := filepath.EvalSymlinks(b.ExecutablePath)
	if err != nil {
		return fmt.Errorf("%w: executable resolve: %v", ErrExecutableUntrusted, err)
	}
	rel, err := filepath.Rel(rootResolved, execResolved)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return fmt.Errorf("%w: executable_path %q escapes install_root %q",
			ErrExecutableUntrusted, b.ExecutablePath, b.InstallRoot)
	}

	got, err := hashFileSHA256(b.ExecutablePath)
	if err != nil {
		return fmt.Errorf("%w: hash: %v", ErrExecutableUntrusted, err)
	}
	if !equalDigest(got, b.ExecutableSHA256) {
		return fmt.Errorf("%w: digest mismatch for %s: lock=%s actual=%s",
			ErrExecutableUntrusted, b.Name, b.ExecutableSHA256, got)
	}
	return nil
}

// shellInterpreters is the spec §8.7 line 1690 deny-list of binaries that
// MUST NOT appear as a plugin's executable_path outside dev profiles.
var shellInterpreters = map[string]bool{
	"sh":             true,
	"bash":           true,
	"zsh":            true,
	"dash":           true,
	"cmd":            true,
	"cmd.exe":        true,
	"powershell":     true,
	"powershell.exe": true,
	"pwsh":           true,
	"pwsh.exe":       true,
}

// hashFileSHA256 streams the file through sha256 and returns the lowercase hex
// digest. Streamed rather than read-into-memory so a multi-megabyte plugin
// binary doesn't briefly double its RAM footprint at spawn time.
func hashFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// equalDigest compares two hex digests case-insensitively and tolerates the
// "sha256:" / "SHA256:" prefix that some manifest providers (e.g. PyPI's
// `--digest sha256:<hex>` flag) emit verbatim into plugins.lock.
func equalDigest(got, want string) bool {
	clean := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.TrimPrefix(s, "sha256:")
		return s
	}
	return clean(got) == clean(want)
}

// QuarantinePlugin records a runtime quarantine in plugin-state.json under
// the spec §8.7 protocol: set quarantined=true, quarantined_at=now (RFC 3339
// UTC), last_error_code=<sentinel>. The mutation runs inside a registry
// write transaction so it shares one (install_generation, install_txid) pair
// with the other two files.
//
// If the plugin row is absent (e.g. a stray quarantine call), QuarantinePlugin
// returns nil — the operator has presumably removed the install already and
// there is nothing to mark.
func QuarantinePlugin(ctx context.Context, reg *registry.Registry, pluginName, errorCode string) error {
	return reg.WriteTransaction(ctx, func(f *registry.Files) error {
		for i, raw := range f.State.Plugins {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if name, _ := m["name"].(string); name != pluginName {
				continue
			}
			m["quarantined"] = true
			m["quarantined_at"] = time.Now().UTC().Format(time.RFC3339)
			m["last_error_code"] = errorCode
			f.State.Plugins[i] = m
			return nil
		}
		return nil
	})
}
