package mcp

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
)

// shadowLogger returns a server logging JSON into buf, so a test reads the
// §9.2 warning off the log channel the stdio server actually uses.
func shadowLogger(t *testing.T, s *Server) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	s.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return &buf
}

// writeShadowProfileDir writes body to <root>/.gum/profiles/<file> and returns
// root. §9.2 resolves project-local profiles from that path.
func writeShadowProfileDir(t *testing.T, file, body string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	dir := filepath.Join(root, ".gum", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// shadowCatalog builds a one-op catalog whose default variant names a builtin
// profile that sets loss-driving fields, so an empty override shadows it.
func shadowCatalog(opID, variantID, profileName string) *catalog.Catalog {
	return &catalog.Catalog{
		Ops: []catalog.Op{{
			OpID:             opID,
			DefaultVariantID: variantID,
			Variants: []catalog.Variant{{
				VariantID:     variantID,
				OutputProfile: profileName,
			}},
		}},
	}
}

// logLines splits a JSON log buffer into records.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

// shadowWarnRecords keeps only the §9.2 shadowing records.
func shadowWarnRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, rec := range logLines(t, buf) {
		if rec["class"] == profile.WarnOverrideDisablesLossyStage {
			out = append(out, rec)
		}
	}
	return out
}

// builtinShadowTarget picks a builtin profile that yields a warning against an
// empty override, so the test pins the real catalog rather than a fixture.
func builtinShadowTarget(t *testing.T) (name string, p *profile.Profile) {
	t.Helper()
	for _, candidate := range profile.BuiltinNames() {
		bp, ok := profile.BuiltinLookup(candidate)
		if !ok {
			continue
		}
		if len(profile.DetectShadowing("x", bp, &profile.Profile{Name: candidate})) > 0 {
			return candidate, bp
		}
	}
	t.Skip("no builtin profile sets a loss-driving field")
	return "", nil
}

// TestMCPWarnsWhenProjectProfileShadowsCatalog pins gum-03bc at the MCP firing
// point: the runtime loader is normative in §9.2, not just profile validate.
func TestMCPWarnsWhenProjectProfileShadowsCatalog(t *testing.T) {
	name, _ := builtinShadowTarget(t)
	root := writeShadowProfileDir(t, name+".toml", "format = \"toon\"\n")

	s := NewServerWithCatalog(nil, shadowCatalog("svc.res.get", "svc.v1.res.get", name))
	buf := shadowLogger(t, s)

	inv := &dispatch.Invocation{OpID: "svc.res.get"}
	s.warnShadowedProfile(root, inv)

	recs := shadowWarnRecords(t, buf)
	if len(recs) == 0 {
		t.Fatalf("no %s record; log = %s", profile.WarnOverrideDisablesLossyStage, buf.String())
	}
	if got := recs[0]["target"]; got != "svc.res.get" {
		t.Errorf("target = %v; want the op id", got)
	}
	msg, _ := recs[0]["msg"].(string)
	if !strings.Contains(msg, "removing a lossy-compression stage") {
		t.Errorf("msg = %q; want the §9.2 wire text", msg)
	}
	if !strings.Contains(msg, "--no-warn-lossy") {
		t.Errorf("msg = %q; want the suppression flag named", msg)
	}
}

// TestMCPWarnsOnBindingShadow covers the other §9.2 shadow shape: an
// [override_bindings] entry attaches a weaker profile to an op whose catalog
// variant already named one.
func TestMCPWarnsOnBindingShadow(t *testing.T) {
	name, _ := builtinShadowTarget(t)
	root := writeShadowProfileDir(t, "local.toml", ""+
		"[output_profiles.\"local_weak\"]\nformat = \"toon\"\n\n"+
		"[override_bindings]\n\"svc.res.get\" = \"local_weak\"\n")

	s := NewServerWithCatalog(nil, shadowCatalog("svc.res.get", "svc.v1.res.get", name))
	buf := shadowLogger(t, s)

	s.warnShadowedProfile(root, &dispatch.Invocation{OpID: "svc.res.get"})

	recs := shadowWarnRecords(t, buf)
	if len(recs) == 0 {
		t.Fatalf("no %s record; log = %s", profile.WarnOverrideDisablesLossyStage, buf.String())
	}
	if got := recs[0]["target"]; got != "svc.res.get" {
		t.Errorf("target = %v; want the bound op id", got)
	}
}

// TestMCPShadowWarningSilentWithoutOverride pins the negative case: a catalog
// profile that no filesystem layer displaces warns about nothing.
func TestMCPShadowWarningSilentWithoutOverride(t *testing.T) {
	name, _ := builtinShadowTarget(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := NewServerWithCatalog(nil, shadowCatalog("svc.res.get", "svc.v1.res.get", name))
	buf := shadowLogger(t, s)

	s.warnShadowedProfile(t.TempDir(), &dispatch.Invocation{OpID: "svc.res.get"})

	if recs := shadowWarnRecords(t, buf); len(recs) != 0 {
		t.Errorf("got %d warnings with no override; log = %s", len(recs), buf.String())
	}
}

// TestMCPShadowWarningSuppressed pins the flag: --no-warn-lossy reaches the
// server through SetSuppressLossyWarnings and silences the log line.
func TestMCPShadowWarningSuppressed(t *testing.T) {
	name, _ := builtinShadowTarget(t)
	root := writeShadowProfileDir(t, name+".toml", "format = \"toon\"\n")

	s := NewServerWithCatalog(nil, shadowCatalog("svc.res.get", "svc.v1.res.get", name))
	s.SetSuppressLossyWarnings(true)
	buf := shadowLogger(t, s)

	s.warnShadowedProfile(root, &dispatch.Invocation{OpID: "svc.res.get"})

	if recs := shadowWarnRecords(t, buf); len(recs) != 0 {
		t.Errorf("got %d warnings under --no-warn-lossy; log = %s", len(recs), buf.String())
	}
}

// TestMCPShadowWarningUsesPinnedVariantProfile pins the variant-precise path: a
// pinned variant's own output_profile is the baseline, not the default's.
func TestMCPShadowWarningUsesPinnedVariantProfile(t *testing.T) {
	name, _ := builtinShadowTarget(t)
	root := writeShadowProfileDir(t, name+".toml", "format = \"toon\"\n")

	cat := shadowCatalog("svc.res.get", "svc.v1.res.get", "")
	cat.Ops[0].Variants = append(cat.Ops[0].Variants, catalog.Variant{
		VariantID:     "svc.v2.res.get",
		OutputProfile: name,
	})

	s := NewServerWithCatalog(nil, cat)
	buf := shadowLogger(t, s)

	s.warnShadowedProfile(root, &dispatch.Invocation{
		OpID:               "svc.res.get",
		RequestedVariantID: "svc.v2.res.get",
	})

	recs := shadowWarnRecords(t, buf)
	if len(recs) == 0 {
		t.Fatalf("pinned variant profile was not used as the baseline; log = %s", buf.String())
	}
}
