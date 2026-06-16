package securityscan

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// MaxBinarySizeBytes caps the linux/amd64 release binary per spec §15.
// 120 MB chosen for install ergonomics; if the binary exceeds this the
// mitigation path is zstd-compressing the embedded catalog snapshot.
const MaxBinarySizeBytes = 120 * 1024 * 1024

// TestBinarySize builds the gum binary for linux/amd64 and asserts the size
// stays under MaxBinarySizeBytes (§15 binary-size cap).
func TestBinarySize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary-size test in -short mode")
	}

	root := moduleRoot(t)
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "gum-linux-amd64")

	cmd := exec.Command("go", "build",
		"-trimpath",
		"-ldflags", "-s -w",
		"-o", binary,
		"./cmd/gum")
	cmd.Dir = root
	cmd.Env = append(cmd.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH=amd64",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build failed: %v\nstderr: %s", err, stderr.String())
	}

	info, err := os.Stat(binary)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > MaxBinarySizeBytes {
		t.Fatalf("linux/amd64 binary size %s exceeds cap %s (spec §15)\n"+
			"mitigation: zstd-compress the embedded catalog snapshot (`gen/catalog.bin`)",
			formatBytes(info.Size()), formatBytes(MaxBinarySizeBytes))
	}
	t.Logf("linux/amd64 binary size: %s / %s cap", formatBytes(info.Size()), formatBytes(MaxBinarySizeBytes))
}

func formatBytes(n int64) string {
	const (
		KB = 1 << 10
		MB = 1 << 20
	)
	switch {
	case n >= MB:
		return fmt.Sprintf("%.2f MB", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.2f KB", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
