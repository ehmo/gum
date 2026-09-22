package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ehmo/gum/internal/cache"
	"github.com/spf13/cobra"
)

// newCacheCmd implements `gum cache stats|clear`. Phase 9 surfaces a minimal
// placeholder payload; live wiring lands when the dispatcher exposes cache
// stats publicly (v0.2.0).
func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect or clear the dispatcher response cache",
	}
	parentHelpOnly(cmd)
	cmd.AddCommand(
		newCacheStatsCmd(),
		newCacheClearCmd(),
		newCacheMigrateCmd(),
	)
	return cmd
}

// newCacheMigrateCmd runs the spec §10.2 BoltDB→WAL-SQLite migration.
// Resolves the per-profile cache directory the same way newCacheClearCmd
// does, then delegates to cache.Migrate. Warnings flow to stderr so
// stdout remains a clean JSON envelope.
func newCacheMigrateCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate BoltDB cache (http.db) to WAL-SQLite (http-wal.db)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			profileDir, err := cacheProfileDir(cmd)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(profileDir, 0o755); err != nil {
				return err
			}

			res, err := cache.Migrate(cache.MigrateOptions{
				CacheDir: profileDir,
				Force:    force,
			})
			if err != nil {
				if errors.Is(err, cache.ErrRsyncAmbiguity) {
					// The envelope is the machine-readable report, but the
					// migration did not run, so the command must still fail.
					// Returning nil here told a script the cache had been
					// migrated while http.db was still the live store.
					if werr := writeJSON(cmd.OutOrStdout(), map[string]any{
						"ok":    false,
						"error": "RSYNC_AMBIGUITY",
						"hint":  "rerun with --force to discard http-wal.db and restart migration from http.db",
					}); werr != nil {
						return werr
					}
					return errRendered{err}
				}
				return err
			}

			for _, w := range res.Warnings {
				_, _ = cmd.ErrOrStderr().Write([]byte("warning: " + w + "\n"))
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"ok":                true,
				"profile_dir":       profileDir,
				"bolt_existed":      res.BoltExisted,
				"wal_existed":       res.WALExisted,
				"sentinel_present":  res.SentinelPresent,
				"sentinel_written":  res.SentinelWritten,
				"entries_migrated":  res.EntriesMigrated,
				"sub_buckets_found": res.SubBucketsFound,
				"bak_renamed":       res.BakRenamed,
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Discard existing http-wal.db without sentinel and re-migrate from http.db")
	return cmd
}

func newCacheStatsCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Print dispatcher cache stats",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// One schema regardless of --format: the spec §3003 envelope
			// (review gum-oqer). Previously the bare invocation emitted a
			// different {version,hits,misses,...} placeholder than
			// --format=json, silently changing shape on scripts that added
			// the flag later.
			_ = format
			env := cacheStatsJSONEnvelope()
			if dir, err := cacheProfileDir(cmd); err == nil {
				usage := measureHTTPCacheDir(dir)
				http, _ := env["http"].(map[string]any)
				http["entries"] = usage.Entries
				http["bytes"] = usage.Bytes
			}
			return writeJSON(cmd.OutOrStdout(), env)
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "Output format (json)")
	return cmd
}

// measureHTTPCacheDir sizes the §10.2 store a profile holds. Hit and miss
// counts stay zero here on purpose: they are per-process, and this process has
// dispatched nothing. Entries and bytes are on disk, so they are reportable.
//
// The file is opened read-only-ish with a short lock timeout, because a
// running MCP server holds it and a stats call must not block behind one.
func measureHTTPCacheDir(dir string) cache.HTTPUsage {
	path := filepath.Join(dir, cache.HTTPCacheBoltFile)
	if _, err := os.Stat(path); err != nil {
		return cache.HTTPUsage{}
	}
	c, err := cache.Open(cache.BBoltConfig{Path: path, OpenTimeout: httpCacheOpenTimeout})
	if err != nil {
		return cache.HTTPUsage{}
	}
	defer func() { _ = c.Close() }()
	usage, err := cache.MeasureHTTP(cache.BoltHTTPStore{C: c})
	if err != nil {
		return cache.HTTPUsage{}
	}
	return usage
}

