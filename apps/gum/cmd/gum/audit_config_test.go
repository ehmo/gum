package main

import (
	"testing"
	"time"

	"github.com/ehmo/gum/internal/auditlog"
	"github.com/ehmo/gum/internal/config"
)

func TestAuditRuntimeConfigDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got := resolveAuditRuntimeConfig("default")
	want := defaultAuditRuntimeConfig()
	if got != want {
		t.Fatalf("resolveAuditRuntimeConfig(default)=%+v; want %+v", got, want)
	}
}

func TestAuditRuntimeConfigProfileOverrides(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg := &config.Config{Values: map[string]string{
		"audit.max_size_mb":           "7",
		"audit.max_files":             "9",
		"audit.retention_days":        "13",
		"audit.unbounded":             "true",
		"audit.drain_timeout_seconds": "4",
	}}
	if err := config.Save("team-a", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	got := resolveAuditRuntimeConfig("team-a")
	if got.maxSizeBytes != 7*1024*1024 {
		t.Errorf("maxSizeBytes=%d; want 7 MiB", got.maxSizeBytes)
	}
	if got.maxFiles != 9 {
		t.Errorf("maxFiles=%d; want 9", got.maxFiles)
	}
	if got.retentionDays != 13 {
		t.Errorf("retentionDays=%d; want 13", got.retentionDays)
	}
	if !got.unbounded {
		t.Error("unbounded=false; want true")
	}
	if got.drainTimeout != 4*time.Second {
		t.Errorf("drainTimeout=%s; want 4s", got.drainTimeout)
	}
}

func TestAuditRuntimeConfigInvalidValuesFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg := &config.Config{Values: map[string]string{
		"audit.max_size_mb":           "-1",
		"audit.max_files":             "-4",
		"audit.retention_days":        "-2",
		"audit.unbounded":             "definitely",
		"audit.drain_timeout_seconds": "-3",
	}}
	if err := config.Save("team-b", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	got := resolveAuditRuntimeConfig("team-b")
	want := defaultAuditRuntimeConfig()
	if got.maxSizeBytes != want.maxSizeBytes {
		t.Errorf("maxSizeBytes=%d; want default %d", got.maxSizeBytes, want.maxSizeBytes)
	}
	if got.maxFiles != want.maxFiles {
		t.Errorf("maxFiles=%d; want default %d", got.maxFiles, want.maxFiles)
	}
	if got.retentionDays != want.retentionDays {
		t.Errorf("retentionDays=%d; want default %d", got.retentionDays, want.retentionDays)
	}
	if got.unbounded != want.unbounded {
		t.Errorf("unbounded=%v; want default %v", got.unbounded, want.unbounded)
	}
	if got.drainTimeout != want.drainTimeout {
		t.Errorf("drainTimeout=%s; want default %s", got.drainTimeout, want.drainTimeout)
	}
}

func TestAuditOptionsForProfileApplyToWriter(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg := &config.Config{Values: map[string]string{
		"audit.max_size_mb":           "0",
		"audit.max_files":             "0",
		"audit.retention_days":        "0",
		"audit.unbounded":             "true",
		"audit.drain_timeout_seconds": "1",
	}}
	if err := config.Save("team-c", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	w, err := auditlog.New(t.TempDir(), append(
		auditOptionsForProfile("team-c", true),
		auditlog.WithBufferedChannel(1),
	)...)
	if err != nil {
		t.Fatalf("auditlog.New with profile options: %v", err)
	}
	w.Append(map[string]any{"op_id": "test.audit.config"})
	if err := w.Close(); err != nil {
		t.Fatalf("close audit writer: %v", err)
	}
	if got := w.DroppedCount(); got != 0 {
		t.Errorf("DroppedCount=%d; want 0 with configured 1s drain", got)
	}
}
