package callargs_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/cli/callargs"
)

// FuzzParseArgs asserts the §12.0 CLI arg parser never panics on arbitrary
// (untrusted) positional args — a panic in CLI parsing is a DoS. A non-nil
// empty Stdin keeps @- from blocking on os.Stdin; @file just errors on a
// missing path. Seeded with the tricky grammar shapes (dotted keys, escapes,
// := JSON, file refs).
func FuzzParseArgs(f *testing.F) {
	for _, seed := range []string{
		"key=value", "key:=123", "a.b.c=x", `a\.b=y`, `a\\b=z`,
		"key:=[1,2,3]", `key:={"x":1}`, "key:=", "=v", "", "k:=null",
		"k:=10garbage", "...", `."=`, "@-", "@/nonexistent", "k=a=b",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, arg string) {
		opts := callargs.Options{Stdin: strings.NewReader("")}
		_, _ = callargs.ParseArgs([]string{arg}, opts)      // single arg
		_, _ = callargs.ParseArgs([]string{arg, arg}, opts) // duplicate path
	})
}
