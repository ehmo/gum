package auditlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestReadFirstTimestamp covers the five branches of the helper used to
// stamp creation_time during recoverMidRotation:
//   - Missing file → (zero, false).
//   - Empty file → (zero, false).
//   - First line malformed JSON → (zero, false).
//   - First line valid JSON but ts unparseable → (zero, false).
//   - Happy path → parsed RFC3339Nano timestamp + true.
func TestReadFirstTimestamp(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing_file", func(t *testing.T) {
		_, ok := readFirstTimestamp(filepath.Join(dir, "absent.jsonl"))
		if ok {
			t.Errorf("ok=true for missing file; want false")
		}
	})

	t.Run("empty_file", func(t *testing.T) {
		path := filepath.Join(dir, "empty.jsonl")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, ok := readFirstTimestamp(path)
		if ok {
			t.Errorf("ok=true for empty file; want false")
		}
	})

	t.Run("malformed_json", func(t *testing.T) {
		path := filepath.Join(dir, "bad-json.jsonl")
		if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, ok := readFirstTimestamp(path)
		if ok {
			t.Errorf("ok=true for malformed JSON; want false")
		}
	})

	t.Run("ts_unparseable", func(t *testing.T) {
		path := filepath.Join(dir, "bad-ts.jsonl")
		if err := os.WriteFile(path, []byte(`{"ts":"never"}`+"\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, ok := readFirstTimestamp(path)
		if ok {
			t.Errorf("ok=true for unparseable ts; want false")
		}
	})

	t.Run("happy_path", func(t *testing.T) {
		path := filepath.Join(dir, "good.jsonl")
		want := "2026-05-25T12:34:56.789Z"
		if err := os.WriteFile(path, []byte(`{"ts":"`+want+`","op":"x"}`+"\n{\"ts\":\"ignored\"}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		got, ok := readFirstTimestamp(path)
		if !ok {
			t.Fatalf("ok=false for valid first line")
		}
		expected, _ := time.Parse(time.RFC3339Nano, want)
		if !got.Equal(expected) {
			t.Errorf("got %s, want %s", got, expected)
		}
	})

	t.Run("no_trailing_newline", func(t *testing.T) {
		// IndexByte returns -1 → nl = n branch.
		path := filepath.Join(dir, "no-newline.jsonl")
		want := "2026-05-25T12:34:56Z"
		if err := os.WriteFile(path, []byte(`{"ts":"`+want+`"}`), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		got, ok := readFirstTimestamp(path)
		if !ok {
			t.Fatalf("ok=false; want true for trailing-newline-free file")
		}
		expected, _ := time.Parse(time.RFC3339Nano, want)
		if !got.Equal(expected) {
			t.Errorf("got %s, want %s", got, expected)
		}
	})
}
