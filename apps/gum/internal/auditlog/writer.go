// Package auditlog owns the on-disk audit-log writer for `~/.local/share/gum/
// <profile>/audit.jsonl` (spec §11). It satisfies the dispatch.auditSink
// interface (`Append(entry map[string]any)`) so the dispatch kernel does not
// depend on a filesystem implementation.
//
// Scope (gum-gv9a): adds time-based rotation, 10 GB hard ceiling,
// audit.unbounded override, cross-process advisory file lock for the
// rotation critical section, ENOENT retry for mid-rotation appends, and
// startup mid-rotation crash recovery.
//
// Scope (gum-dxpy): adds an opt-in buffered-channel writer goroutine plus a
// Close() method that drains pending entries with a configurable timeout
// (audit.drain_timeout_seconds, 0-30, default 2). Used by cmd/gum's signal
// handler to flush queued entries on SIGTERM/SIGINT. The synchronous
// Append path is preserved (channel is nil) so existing tests and embedded
// callers keep their immediate-persist semantics.
package auditlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ehmo/gum/internal/fsatomic"
)

// DefaultMaxSizeBytes is the spec §11 default for `audit.max_size_mb` (100 MB).
const DefaultMaxSizeBytes int64 = 100 * 1024 * 1024

// DefaultMaxFiles is the spec §11 default for `audit.max_files`.
const DefaultMaxFiles = 5

// DefaultRetentionDays is the spec §11 default for `audit.retention_days`.
const DefaultRetentionDays = 90

// HardCeilingBytes is the spec §11 normative 10 GB cap. When unbounded=false
// and a write would push audit.jsonl past this value, an emergency rotation
// fires regardless of `audit.max_size_mb=0`.
const HardCeilingBytes int64 = 10 * 1024 * 1024 * 1024

// rotationLockTimeout is the spec §11 advisory-lock wait (2 seconds). The
// timeout skip applies only to normal threshold rotation; hard-ceiling
// rotation blocks until the lock is acquired.
const rotationLockTimeout = 2 * time.Second

// hardCeilingLockTimeout bounds the worst-case hard-ceiling wait. The spec
// allows "block until rotation succeeds or return an audit append failure";
// we choose a longer timeout (1 minute) before returning failure so the
// dispatch hot path never wedges indefinitely.
const hardCeilingLockTimeout = 60 * time.Second

// DefaultDrainTimeout is the spec §11 default for audit.drain_timeout_seconds
// (2 seconds). Used by Close() when the buffered-channel path is enabled and
// no override is configured.
const DefaultDrainTimeout = 2 * time.Second

// MaxDrainTimeout is the spec §11 upper bound on audit.drain_timeout_seconds
// (30 seconds). WithDrainTimeout silently clamps to this ceiling.
const MaxDrainTimeout = 30 * time.Second

// SchemaVersion is the audit-log schema version stamped as the first `v` key
// in every entry (spec §11 audit-log schema-version paragraph).
const SchemaVersion = 1

// errLockTimeout is returned by acquireRotationLock when another process
// holds the lock longer than rotationLockTimeout. Callers distinguish "skip
// normal rotation, proceed with append" (this error) from real I/O errors.
var errLockTimeout = errors.New("auditlog: rotation lock timeout")

// Writer appends audit entries to a per-profile audit.jsonl. Append is safe
// for concurrent use within a single process via a mutex; cross-process
// rotation safety is provided by an advisory file lock on `audit.jsonl.lock`
// (spec §11).
//
// When constructed with WithBufferedChannel(n) (n > 0), Append enqueues to
// an n-deep buffered channel drained by a background goroutine; the caller's
// Append returns as soon as the entry is queued (or counted dropped if the
// channel is full). Close() then drains the channel with the configured
// drain timeout and emits one audit_drain_complete event with drained/
// dropped counts. Without WithBufferedChannel, Append remains synchronous.
type Writer struct {
	dir           string
	path          string
	lockPath      string
	sentinelPath  string
	maxSize       int64
	maxFiles      int
	retentionDays int
	hardCeiling   int64
	unbounded     bool

	mu           sync.Mutex
	now          func() time.Time
	creationTime time.Time // when the live audit.jsonl was created (best effort)

	// Buffered-channel async path (gum-dxpy). All nil/zero when
	// WithBufferedChannel was not set; in that case Append is synchronous.
	ch           chan map[string]any
	stopCh       chan struct{}
	drainTimeout time.Duration
	workerWG     sync.WaitGroup
	closed       atomic.Bool
	drainedCount atomic.Uint64
	droppedCount atomic.Uint64
}

