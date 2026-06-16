package toon_test

import (
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// FuzzToonDecode asserts the TOON decoder never panics on arbitrary (possibly
// adversarial) input — a panic in a parser fed untrusted data is a DoS. Seeded
// with the shapes the audit touched (quoted keys with '=', sentinels, CSV).
func FuzzToonDecode(f *testing.F) {
	for _, seed := range []string{
		"key=value\n",
		`"a=b"=c` + "\n",
		"{}\n",
		"count=0\nsize=0\n",
		`"k""q"=v` + "\n",
		"a,b,c\n1,2,3\n",
		"=\n",
		`"` + "\n",
		"\x00\x1f=\xff",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = toon.Decode(data) // must not panic on any input
	})
}
