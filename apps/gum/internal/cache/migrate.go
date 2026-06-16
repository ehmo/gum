// Package cache — BoltDB → WAL-SQLite migration (spec §10.2 lines 2234-2241).
//
// The migration is idempotent. Repeated invocations after a successful run
// observe the sentinel row in http-wal.db and exit early. Mid-migration
// crashes are detected and rolled back automatically on the next run.

package cache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

// HTTPCacheBoltFile is the stdio-mode HTTP cache filename. Listed alongside
// HTTPWALDBFile so callers can compose absolute paths from a profile dir.
const HTTPCacheBoltFile = "http.db"

// HTTPCacheBoltBakFile is the post-migration backup name (`http.db.bak`).
// `gum cache clear --bak` removes it; the migration tool produces it.
const HTTPCacheBoltBakFile = "http.db.bak"

// ErrRsyncAmbiguity fires when both http.db and http-wal.db are present
// but the sentinel row is absent in the SQLite file (spec §10.2 rsync
// ambiguity paragraph). The operator must rerun with Force=true to
// override.
var ErrRsyncAmbiguity = errors.New("cache: both http.db and http-wal.db exist without sentinel; rerun with --force")

// MigrateOptions controls the migration. Path resolution lives in the
// caller (CLI) so tests can hand the migrator absolute tempdir paths.
type MigrateOptions struct {
	// CacheDir is the per-profile cache directory, typically
	// `<XDG_CACHE_HOME>/gum/<profile>`. Migration paths are derived as
	// `<CacheDir>/http.db`, `<CacheDir>/http-wal.db`, `<CacheDir>/http.db.bak`.
	CacheDir string
	// Force overrides the rsync-ambiguity gate. When true, an existing
	// http-wal.db without the sentinel is deleted and migration restarts
	// from the BoltDB source.
	Force bool
}

// MigrateResult reports what the migration tool did. The CLI surfaces these
// fields in its JSON envelope so operators can audit the action.
type MigrateResult struct {
	// BoltExisted is true when http.db was found at migration start.
	BoltExisted bool `json:"bolt_existed"`
	// WALExisted is true when http-wal.db was found at migration start.
	WALExisted bool `json:"wal_existed"`
	// SentinelPresent reports whether the SQLite sentinel row was found
	// before any work was done. When true, the migration is a no-op.
	SentinelPresent bool `json:"sentinel_present_pre"`
	// SentinelWritten is true when the migration wrote (or re-wrote) the
	// sentinel row. Always true on a successful run.
	SentinelWritten bool `json:"sentinel_written"`
	// EntriesMigrated counts top-level + sub-bucket BoltDB keys copied to
	// SQLite. Zero on a no-op or a clean-bootstrap (no BoltDB present).
	EntriesMigrated int `json:"entries_migrated"`
	// BakRenamed is true when http.db was renamed to http.db.bak.
	BakRenamed bool `json:"bak_renamed"`
	// SubBucketsFound is the count of nested buckets seen in BoltDB. The
	// spec mandates the tool warn (but proceed) since v0.1.0 doesn't
	// create them.
	SubBucketsFound int `json:"sub_buckets_found"`
	// Warnings is a free-form slice; the CLI emits them on stderr so
	// scripts piping stdout JSON aren't disturbed.
	Warnings []string `json:"warnings,omitempty"`
}

