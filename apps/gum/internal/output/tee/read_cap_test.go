package tee_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/tee"
)

// TestReadRefusesOversizedArtifact pins the decompression cap in Read. A
// crafted artifact that inflates past 64 MiB must be refused instead of
// buffered, which is the guard that keeps a planted tee file from exhausting
// memory through the gum://results/<hash> resource.
func TestReadRefusesOversizedArtifact(t *testing.T) {
	const overCap = (64 << 20) + 1

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := io.CopyN(gz, zeroReader{}, overCap); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	path := filepath.Join(t.TempDir(), "bomb.json.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	_, err := tee.Read(path)
	if err == nil {
		t.Fatal("Read err=nil; want the cap refusal")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("err=%v; want the byte-cap error", err)
	}
}

// zeroReader is an endless source of NUL bytes, which gzip compresses to a
// few kilobytes so the fixture stays small on disk.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
