// Package cache — WAL-enabled SQLite backend for HTTP-mode MCP (spec §10.2).
//
// The stdio backend (BBoltCache in bbolt.go) is a single-writer file that
// breaks under concurrent processes. HTTP-mode MCP allows multiple gum
// processes to share a cache directory (via symlinks or explicit pointing),
// so the spec mandates a WAL-enabled SQLite file at `http-wal.db` that
// supports concurrent readers + a single writer behind a busy-timeout retry.
//
// This file is the **storage** half; migration logic lives in migrate.go.

package cache

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (spec §10.2)
)

// HTTPWALDBFile is the spec §10.2 filename for the WAL-SQLite HTTP cache.
const HTTPWALDBFile = "http-wal.db"

// SentinelKey is the migration-complete sentinel row spec §10.2 step 1
// inserts as the final write of a successful BoltDB→SQLite migration.
const SentinelKey = "__migration_complete__"

// SentinelValue is the spec-mandated payload for the sentinel row: the
// migration version pair. Future-proofs against multi-hop migrations.
const SentinelValue = "v0.1.0->v0.4.0"

// SQLiteWALCache is the spec §10.2 WAL-SQLite backend. Schema is a single
// key→value table with optional TTL; the sentinel row uses the same table.
// Concurrent access safety comes from the WAL journal_mode + busy_timeout
// pragmas baked into the DSN.
type SQLiteWALCache struct {
	db   *sql.DB
	path string
}

// SQLiteConfig configures OpenSQLiteWAL. Path defaults to
// `<XDG_CACHE_HOME>/gum/default/http-wal.db` when empty; that fallback is
// reserved for tests — production callers always pass the resolved profile
// path.
type SQLiteConfig struct {
	// Path is the absolute path to the SQLite file. Required for production
	// callers; tests may leave empty to default to a deterministic location.
	Path string
	// BusyTimeoutMS overrides the spec-default 5000ms busy_timeout pragma.
	// Zero falls back to the spec value.
	BusyTimeoutMS int
}

// ErrSQLiteCorrupt is returned by OpenSQLiteWAL when the file exists but
// cannot be opened as a SQLite database. The caller's recovery is to invoke
// `gum cache migrate --force`, which deletes the file and re-derives from
// BoltDB.
var ErrSQLiteCorrupt = errors.New("cache: sqlite file corrupt or not a valid database")

// OpenSQLiteWAL creates or opens the http-wal.db SQLite database. The DSN
// pre-bakes the spec §10.2 pragmas (journal_mode=wal, busy_timeout=5000) so
// every connection inherits them from the first open. The kv table is
// idempotently created.
func OpenSQLiteWAL(cfg SQLiteConfig) (*SQLiteWALCache, error) {
	if cfg.Path == "" {
		return nil, errors.New("cache: SQLiteConfig.Path is required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o700); err != nil {
		return nil, fmt.Errorf("cache: create cache dir: %w", err)
	}
	busy := cfg.BusyTimeoutMS
	if busy == 0 {
		busy = 5000
	}
	q := url.Values{}
	q.Set("_pragma", fmt.Sprintf("busy_timeout(%d)", busy))
	q.Add("_pragma", "journal_mode(wal)")
	dsn := "file:" + cfg.Path + "?" + q.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSQLiteCorrupt, err)
	}
	// Bound the connection pool to mirror the spec's "single writer + many
	// readers" guarantee: SetMaxOpenConns(1) for writes is too restrictive
	// (kills read concurrency); the WAL pragma plus busy_timeout handles
	// writer contention naturally, so we let database/sql manage the pool.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%w: %v", ErrSQLiteCorrupt, err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS kv (
			key TEXT PRIMARY KEY,
			value BLOB NOT NULL,
			expires_at_unix INTEGER NOT NULL DEFAULT 0,
			ts_unix INTEGER NOT NULL
		)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cache: create kv table: %w", err)
	}
	return &SQLiteWALCache{db: db, path: cfg.Path}, nil
}

// Close releases the database handle.
func (s *SQLiteWALCache) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path returns the absolute path to the SQLite file. Used by callers
// composing additional paths (e.g. http.db.bak alongside http-wal.db).
func (s *SQLiteWALCache) Path() string { return s.path }

// Set writes a value for key with optional TTL. A zero ttl means "no
// expiry". Existing rows are replaced.
func (s *SQLiteWALCache) Set(key string, value []byte, ttl time.Duration) error {
	now := time.Now().Unix()
	var expires int64
	if ttl > 0 {
		expires = time.Now().Add(ttl).Unix()
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO kv(key, value, expires_at_unix, ts_unix) VALUES (?, ?, ?, ?)`,
		key, value, expires, now,
	)
	if err != nil {
		return fmt.Errorf("cache: sqlite set: %w", err)
	}
	return nil
}

// Get returns the value and true when present and unexpired. A miss
// (absent or TTL elapsed) returns (nil, false, nil). Errors only fire on
// I/O failure.
func (s *SQLiteWALCache) Get(key string) ([]byte, bool, error) {
	row := s.db.QueryRow(`SELECT value, expires_at_unix FROM kv WHERE key = ?`, key)
	var value []byte
	var expires int64
	if err := row.Scan(&value, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("cache: sqlite get: %w", err)
	}
	if expires != 0 && expires <= time.Now().Unix() {
		return nil, false, nil
	}
	return value, true, nil
}

// SentinelPresent reports whether the migration-complete sentinel row is
// present. Spec §10.2 step 1: presence means http-wal.db is authoritative;
// absence (with a present file) signals a mid-migration crash and demands
// either deletion (--force) or operator intervention.
func (s *SQLiteWALCache) SentinelPresent() (bool, error) {
	row := s.db.QueryRow(`SELECT 1 FROM kv WHERE key = ?`, SentinelKey)
	var one int
	if err := row.Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("cache: sentinel check: %w", err)
	}
	return true, nil
}

// WriteSentinel inserts the spec §10.2 migration-complete sentinel as the
// final write of a migration transaction. Idempotent: repeated calls
// re-insert with a refreshed ts_unix but the value stays SentinelValue.
func (s *SQLiteWALCache) WriteSentinel() error {
	return s.Set(SentinelKey, []byte(SentinelValue), 0)
}