// Option configures a Writer.
type Option func(*Writer)

// WithMaxSizeBytes overrides the per-file rotation threshold (default
// DefaultMaxSizeBytes). A value of 0 disables size-based rotation.
func WithMaxSizeBytes(n int64) Option { return func(w *Writer) { w.maxSize = n } }

// WithMaxFiles overrides the rotated-archive retention count (default
// DefaultMaxFiles). A value of 0 retains zero archives (oldest deleted on
// every rotation).
func WithMaxFiles(n int) Option { return func(w *Writer) { w.maxFiles = n } }

// WithClock injects a deterministic time source for tests. nil resets to
// time.Now.
func WithClock(now func() time.Time) Option {
	return func(w *Writer) {
		if now == nil {
			w.now = time.Now
		} else {
			w.now = now
		}
	}
}

// WithRetentionDays overrides the per-file age threshold (default
// DefaultRetentionDays). 0 disables time-based rotation per spec §11.
func WithRetentionDays(n int) Option { return func(w *Writer) { w.retentionDays = n } }

// WithUnbounded toggles the spec §11 `audit.unbounded` switch. When true the
// 10 GB hard ceiling is lifted; configured size/age rotation thresholds still
// apply per spec.
func WithUnbounded(b bool) Option { return func(w *Writer) { w.unbounded = b } }

// WithHardCeilingBytes overrides the spec §11 10 GB hard ceiling. Tests use
// small values; production code MUST NOT call this option.
func WithHardCeilingBytes(n int64) Option { return func(w *Writer) { w.hardCeiling = n } }

// WithBufferedChannel enables the gum-dxpy async writer path with a queue of
// the given depth. n <= 0 leaves the writer synchronous (default). When
// enabled, Append enqueues to the channel; a background goroutine drains
// into the on-disk append loop; Close() drains pending entries with the
// drain timeout and emits audit_drain_complete.
func WithBufferedChannel(n int) Option {
	return func(w *Writer) {
		if n <= 0 {
			return
		}
		w.ch = make(chan map[string]any, n)
		w.stopCh = make(chan struct{})
	}
}

// WithDrainTimeout overrides the spec §11 audit.drain_timeout_seconds knob
// used by Close() when the buffered-channel path is enabled. Negative values
// are treated as DefaultDrainTimeout (2s); values above MaxDrainTimeout (30s)
// are clamped down. Ignored when WithBufferedChannel was not set.
func WithDrainTimeout(d time.Duration) Option {
	return func(w *Writer) {
		switch {
		case d < 0:
			w.drainTimeout = DefaultDrainTimeout
		case d > MaxDrainTimeout:
			w.drainTimeout = MaxDrainTimeout
		default:
			w.drainTimeout = d
		}
	}
}

// New constructs a Writer rooted at profileDir/audit.jsonl with the §11
// defaults. profileDir is created (mode 700) if it does not already exist.
// If audit.jsonl is missing but audit.jsonl.lock is present (mid-rotation
// crash from a prior process), the live file is recreated under the lock
// per spec §11 "mid-rotation crash recovery".
func New(profileDir string, opts ...Option) (*Writer, error) {
	if profileDir == "" {
		return nil, errors.New("auditlog: empty profileDir")
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return nil, fmt.Errorf("auditlog: mkdir %q: %w", profileDir, err)
	}
	w := &Writer{
		dir:           profileDir,
		path:          filepath.Join(profileDir, "audit.jsonl"),
		lockPath:      filepath.Join(profileDir, "audit.jsonl.lock"),
		sentinelPath:  filepath.Join(profileDir, "audit.broken"),
		maxSize:       DefaultMaxSizeBytes,
		maxFiles:      DefaultMaxFiles,
		retentionDays: DefaultRetentionDays,
		hardCeiling:   HardCeilingBytes,
		now:           time.Now,
	}
	for _, o := range opts {
		o(w)
	}
	w.recoverMidRotation()
	w.initCreationTime()
	if w.ch != nil {
		if w.drainTimeout == 0 {
			w.drainTimeout = DefaultDrainTimeout
		}
		w.workerWG.Add(1)
		go w.workerLoop()
	}
	return w, nil
}