// Migrate runs the spec §10.2 idempotent migration sequence. The 4 spec
// branches:
//
//  1. http-wal.db exists + sentinel present → no-op; rename leftover http.db.
//  2. http-wal.db exists + sentinel absent (mid-migration crash) → delete
//     http-wal.db, proceed to step 3.
//  3. http.db exists → copy rows into a fresh http-wal.db, write sentinel,
//     rename to http.db.bak.
//  4. Neither exists → create empty http-wal.db with sentinel.
//
// Returns MigrateResult on success, ErrRsyncAmbiguity when both files are
// present and the sentinel is absent (caller must rerun with Force=true).
func Migrate(opts MigrateOptions) (*MigrateResult, error) {
	if opts.CacheDir == "" {
		return nil, errors.New("cache: MigrateOptions.CacheDir is required")
	}
	boltPath := filepath.Join(opts.CacheDir, HTTPCacheBoltFile)
	walPath := filepath.Join(opts.CacheDir, HTTPWALDBFile)
	bakPath := filepath.Join(opts.CacheDir, HTTPCacheBoltBakFile)

	res := &MigrateResult{}
	boltExists := fileExists(boltPath)
	walExists := fileExists(walPath)
	res.BoltExisted = boltExists
	res.WALExisted = walExists

	// Branch 1 / 2: http-wal.db is present.
	if walExists {
		s, err := OpenSQLiteWAL(SQLiteConfig{Path: walPath})
		if err != nil {
			return nil, err
		}
		ok, sErr := s.SentinelPresent()
		if sErr != nil {
			_ = s.Close()
			return nil, sErr
		}
		res.SentinelPresent = ok
		if ok {
			// Branch 1: sentinel present. Trust http-wal.db. Rename any
			// leftover http.db to .bak so subsequent runs converge.
			if err := s.Close(); err != nil {
				return nil, fmt.Errorf("cache: close wal after sentinel check: %w", err)
			}
			if boltExists {
				if err := os.Rename(boltPath, bakPath); err != nil {
					return nil, fmt.Errorf("cache: rename leftover bolt: %w", err)
				}
				res.BakRenamed = true
			}
			res.SentinelWritten = false
			return res, nil
		}
		// Branch 2: sentinel absent.
		if err := s.Close(); err != nil {
			return nil, fmt.Errorf("cache: close wal before recovery: %w", err)
		}
		if boltExists && !opts.Force {
			return nil, ErrRsyncAmbiguity
		}
		if err := os.Remove(walPath); err != nil {
			return nil, fmt.Errorf("cache: delete mid-migration wal: %w", err)
		}
		res.WALExisted = true // preserve the "we saw it" signal in the result
	}

	// Branch 4: neither file exists → fresh bootstrap.
	if !boltExists {
		s, err := OpenSQLiteWAL(SQLiteConfig{Path: walPath})
		if err != nil {
			return nil, err
		}
		if err := s.WriteSentinel(); err != nil {
			_ = s.Close()
			return nil, err
		}
		if err := s.Close(); err != nil {
			return nil, fmt.Errorf("cache: close fresh wal: %w", err)
		}
		res.SentinelWritten = true
		return res, nil
	}

	// Branch 3: bolt exists, wal does not.
	migrated, subBuckets, warnings, err := copyBoltToSQLite(boltPath, walPath)
	if err != nil {
		// Best-effort cleanup: a partial wal file would re-trigger branch 2
		// on the next run, which is exactly the spec's recovery path.
		_ = os.Remove(walPath)
		return nil, err
	}
	res.EntriesMigrated = migrated
	res.SubBucketsFound = subBuckets
	res.SentinelWritten = true
	res.Warnings = warnings

	if err := os.Rename(boltPath, bakPath); err != nil {
		return nil, fmt.Errorf("cache: rename bolt to bak: %w", err)
	}
	res.BakRenamed = true
	return res, nil
}

// copyBoltToSQLite opens the BoltDB at boltPath, copies every key (including
// sub-bucket keys, prefixed with the parent bucket name) into a fresh SQLite
// file at walPath, writes the sentinel row as the final operation, and
// returns the count of rows + sub-buckets observed. The sentinel write
// closes the migration transaction; a crash before it leaves the file
// without the sentinel, which branch 2 cleans up.
func copyBoltToSQLite(boltPath, walPath string) (int, int, []string, error) {
	db, err := bolt.Open(boltPath, 0o600, nil)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("cache: open bolt for migration: %w", err)
	}

	s, err := OpenSQLiteWAL(SQLiteConfig{Path: walPath})
	if err != nil {
		_ = db.Close()
		return 0, 0, nil, err
	}

	var migrated, subBuckets int
	var warnings []string

	err = db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(bucketName []byte, b *bolt.Bucket) error {
			if err := b.ForEach(func(k, v []byte) error {
				if v == nil {
					// Nested bucket; record it and recurse one level deep.
					subBuckets++
					sub := b.Bucket(k)
					if sub == nil {
						return nil
					}
					prefix := string(bucketName) + "/" + string(k) + "/"
					return sub.ForEach(func(sk, sv []byte) error {
						if sv == nil {
							subBuckets++
							return nil
						}
						if err := s.Set(prefix+string(sk), sv, 0); err != nil {
							return err
						}
						migrated++
						return nil
					})
				}
				if err := s.Set(string(bucketName)+"/"+string(k), v, 0); err != nil {
					return err
				}
				migrated++
				return nil
			}); err != nil {
				return err
			}
			return nil
		})
	})
	if err != nil {
		_ = s.Close()
		_ = db.Close()
		return 0, 0, nil, fmt.Errorf("cache: migrate bolt entries: %w", err)
	}

	if subBuckets > 0 {
		warnings = append(warnings, fmt.Sprintf("found %d nested bbolt sub-buckets; v0.1.0 does not create these — migrated with bucket-name prefix", subBuckets))
	}

	if err := s.WriteSentinel(); err != nil {
		_ = s.Close()
		_ = db.Close()
		return 0, 0, nil, err
	}
	if err := s.Close(); err != nil {
		_ = db.Close()
		return 0, 0, nil, fmt.Errorf("cache: close migrated wal: %w", err)
	}
	if err := db.Close(); err != nil {
		return 0, 0, nil, fmt.Errorf("cache: close migrated bolt: %w", err)
	}
	return migrated, subBuckets, warnings, nil
}

// fileExists is a thin wrapper around os.Stat for readability at the
// migration-branch decision points.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
