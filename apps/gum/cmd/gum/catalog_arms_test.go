package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// runCatalogOverrides runs `catalog list-overrides` under root, with the
// supplied stdout/stderr writers so the terminal and write-failure arms can
// both be driven.
func runCatalogOverrides(t *testing.T, out, errOut io.Writer) error {
	t.Helper()
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"catalog", "list-overrides"})
	return root.Execute()
}

// TestCatalogOverridesRejectsABadProfile pins the resolveProfileName arm
// inside RunE. The real root rejects the name in PersistentPreRunE, so only
// a root without that hook reaches the guard.
func TestCatalogOverridesRejectsABadProfile(t *testing.T) {
	withTempConfigRootCLI(t)
	withTempDataRootCLI(t)

	root := bareRootWithProfile("bad/name")
	root.PersistentFlags().Lookup("profile").Changed = true
	root.AddCommand(newCatalogCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"catalog", "list-overrides"})

	err := root.Execute()
	if err == nil {
		t.Fatal("list-overrides with a bad profile succeeded; want a rejection")
	}
	if !strings.Contains(err.Error(), "bad/name") {
		t.Errorf("err=%q does not name the rejected profile", err)
	}
}

// TestCatalogOverridesSurfacesADataDirFailure pins the name.DataDir() arm.
// With no home and no XDG_DATA_HOME there is no profile directory to read
// plugin-catalog.json from, and the command must say so rather than report
// an empty override set.
func TestCatalogOverridesSurfacesADataDirFailure(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out, errOut bytes.Buffer
	err := runCatalogOverrides(t, &out, &errOut)
	if err == nil {
		t.Fatal("list-overrides without a home succeeded; want a data-dir failure")
	}
	if !strings.Contains(err.Error(), "could not determine data dir") {
		t.Errorf("err=%q; want the data-dir wrap", err)
	}
}

// TestCatalogOverridesNotesAnEmptySetOnATerminal pins the gum-s985 split:
// the "no overrides" note goes to stderr for a human and never to stdout,
// so a pipe stays parseable.
func TestCatalogOverridesNotesAnEmptySetOnATerminal(t *testing.T) {
	withTempConfigRootCLI(t)
	withTempDataRootCLI(t)

	// os.DevNull is a character device, so isTerminal reports true without a
	// pty while the note itself is discarded.
	tty, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = tty.Close() })

	var out bytes.Buffer
	if err := runCatalogOverrides(t, &out, tty); err != nil {
		t.Fatalf("list-overrides: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q; the note must stay on stderr", out.String())
	}
}

// TestCatalogOverridesSurfacesAWriteFailure pins the encoder arm. One
// override has to exist first, otherwise the command returns before the
// encoder runs.
func TestCatalogOverridesSurfacesAWriteFailure(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)
	writePluginCatalog(t, dataRoot, "default", []byte(`{
  "plugin_catalog_schema_version": 1,
  "variants": [
    {
      "variant_id": "fli.v1.plugin.search",
      "variant_schema_version": 1,
      "risk_class": "read",
      "risk_override": true,
      "risk_override_reason": "POST endpoint returning read-only results"
    }
  ]
}`))

	var errOut bytes.Buffer
	err := runCatalogOverrides(t, failWriter{}, &errOut)
	if err == nil {
		t.Fatal("list-overrides onto a failing writer succeeded; want an encode failure")
	}
	if !strings.Contains(err.Error(), "encoding output") {
		t.Errorf("err=%q; want the 'encoding output' wrap", err)
	}
}