// cacheStatsJSONEnvelope returns a CacheStatsResult envelope matching spec §3003.
// The semantic and prompt counters are zero because live wiring lands in
// v0.2.0; the caller fills the §10.2 http entry and byte counts from disk.
func cacheStatsJSONEnvelope() map[string]any {
	return map[string]any{
		"semantic": map[string]any{
			"hits":      int64(0),
			"misses":    int64(0),
			"evictions": int64(0),
			"entries":   int64(0),
			"bytes":     int64(0),
		},
		"http": map[string]any{
			"hits":    int64(0),
			"misses":  int64(0),
			"entries": int64(0),
			"bytes":   int64(0),
		},
		"prompt": map[string]any{
			"supported":     false,
			"hits_estimate": nil,
		},
		"audit_broken": false,
	}
}

func newCacheClearCmd() *cobra.Command {
	var bakFlag bool
	var expiredFlag bool

	cmd := &cobra.Command{
		Use:   "clear [pattern]",
		Short: "Clear the dispatcher response cache",
		Long: "Clear the dispatcher response cache.\n\n" +
			"With no flags this removes stored HTTP/ETag validators. Those entries have\n" +
			"no TTL and nothing evicts them, so this is the only way to reclaim the\n" +
			"store or to force a read to be re-shaped under a changed output profile.\n\n" +
			"pattern is a glob matched against op_id: `gum cache clear \"gmail.*\"` drops\n" +
			"one API and `gum cache clear gmail.users.messages.list` drops one op. With\n" +
			"no pattern the whole store is cleared.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileDir, err := cacheProfileDir(cmd)
			if err != nil {
				return err
			}

			result := map[string]any{}

			if !bakFlag && !expiredFlag {
				// §10.2 entries have no TTL and nothing evicts them, so this
				// is the only path that reclaims the store. A bare invocation
				// clears all of it; a pattern clears the ops it names.
				pattern := ""
				if len(args) == 1 {
					pattern = args[0]
				}
				cleared, cerr := clearHTTPCacheDir(profileDir, pattern)
				if cerr != nil {
					return cerr
				}
				result["cleared"] = true
				result["http_entries_removed"] = cleared
				result["pattern"] = pattern
				result["note"] = "cleared the §10.2 HTTP/ETag store; the §10.3 semantic cache is process-local"
				return writeJSON(cmd.OutOrStdout(), result)
			}

			if bakFlag {
				bakPath := filepath.Join(profileDir, "http.db.bak")
				removed := false
				if _, err := os.Stat(bakPath); err == nil {
					if err := os.Remove(bakPath); err != nil {
						return err
					}
					removed = true
				} else if !errors.Is(err, os.ErrNotExist) {
					return err
				}
				result["removed_bak"] = removed
				result["path"] = bakPath
			}

			if expiredFlag {
				cachePath := filepath.Join(profileDir, "cache.db")
				if _, err := os.Stat(cachePath); errors.Is(err, os.ErrNotExist) {
					result["expired_removed"] = 0
				} else {
					c, err := cache.Open(cache.BBoltConfig{Path: cachePath})
					if err != nil {
						return err
					}
					count, evictErr := c.EvictExpired()
					_ = c.Close()
					if evictErr != nil {
						return evictErr
					}
					result["expired_removed"] = count
				}
			}

			return writeJSON(cmd.OutOrStdout(), result)
		},
	}
	cmd.Flags().BoolVar(&bakFlag, "bak", false, "Remove http.db.bak backup file")
	cmd.Flags().BoolVar(&expiredFlag, "expired", false, "Evict TTL-expired cache entries")
	return cmd
}

// clearHTTPCacheDir removes §10.2 entries from one profile's store. An absent
// file is not an error: nothing was cached, so nothing needs clearing.
func clearHTTPCacheDir(dir, pattern string) (int, error) {
	path := filepath.Join(dir, cache.HTTPCacheBoltFile)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	c, err := cache.Open(cache.BBoltConfig{Path: path, OpenTimeout: httpCacheOpenTimeout})
	if err != nil {
		if errors.Is(err, cache.ErrCacheLocked) {
			// Another gum process holds the file. Deleting rows under it
			// would leave that process serving a hot tier the disk no longer
			// backs, so say so instead.
			return 0, fmt.Errorf("cache: %s is in use by another gum process; stop it and retry", path)
		}
		return 0, err
	}
	defer func() { _ = c.Close() }()
	return cache.ClearHTTP(cache.BoltHTTPStore{C: c}, pattern)
}

func cacheProfileDir(cmd *cobra.Command) (string, error) {
	name, err := resolveProfileName(cmd)
	if err != nil {
		return "", err
	}
	return name.CacheDir()
}
