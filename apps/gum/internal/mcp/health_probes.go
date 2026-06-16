package mcp

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// healthSnapshotTTL is the §13 line 3149 "5s sample TTL" constant. The probe
// layer caches snapshot results for this window so repeated resources/read
// calls do not re-stat the filesystem.
const healthSnapshotTTL = 5 * time.Second

// subsystemHealth is one row of the gum://status/health response prior to
// TOON encoding. Status is from the closed enum {"healthy","degraded"};
// "down" is reserved for future live probes that detect a hard failure.
type subsystemHealth struct {
	Subsystem   string
	Status      string
	Detail      string
	LastCheckAt time.Time
}

// healthProbe returns a one-row health observation for one subsystem. The
// probeProfileDir argument is the active profile directory (may be empty if
// the profile dir cannot be resolved — probes MUST tolerate that and report a
// safe default rather than crashing).
type healthProbe func(now time.Time, profileDir string) subsystemHealth

// healthProbes maps each subsystem in the closed enum (spec §13 line 3149) to
// its cheap local probe. Probes MUST NOT make upstream network calls per the
// same spec line. Adding or removing a subsystem requires a minor-version
// spec PR and a matching update to staticHealthSubsystems.
var healthProbes = map[string]healthProbe{
	"audit_log":      probeAuditLog,
	"cache_sqlite":   probeCacheSQLite,
	"canary_runner":  probeCanaryRunner,
	"gain_ledger":    probeGainLedger,
	"keychain":       probeKeychain,
	"tee_filesystem": probeTeeFilesystem,
}

// healthSnapshotCache memoises the full subsystem table for healthSnapshotTTL.
// The mutex is intentionally coarse-grained: snapshots are cheap and the
// resource read path is not hot enough to justify per-subsystem locking.
type healthSnapshotCache struct {
	mu         sync.Mutex
	rows       []subsystemHealth
	computedAt time.Time
}

// snapshot returns the cached health rows when the cache is still warm,
// otherwise it rebuilds the table by invoking every probe.
func (c *healthSnapshotCache) snapshot(now time.Time, profileDir string) []subsystemHealth {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.computedAt.IsZero() && now.Sub(c.computedAt) < healthSnapshotTTL {
		return append([]subsystemHealth(nil), c.rows...)
	}
	rows := make([]subsystemHealth, 0, len(healthProbes))
	for _, name := range staticHealthSubsystems {
		probe, ok := healthProbes[name]
		if !ok {
			rows = append(rows, subsystemHealth{
				Subsystem:   name,
				Status:      "degraded",
				Detail:      "probe not registered",
				LastCheckAt: now,
			})
			continue
		}
		rows = append(rows, probe(now, profileDir))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Subsystem < rows[j].Subsystem })
	c.rows = rows
	c.computedAt = now
	return append([]subsystemHealth(nil), rows...)
}

// probeAuditLog reports whether the per-profile audit sink has a usable
// filesystem target and no active audit.broken sentinel.
func probeAuditLog(now time.Time, profileDir string) subsystemHealth {
	if profileDir == "" {
		return subsystemHealth{
			Subsystem:   "audit_log",
			Status:      "degraded",
			Detail:      "audit dir unresolvable",
			LastCheckAt: now,
		}
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return subsystemHealth{
			Subsystem:   "audit_log",
			Status:      "degraded",
			Detail:      "cannot create audit dir: " + err.Error(),
			LastCheckAt: now,
		}
	}
	sentinel := filepath.Join(profileDir, "audit.broken")
	if _, err := os.Stat(sentinel); err == nil {
		return subsystemHealth{
			Subsystem:   "audit_log",
			Status:      "degraded",
			Detail:      "audit sink previously failed: " + readHealthAuditBrokenHint(sentinel),
			LastCheckAt: now,
		}
	} else if !os.IsNotExist(err) {
		return subsystemHealth{
			Subsystem:   "audit_log",
			Status:      "degraded",
			Detail:      "cannot inspect audit.broken: " + err.Error(),
			LastCheckAt: now,
		}
	}
	probe := filepath.Join(profileDir, ".audit-health-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return subsystemHealth{
			Subsystem:   "audit_log",
			Status:      "degraded",
			Detail:      "audit dir not writable: " + err.Error(),
			LastCheckAt: now,
		}
	}
	_ = os.Remove(probe)
	return subsystemHealth{
		Subsystem:   "audit_log",
		Status:      "healthy",
		Detail:      "audit sink writable",
		LastCheckAt: now,
	}
}

func readHealthAuditBrokenHint(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return err.Error()
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return err.Error()
	}
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		return path
	}
	return msg
}