// workerLoop drains the buffered channel into syncAppend. It exits when:
//   - the channel is closed AND drained (clean Close path), or
//   - stopCh is signalled (drain-timeout path); any items still in the
//     channel are counted dropped without being written.
func (w *Writer) workerLoop() {
	defer w.workerWG.Done()
	for {
		select {
		case <-w.stopCh:
			// Timeout fired: drain remaining queued items as dropped and exit.
			// Use the ok-check on every receive — a closed channel returns
			// zero-value immediately and would otherwise spin forever (the
			// `default` case never fires when the channel has a value, even
			// the zero value of a closed channel).
			for {
				select {
				case _, ok := <-w.ch:
					if !ok {
						return
					}
					w.droppedCount.Add(1)
				default:
					return
				}
			}
		case entry, ok := <-w.ch:
			if !ok {
				return // channel closed and drained
			}
			w.syncAppend(entry)
			w.drainedCount.Add(1)
		}
	}
}

// Close flushes any pending async entries with the configured drain timeout
// and emits one audit_drain_complete event with drained/dropped counts. It
// is a no-op when WithBufferedChannel was not configured. Safe to call
// multiple times; only the first call actually drains.
func (w *Writer) Close() error {
	if w.ch == nil {
		return nil
	}
	if !w.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(w.ch)

	done := make(chan struct{})
	go func() {
		w.workerWG.Wait()
		close(done)
	}()

	timedOut := false
	select {
	case <-done:
	case <-time.After(w.drainTimeout):
		// Entries past timeout are silently dropped (spec §11: NOT audit.broken).
		timedOut = true
		close(w.stopCh)
		<-done
	}

	slog.Info("audit_drain_complete",
		"event", "audit_drain_complete",
		"drained", w.drainedCount.Load(),
		"dropped", w.droppedCount.Load(),
		"timed_out", timedOut,
	)
	return nil
}

// DrainedCount returns the number of entries successfully written via the
// async worker since startup. Reads zero when WithBufferedChannel was not
// configured. Test-only surface.
func (w *Writer) DrainedCount() uint64 { return w.drainedCount.Load() }

// DroppedCount returns the number of entries dropped via the async path —
// either because the channel was full at enqueue time or because Close()
// hit the drain timeout. Test-only surface.
func (w *Writer) DroppedCount() uint64 { return w.droppedCount.Load() }

// recoverMidRotation handles the spec §11 case where audit.jsonl is absent
// but audit.jsonl.lock is present (a prior process crashed between the
// rename and the O_CREAT|O_EXCL of the new file). Best-effort: failures are
// logged and the next Append's normal create path will retry.
func (w *Writer) recoverMidRotation() {
	if _, err := os.Stat(w.lockPath); err != nil {
		return
	}
	if _, err := os.Stat(w.path); err == nil {
		return // another process already recovered.
	}
	release, lockErr := acquireRotationLock(w.lockPath, rotationLockTimeout)
	if lockErr != nil {
		slog.Warn("auditlog: crash recovery skipped (lock unavailable)", "err", lockErr)
		return
	}
	defer func() { _ = release() }()

	// Re-check inside the lock — another process may have created the file
	// while we were waiting.
	if _, err := os.Stat(w.path); err == nil {
		return
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		slog.Warn("auditlog: crash recovery O_CREAT failed", "path", w.path, "err", err)
		return
	}
	_ = f.Close()
}

// initCreationTime sets w.creationTime from the live file's first-line
// timestamp (best-effort approximation of the file's birth time, since
// stat birthtime is not portable). Falls back to mtime, then to now.
func (w *Writer) initCreationTime() {
	w.creationTime = w.now()
	info, err := os.Stat(w.path)
	if err != nil || info.Size() == 0 {
		return
	}
	if ts, ok := readFirstTimestamp(w.path); ok {
		w.creationTime = ts
		return
	}
	w.creationTime = info.ModTime()
}

