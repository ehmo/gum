// Package notify implements the opt-in update notifier for `gum version`
// (gum-afcv.5). Behavior contract:
//
//   - Disabled by default. Opt-in via `gum config set notify.enabled=true`.
//   - Reads/writes a per-profile cache at <XDG_CACHE_HOME>/gum/<profile>/notify.json
//     with a 24h TTL so we don't hammer api.github.com on every invocation.
//   - On cache hit (and a newer version is recorded), prints a single-line
//     warning to stderr. NEVER to stdout — pipelines must stay clean.
//   - On cache miss/stale, fires the GitHub releases-API check in a goroutine
//     with a 2s context timeout. The goroutine writes the cache on success and
//     exits silently on any error. The caller does NOT block — the notice
//     surfaces on the NEXT invocation, matching npm/cargo/brew conventions.
//   - "dev" builds (the default ldflag value) are exempt — no check, no cache.
//
// The cache schema is JSON for human inspectability:
//
//	{"checked_at":"2026-05-24T12:34:56Z","latest_version":"v0.1.5"}
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	profilepkg "github.com/ehmo/gum/internal/profile"
)

// DefaultRepo is the upstream this notifier checks. Override in tests.
const DefaultRepo = "ehmo/gum"

// CacheTTL is the freshness window for the notify.json cache.
const CacheTTL = 24 * time.Hour

// CheckTimeout is the deadline applied to the GitHub releases API call.
const CheckTimeout = 2 * time.Second

// ConfigKey is the config.toml key that gates the notifier.
const ConfigKey = "notify.enabled"

// cacheEntry is the on-disk payload for notify.json.
type cacheEntry struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version"`
}

// CachePath returns the per-profile cache file path. Honours XDG_CACHE_HOME
// and falls back to ~/.cache when unset (same convention as internal/cache).
func CachePath(profile string) (string, error) {
	name, err := profilepkg.Parse(profile)
	if err != nil {
		return "", err
	}
	return name.NotifyPath()
}

// readCache returns the cached entry, ok=true when the file exists and parses,
// fresh=true when CheckedAt is within CacheTTL.
func readCache(path string) (entry cacheEntry, ok, fresh bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheEntry{}, false, false
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		return cacheEntry{}, false, false
	}
	fresh = time.Since(entry.CheckedAt) < CacheTTL
	return entry, true, fresh
}

// writeCache atomically persists entry to path with mode 0600. Directory is
// created with 0700. Returns the first error encountered; callers in the
// goroutine path swallow this (notifier failures must never surface).
func writeCache(path string, entry cacheEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Fetcher abstracts the GitHub releases API call so tests can inject a stub
// without touching the network.
type Fetcher interface {
	Latest(ctx context.Context, repo string) (string, error)
}

// HTTPFetcher is the production Fetcher that talks to api.github.com.
type HTTPFetcher struct {
	Client *http.Client
}

// Latest returns the tag_name of the latest release for repo (e.g., "ehmo/gum").
// Honours the supplied context for cancellation/timeout.
func (h HTTPFetcher) Latest(ctx context.Context, repo string) (string, error) {
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: CheckTimeout}
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// 404 on unreleased forks, 403 on rate-limit. Both are quiet failures.
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("github: status %d", resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", errors.New("github: empty tag_name")
	}
	return payload.TagName, nil
}

// CompareVersions returns 1 if a > b, -1 if a < b, 0 if equal. Both inputs
// are trimmed of a leading 'v'. Non-numeric segments (e.g., "-rc1") are
// compared lexicographically AFTER the numeric prefix matches; if either
// side fails to parse as semver, returns 0 (treated as equal — no warning).
func CompareVersions(a, b string) int {
	ap := splitSemver(a)
	bp := splitSemver(b)
	if ap == nil || bp == nil {
		return 0
	}
	for i := 0; i < 3; i++ {
		if ap[i] != bp[i] {
			if ap[i] > bp[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

// splitSemver returns the [major, minor, patch] integer triple for "vX.Y.Z"
// or "X.Y.Z" (any trailing -prerelease / +build metadata is discarded for
// comparison). Returns nil when the input is not a parseable semver.
func splitSemver(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if cut := strings.IndexAny(s, "-+"); cut >= 0 {
		s = s[:cut]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return out
}

// MaybeNotify is the single entry point called from `gum version`. It:
//
//  1. Returns immediately when notify.enabled is false or current == "dev".
//  2. Reads the cache. If a newer version is recorded, prints one stderr line.
//  3. If the cache is stale or missing, fires an async refresh and returns.
//
// The refresh goroutine is fire-and-forget: it MUST NOT print, panic, or
// block process exit. The supplied done channel (when non-nil) is closed
// after the goroutine finishes — used only by tests; production callers
// pass nil and let the goroutine race the process exit, which is benign
// because the writeCache is best-effort.
func MaybeNotify(stderr io.Writer, current, profile string, enabled bool, fetcher Fetcher, done chan<- struct{}) {
	closeDone := func() {
		if done != nil {
			close(done)
		}
	}
	if !enabled || current == "" || current == "dev" {
		closeDone()
		return
	}
	path, err := CachePath(profile)
	if err != nil {
		closeDone()
		return
	}
	entry, ok, fresh := readCache(path)
	if ok && entry.LatestVersion != "" && CompareVersions(entry.LatestVersion, current) > 0 {
		_, _ = fmt.Fprintf(stderr, "[notify] a newer gum is available: %s (you have %s). See https://github.com/%s/releases/latest\n",
			entry.LatestVersion, current, DefaultRepo)
	}
	if fresh {
		closeDone()
		return
	}
	if fetcher == nil {
		fetcher = HTTPFetcher{}
	}
	go func() {
		defer closeDone()
		ctx, cancel := context.WithTimeout(context.Background(), CheckTimeout)
		defer cancel()
		latest, err := fetcher.Latest(ctx, DefaultRepo)
		if err != nil {
			return
		}
		_ = writeCache(path, cacheEntry{CheckedAt: time.Now().UTC(), LatestVersion: latest})
	}()
}