// probeCacheSQLite probes the cache backing store. v0.1.0 ships bbolt
// (~/.cache/gum/cache.db); SQLite migration is gum-9qn. The probe is
// considered healthy when the parent directory exists or can be created
// — an absent cache.db is interpreted as "no cached responses yet", which
// is the normal fresh-install state.
func probeCacheSQLite(now time.Time, _ string) subsystemHealth {
	cacheDir, err := cacheRootDir()
	if err != nil {
		return subsystemHealth{
			Subsystem:   "cache_sqlite",
			Status:      "degraded",
			Detail:      "cache dir unresolvable: " + err.Error(),
			LastCheckAt: now,
		}
	}
	if info, err := os.Stat(cacheDir); err == nil && info.IsDir() {
		return subsystemHealth{
			Subsystem:   "cache_sqlite",
			Status:      "healthy",
			Detail:      "bbolt cache.db (sqlite migration v0.2.0)",
			LastCheckAt: now,
		}
	}
	// Parent dir does not yet exist — fresh install. Healthy.
	return subsystemHealth{
		Subsystem:   "cache_sqlite",
		Status:      "healthy",
		Detail:      "no cache initialized",
		LastCheckAt: now,
	}
}

// probeCanaryRunner reports on the §8.5 passive canary runner. v0.1.0 keeps
// the runner in-process with zero registered canaries; the resource shows
// this as healthy with an explanatory detail so operators do not mistake
// the empty roster for a failure.
func probeCanaryRunner(now time.Time, _ string) subsystemHealth {
	return subsystemHealth{
		Subsystem:   "canary_runner",
		Status:      "healthy",
		Detail:      "no canaries registered (v0.1.0)",
		LastCheckAt: now,
	}
}

// probeGainLedger probes the §9.5 gain ledger. The ledger is created lazily
// on the first append, so an absent file is normal on a fresh install. The
// probe reports degraded only when the parent dir cannot be resolved.
func probeGainLedger(now time.Time, _ string) subsystemHealth {
	home, err := os.UserHomeDir()
	if err != nil {
		return subsystemHealth{
			Subsystem:   "gain_ledger",
			Status:      "degraded",
			Detail:      "home dir unresolvable: " + err.Error(),
			LastCheckAt: now,
		}
	}
	ledger := filepath.Join(home, ".local", "share", "gum", "gain-ledger.jsonl")
	if info, err := os.Stat(ledger); err == nil && !info.IsDir() {
		return subsystemHealth{
			Subsystem:   "gain_ledger",
			Status:      "healthy",
			Detail:      "ledger present",
			LastCheckAt: now,
		}
	}
	return subsystemHealth{
		Subsystem:   "gain_ledger",
		Status:      "healthy",
		Detail:      "no entries yet",
		LastCheckAt: now,
	}
}

// probeKeychain reports on the §7 keychain integration. Calling into the OS
// keychain (Security framework on macOS, libsecret on Linux) is expensive
// and can prompt the user, so v0.1.0 returns healthy unconditionally —
// live keychain reachability is deferred. The detail field calls this out
// so operators are not misled.
func probeKeychain(now time.Time, _ string) subsystemHealth {
	return subsystemHealth{
		Subsystem:   "keychain",
		Status:      "healthy",
		Detail:      "OS keychain probe deferred (v0.2.0)",
		LastCheckAt: now,
	}
}

// probeTeeFilesystem probes the tee artifact tree under the active profile
// directory. An absent <profileDir>/tee subtree is normal on a fresh
// install. The probe reports degraded only when the profile dir itself
// cannot be resolved.
func probeTeeFilesystem(now time.Time, profileDir string) subsystemHealth {
	if profileDir == "" {
		return subsystemHealth{
			Subsystem:   "tee_filesystem",
			Status:      "degraded",
			Detail:      "profile dir unresolvable",
			LastCheckAt: now,
		}
	}
	teeDir := filepath.Join(profileDir, "tee")
	if info, err := os.Stat(teeDir); err == nil && info.IsDir() {
		return subsystemHealth{
			Subsystem:   "tee_filesystem",
			Status:      "healthy",
			Detail:      "tee tree present",
			LastCheckAt: now,
		}
	}
	return subsystemHealth{
		Subsystem:   "tee_filesystem",
		Status:      "healthy",
		Detail:      "no tee artifacts yet",
		LastCheckAt: now,
	}
}

// cacheRootDir resolves ~/.cache/gum (honouring XDG_CACHE_HOME) — the parent
// of the bbolt cache.db file. Returned without creating the directory; the
// caller is the cache package itself.
func cacheRootDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "gum"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "gum"), nil
}