// readFirstTimestamp parses the `ts` field from the first JSONL line. Returns
// (zero, false) on any error.
func readFirstTimestamp(path string) (time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 1024)
	n, _ := f.Read(buf)
	if n == 0 {
		return time.Time{}, false
	}
	nl := bytes.IndexByte(buf[:n], '\n')
	if nl < 0 {
		nl = n
	}
	var parsed struct {
		TS string `json:"ts"`
	}
	if err := json.Unmarshal(buf[:nl], &parsed); err != nil {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, parsed.TS)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

// Path returns the absolute audit.jsonl path. Useful for tests and ops
// commands that need to surface the file location.
func (w *Writer) Path() string { return w.path }

// SentinelPath returns the absolute audit.broken sentinel path. Spec §11
// requires the sentinel be discoverable at a stable per-profile path so
// `gum.cache_stats` can probe it.
func (w *Writer) SentinelPath() string { return w.sentinelPath }

// Append writes one JSONL record built from entry. It satisfies the
// dispatch.auditSink interface (spec §3.1 step 7). On any filesystem error
// it (a) emits a structured `audit_write_failure` event via slog and (b)
// writes the §11 audit.broken sentinel with the UTC timestamp and OS error.
// The dispatch is NOT failed — availability takes precedence over log
// completeness (§11).
//
// When WithBufferedChannel is configured, Append enqueues to the channel
// (non-blocking, drops on full) and returns; the background worker performs
// the actual write. Without WithBufferedChannel, Append runs syncAppend
// directly on the caller's goroutine (the historical behaviour).
func (w *Writer) Append(entry map[string]any) {
	if w.ch == nil {
		w.syncAppend(entry)
		return
	}
	if w.closed.Load() {
		w.droppedCount.Add(1)
		return
	}
	select {
	case w.ch <- entry:
	default:
		// Channel full; entry dropped (caller has no recourse — availability
		// takes precedence per spec §11).
		w.droppedCount.Add(1)
	}
}

// syncAppend is the historical synchronous append path: marshal, rotate if
// needed, write, clear sentinel. Used directly by Append when no channel is
// configured, and by workerLoop on the async path.
func (w *Writer) syncAppend(entry map[string]any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := marshalEntry(entry, w.now().UTC())
	if err != nil {
		w.handleFailure(fmt.Errorf("marshal: %w", err))
		return
	}

	// Determine which rotation paths might fire. Hard ceiling takes priority
	// over normal size/age thresholds because it has stronger semantics
	// (block-until-acquired) per spec §11.
	currentSize := w.currentSizeLocked()
	if !w.unbounded && w.hardCeiling > 0 && currentSize+int64(len(data)) > w.hardCeiling {
		if rotErr := w.rotateUnderLock(hardCeilingLockTimeout); rotErr != nil {
			w.handleFailure(fmt.Errorf("hard-ceiling rotate: %w", rotErr))
			return
		}
	} else if w.shouldNormalRotate(currentSize, len(data)) {
		if rotErr := w.rotateUnderLock(rotationLockTimeout); rotErr != nil && !errors.Is(rotErr, errLockTimeout) {
			w.handleFailure(fmt.Errorf("rotate: %w", rotErr))
			return
		}
		// errLockTimeout: another process is rotating. Skip our rotation
		// and proceed with append (spec §11 explicit allowance).
	}

	if err := w.appendBytesLocked(data); err != nil {
		w.handleFailure(err)
		return
	}

	// Successful append: clear any stale sentinel from a previous failure
	// (spec §11 "Once a successful append occurs … GUM removes audit.broken").
	if _, err := os.Stat(w.sentinelPath); err == nil {
		_ = os.Remove(w.sentinelPath)
	}
}

// appendBytesLocked opens audit.jsonl with O_APPEND|O_CREATE|O_WRONLY and
// writes data. Per spec §11 it retries once on ENOENT (mid-rotation race
// where another process renamed the file before our open).
func (w *Writer) appendBytesLocked(data []byte) error {
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && attempt == 0 {
				continue // retry per §11
			}
			return fmt.Errorf("open: %w", err)
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return fmt.Errorf("write: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close: %w", err)
		}
		return nil
	}
	return fmt.Errorf("open: %w (ENOENT retry exhausted)", os.ErrNotExist)
}

