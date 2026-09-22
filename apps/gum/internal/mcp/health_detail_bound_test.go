package mcp

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// gum-9axv. Spec §13 bounds the gum://status/health detail column at 80
// characters. Nothing enforced it, for any row: several degraded details
// concatenate an os error whose text is set by the path that failed and so
// is unbounded at the source.
//
// The owner narrowed the clause to "empty, or a short reason", so a healthy
// row keeps its explanation. What both arms share is the bound, which these
// tests make real. They drive healthSnapshotCache.snapshot, the resource's
// own entry point and the single place the clamp runs, so a probe added
// later cannot route around it.

// snapshotRows indexes one snapshot by subsystem.
func snapshotRows(t *testing.T, profileDir string) map[string]subsystemHealth {
	t.Helper()
	c := &healthSnapshotCache{}
	rows := c.snapshot(time.Now().UTC(), profileDir)
	byName := make(map[string]subsystemHealth, len(rows))
	for _, row := range rows {
		byName[row.Subsystem] = row
	}
	return byName
}

func TestHealthDetailIsBoundedForEverySubsystem(t *testing.T) {
	rows := snapshotRows(t, t.TempDir())

	if len(rows) != len(staticHealthSubsystems) {
		t.Fatalf("snapshot returned %d rows; want %d", len(rows), len(staticHealthSubsystems))
	}
	for _, name := range staticHealthSubsystems {
		row, ok := rows[name]
		if !ok {
			t.Errorf("snapshot has no row for %q", name)
			continue
		}
		switch row.Status {
		case "healthy", "degraded", "unavailable":
		default:
			t.Errorf("%s: Status = %q; not in the closed enum", name, row.Status)
		}
		if n := utf8.RuneCountInString(row.Detail); n > healthDetailMaxChars {
			t.Errorf("%s: detail is %d chars; the §13 bound is %d: %q", name, n, healthDetailMaxChars, row.Detail)
		}
	}
}

// A degraded row built from an os error is the case the bound exists for.
// Before the clamp this row was 388 characters.
func TestHealthDetailIsBoundedOnAnOSError(t *testing.T) {
	deep := t.TempDir()
	for range 8 {
		deep = filepath.Join(deep, strings.Repeat("segment", 4))
	}

	row := snapshotRows(t, filepath.Join(deep, "\x00profile"))["audit_log"]
	if row.Status != "degraded" {
		t.Fatalf("Status = %q; want degraded for an unusable audit dir", row.Status)
	}
	if n := utf8.RuneCountInString(row.Detail); n > healthDetailMaxChars {
		t.Fatalf("detail is %d chars; the §13 bound is %d: %q", n, healthDetailMaxChars, row.Detail)
	}
	if !strings.HasSuffix(row.Detail, healthDetailEllipsis) {
		t.Errorf("detail = %q; a clamped detail must end in %q so the reader knows it was cut",
			row.Detail, healthDetailEllipsis)
	}
}

// The clamp counts runes, not bytes, and never splits one.
func TestClampHealthDetailKeepsRunesWhole(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"short is untouched", "ledger present", "ledger present"},
		{"exactly at the bound", strings.Repeat("a", 80), strings.Repeat("a", 80)},
		{"one over is clamped", strings.Repeat("a", 81), strings.Repeat("a", 77) + healthDetailEllipsis},
		{"multibyte is not split", strings.Repeat("é", 200), strings.Repeat("é", 77) + healthDetailEllipsis},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := clampHealthDetail(tc.in)
			if got != tc.want {
				t.Errorf("clampHealthDetail(%d chars) = %q; want %q", utf8.RuneCountInString(tc.in), got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("clampHealthDetail produced invalid UTF-8: %q", got)
			}
		})
	}
}

// The owner's arm (b): a healthy row may carry a short reason. Two probes
// report healthy only because they are not implemented, and blanking their
// detail would make a deferred probe read as a real pass.
func TestHealthyRowsMayExplainThemselves(t *testing.T) {
	rows := snapshotRows(t, t.TempDir())

	for _, name := range []string{"canary_runner", "keychain"} {
		row := rows[name]
		if row.Status != "healthy" {
			t.Errorf("%s: Status = %q; want healthy", name, row.Status)
		}
		if row.Detail == "" {
			t.Errorf("%s: detail is empty; a probe that is healthy only because it is deferred must say so", name)
		}
	}
}
