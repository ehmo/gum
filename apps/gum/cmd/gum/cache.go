package main

import (
	"errors"
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
					return writeJSON(cmd.OutOrStdout(), map[string]any{
						"ok":    false,
						"error": "RSYNC_AMBIGUITY",
						"hint":  "rerun with --force to discard http-wal.db and restart migration from http.db",
					})
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
			return writeJSON(cmd.OutOrStdout(), cacheStatsJSONEnvelope())
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "Output format (json)")
	return cmd
}

// cacheStatsJSONEnvelope returns a CacheStatsResult envelope matching spec §3003.
// All counters are zero because live wiring lands in v0.2.0.
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
		Use:   "clear",
		Short: "Clear the dispatcher response cache",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !bakFlag && !expiredFlag {
				// v0.1.0 placeholder — preserved for backward compat.
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"cleared": true,
					"note":    "the v0.1.0 cache is process-local; clearing is a no-op",
				})
			}

			profileDir, err := cacheProfileDir(cmd)
			if err != nil {
				return err
			}

			result := map[string]any{}

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
					count := c.EvictExpired()
					_ = c.Close()
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

func cacheProfileDir(cmd *cobra.Command) (string, error) {
	name, err := resolveProfileName(cmd)
	if err != nil {
		return "", err
	}
	return name.CacheDir()
}