// currentSizeLocked stats audit.jsonl and returns its size; 0 if absent.
func (w *Writer) currentSizeLocked() int64 {
	info, err := os.Stat(w.path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// shouldNormalRotate evaluates the size and age thresholds (spec §11). Size
// rotation triggers when the next write would push past maxSize; age
// rotation triggers when the live file is older than retentionDays. A zero
// value on either threshold disables that path per spec.
func (w *Writer) shouldNormalRotate(currentSize int64, incoming int) bool {
	if w.maxSize > 0 && currentSize+int64(incoming) > w.maxSize && currentSize > 0 {
		return true
	}
	if w.retentionDays > 0 && currentSize > 0 {
		age := w.now().Sub(w.creationTime)
		if age >= time.Duration(w.retentionDays)*24*time.Hour {
			return true
		}
	}
	return false
}

// rotateUnderLock acquires the cross-process advisory lock with the supplied
// timeout, re-checks thresholds under the lock, performs the atomic rename
// + O_CREAT|O_EXCL sequence, enforces max_files, and releases. Spec §11
// rotation protocol.
func (w *Writer) rotateUnderLock(timeout time.Duration) error {
	release, err := acquireRotationLock(w.lockPath, timeout)
	if err != nil {
		if errors.Is(err, errLockTimeout) {
			return err // bubble up so caller can decide to skip
		}
		// Unexpected lock setup errors fall back to the pre-lock rotation path;
		// hard lock contention is handled by errLockTimeout above.
		return w.rotateLockedAtomic("")
	}
	defer func() { _ = release() }()
	return w.rotateLockedAtomic(w.lockPath)
}

// rotateLockedAtomic executes the rotation steps assumed to be guarded by
// w.mu (intra-process) and the file lock on lockPath (cross-process). The
// lockPath argument is unused inside the function — it's threaded only so
// callers communicate intent and so log messages can include it.
func (w *Writer) rotateLockedAtomic(_ string) error {
	// Step 1: stat — if a peer already rotated, skip.
	if info, err := os.Stat(w.path); err == nil {
		if info.Size() == 0 {
			// Already fresh (peer rotated). Reset creationTime to keep the
			// age threshold honest for this process.
			w.creationTime = w.now()
			return nil
		}
	} else if errors.Is(err, os.ErrNotExist) {
		// audit.jsonl missing — recreate empty and bail.
		f, cerr := os.OpenFile(w.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if cerr != nil && !errors.Is(cerr, os.ErrExist) {
			return cerr
		}
		if f != nil {
			_ = f.Close()
		}
		w.creationTime = w.now()
		return nil
	}

	// Step 2: rename audit.jsonl → audit.<iso>.jsonl
	ts := w.now().UTC().Format("2006-01-02T150405Z")
	rotated := filepath.Join(w.dir, fmt.Sprintf("audit.%s.jsonl", ts))
	for i := 1; i < 1000; i++ {
		if _, err := os.Stat(rotated); errors.Is(err, os.ErrNotExist) {
			break
		}
		rotated = filepath.Join(w.dir, fmt.Sprintf("audit.%s.%d.jsonl", ts, i))
	}
	if err := os.Rename(w.path, rotated); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Peer rotated between our stat and rename; treat as success.
			f, cerr := os.OpenFile(w.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if cerr != nil && !errors.Is(cerr, os.ErrExist) {
				return cerr
			}
			if f != nil {
				_ = f.Close()
			}
			w.creationTime = w.now()
			return nil
		}
		return err
	}
	if err := os.Chmod(rotated, 0o600); err != nil {
		slog.Warn("auditlog: chmod archive failed", "path", rotated, "err", err)
	}

	// Step 3: create fresh empty audit.jsonl atomically.
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create new audit.jsonl: %w", err)
	}
	if f != nil {
		_ = f.Close()
	}
	w.creationTime = w.now()

	// Step 4: enforce max_files.
	return w.enforceMaxFilesLocked()
}

// enforceMaxFilesLocked deletes the oldest archive files until the count is
// at or below w.maxFiles. Called from rotateLockedAtomic; assumes w.mu is held.
func (w *Writer) enforceMaxFilesLocked() error {
	if w.maxFiles < 0 {
		return nil
	}
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}
	var archives []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "audit.") && strings.HasSuffix(name, ".jsonl") && name != "audit.jsonl" {
			archives = append(archives, filepath.Join(w.dir, name))
		}
	}
	if len(archives) <= w.maxFiles {
		return nil
	}
	sort.Strings(archives)
	excess := len(archives) - w.maxFiles
	for i := 0; i < excess; i++ {
		if err := os.Remove(archives[i]); err != nil {
			return err
		}
	}
	return nil
}

