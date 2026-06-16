// Package initpkg implements `gum init` (spec §12.2): the diff-and-prompt
// installer that patches ~/.claude/settings.json and writes GUM.md.
//
// Named `initpkg` rather than `init` because `init` is a reserved Go identifier
// for package-init functions.
package initpkg

import (
	_ "embed"
	"text/template"
)

// GUMmdTmpl is the embedded GUM.md starter template (gum-xkug.5). Rendered by
// RenderGUMmd with the current gum version. Safe for `gum init --refresh` to
// rewrite without diff because the file is informational, not security-
// sensitive (spec §12.2 step 3a).
//
//go:embed GUM.md.tmpl
var GUMmdTmpl string

// RenderGUMmd renders GUMmdTmpl into the project's GUM.md byte form for the
// given gum binary version. Caller MUST persist the bytes atomically.
func RenderGUMmd(version string) ([]byte, error) {
	t, err := template.New("GUM.md").Parse(GUMmdTmpl)
	if err != nil {
		return nil, err
	}
	var buf templateBuffer
	if err := t.Execute(&buf, map[string]string{"GumVersion": version}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// templateBuffer is a no-allocation wrapper around []byte to keep this file
// self-contained (no need to import bytes just to render a tiny template).
type templateBuffer struct{ b []byte }

func (b *templateBuffer) Write(p []byte) (int, error) {
	b.b = append(b.b, p...)
	return len(p), nil
}

func (b *templateBuffer) Bytes() []byte { return b.b }