// handleFailure writes the audit.broken sentinel and emits the §11
// structured-log event. Caller must hold w.mu.
func (w *Writer) handleFailure(cause error) {
	profile := filepath.Base(w.dir)
	slog.Error("audit_write_failure",
		"event", "audit_write_failure",
		"error", cause.Error(),
		"profile", profile,
	)
	contents := fmt.Sprintf("%s %s\n", w.now().UTC().Format(time.RFC3339Nano), cause.Error())
	// Sentinel file is overwritten on each failure per §11; atomic write so a
	// crash mid-write can't leave a garbled sentinel (review gum-yz76).
	if err := fsatomic.WriteFile(w.sentinelPath, []byte(contents), 0o600); err != nil {
		// Sentinel itself failed to write — log and continue. Availability
		// takes precedence; we have nothing else to do.
		slog.Error("auditlog: sentinel write failed", "path", w.sentinelPath, "err", err)
	}
}

// auditEntryKeyOrder fixes the on-disk key order for the normative §11
// entry shape. `v` MUST appear first; the remaining keys follow the order
// documented in the spec's `audit.jsonl` schema-version paragraph.
var auditEntryKeyOrder = []string{
	"v",
	"ts",
	"op_id",
	"variant_id",
	"args_hash",
	"client_id",
	"risk_class",
	"risk_override",
	"risk_override_reason",
	"shaping_bypassed",
	"sanitizer_bypassed",
	"dual_fetch",
	"panic",
}

// optionalOmitWhenFalse lists the keys that the spec §11 "compact" rule
// removes when false or null. `risk_override` is always emitted; the rest
// drop when their value is the zero value of bool.
var optionalOmitWhenFalse = map[string]bool{
	"shaping_bypassed":     true,
	"sanitizer_bypassed":   true,
	"dual_fetch":           true,
	"risk_override_reason": true,
	"panic":                true,
}

// marshalEntry produces the on-disk JSONL line for entry. It stamps `v: 1`
// as the first key and `ts` as the second (RFC 3339 UTC). Spec-required
// fields not present in entry default to JSON null (string fields) / false
// (booleans) where the spec mandates they be carried.
func marshalEntry(entry map[string]any, ts time.Time) ([]byte, error) {
	merged := make(map[string]any, len(entry)+2)
	for k, v := range entry {
		merged[k] = v
	}
	merged["v"] = SchemaVersion
	merged["ts"] = ts.Format(time.RFC3339Nano)

	// risk_override always present per §11 ("not subject to the
	// omit-when-absent rule").
	if _, ok := merged["risk_override"]; !ok {
		merged["risk_override"] = false
	}

	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	written := map[string]bool{}
	writeKV := func(k string, v any) error {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		keyB, err := json.Marshal(k)
		if err != nil {
			return err
		}
		valB, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(keyB)
		buf.WriteByte(':')
		buf.Write(valB)
		written[k] = true
		return nil
	}

	// Emit keys in the canonical order.
	for _, k := range auditEntryKeyOrder {
		v, ok := merged[k]
		if !ok {
			continue
		}
		if optionalOmitWhenFalse[k] {
			if b, isBool := v.(bool); isBool && !b {
				continue
			}
			if v == nil {
				continue
			}
		}
		if err := writeKV(k, v); err != nil {
			return nil, err
		}
	}

	// Emit any unknown keys (call-site-provided extensions) in
	// deterministic alphabetical order after the canonical block.
	extras := make([]string, 0)
	for k := range merged {
		if !written[k] {
			extras = append(extras, k)
		}
	}
	sort.Strings(extras)
	for _, k := range extras {
		if err := writeKV(k, merged[k]); err != nil {
			return nil, err
		}
	}

	buf.WriteByte('}')
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}
