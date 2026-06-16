package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestLoadPluginResourceRecordNeedsConfiguration pins
// loadPluginResourceRecord's `case "needs_configuration":` arm
// (plugin_resource.go:123-129) plus its two inner sub-arms:
//   - line 124-126: empty state.reason → fallback to "missing_credentials"
//   - line 127-129: non-empty credential_descriptors → surface sanitised list
//
// Without coverage on this branch, a plugin that needs auth setup would
// silently surface an empty Reason and no descriptors, blinding the
// resource consumer to actionable setup hints (spec §13 line 3165).
func TestLoadPluginResourceRecordNeedsConfiguration(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	dir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock := `{"plugins":[{"name":"needsauth.example","version":"0.1.0"}]}`
	if err := os.WriteFile(filepath.Join(dir, "plugins.lock"), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	// status=needs_configuration with NO "reason" key → exercises the
	// fallback to "missing_credentials". credential_descriptors present
	// → exercises the descs!=nil assignment arm.
	state := `{"plugins":[{
		"name":"needsauth.example",
		"status":"needs_configuration",
		"credential_descriptors":[
			{"alias":"api_key","kind":"secret","display_name":"API Key","setup_hint":"Set EXAMPLE_API_KEY","env":"EXAMPLE_API_KEY"}
		]
	}]}`
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Server{profile: "default"}
	rec, ok := s.loadPluginResourceRecord("needsauth.example")
	if !ok || rec == nil {
		t.Fatalf("ok=%v rec=%v; want record", ok, rec)
	}
	if rec.Status != "needs_configuration" {
		t.Errorf("Status=%q; want needs_configuration", rec.Status)
	}
	if rec.Reason != "missing_credentials" {
		t.Errorf("Reason=%q; want fallback 'missing_credentials' (state.reason blank)", rec.Reason)
	}
	if len(rec.CredentialDescriptors) != 1 {
		t.Fatalf("CredentialDescriptors=%v; want 1 entry", rec.CredentialDescriptors)
	}
	desc, ok := rec.CredentialDescriptors[0].(map[string]any)
	if !ok {
		t.Fatalf("descriptor[0] type=%T; want map[string]any", rec.CredentialDescriptors[0])
	}
	// Spec §13 line 3165: the raw "env" key MUST be stripped by the
	// sanitiser; only the four whitelisted fields survive.
	if _, leaked := desc["env"]; leaked {
		t.Errorf("descriptor leaked raw 'env' key=%v; sanitiser must drop it", desc["env"])
	}
	if desc["alias"] != "api_key" {
		t.Errorf("descriptor alias=%v; want api_key", desc["alias"])
	}
}

// TestLoadPluginResourceRecordQuarantined pins loadPluginResourceRecord's
// `case "quarantined":` arm (plugin_resource.go:130-133). When the state
// row carries quarantined=true, resolvePluginStatus returns "quarantined"
// and the assembler MUST surface the three quarantine fields (reason,
// quarantined_at, last_error_code) so MCP consumers can render the
// §8.6 quarantine UX rather than mis-labelling the plugin as active.
func TestLoadPluginResourceRecordQuarantined(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	dir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock := `{"plugins":[{"name":"bad.example","version":"0.1.0"}]}`
	if err := os.WriteFile(filepath.Join(dir, "plugins.lock"), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	state := `{"plugins":[{
		"name":"bad.example",
		"quarantined":true,
		"reason":"tool_call_failed",
		"quarantined_at":"2026-02-15T12:00:00Z",
		"last_error_code":"PLUGIN_FAULT"
	}]}`
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Server{profile: "default"}
	rec, ok := s.loadPluginResourceRecord("bad.example")
	if !ok || rec == nil {
		t.Fatalf("ok=%v rec=%v; want record", ok, rec)
	}
	if rec.Status != "quarantined" {
		t.Errorf("Status=%q; want quarantined", rec.Status)
	}
	if rec.Reason != "tool_call_failed" {
		t.Errorf("Reason=%q; want tool_call_failed", rec.Reason)
	}
	if rec.QuarantinedAt != "2026-02-15T12:00:00Z" {
		t.Errorf("QuarantinedAt=%q; want 2026-02-15T12:00:00Z", rec.QuarantinedAt)
	}
	if rec.LastErrorCode != "PLUGIN_FAULT" {
		t.Errorf("LastErrorCode=%q; want PLUGIN_FAULT", rec.LastErrorCode)
	}
}

// TestLoadPluginFileEnvelopeMalformedJSONReturnsNil pins
// loadPluginFileEnvelope's `json.Unmarshal err → return nil` arm
// (plugin_resource.go:157-159). A malformed file MUST be treated as
// "no rows" rather than crashing the resource handler; otherwise a
// corrupted plugin-catalog.json would 500 the entire gum://plugin/{name}
// surface instead of degrading to empty variant_ids.
func TestLoadPluginFileEnvelopeMalformedJSONReturnsNil(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	dir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Valid lock+state so a record assembles; malformed catalog so the
	// envelope reader takes the json.Unmarshal-err path silently.
	lock := `{"plugins":[{"name":"ok.example","version":"1.0.0"}]}`
	if err := os.WriteFile(filepath.Join(dir, "plugins.lock"), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	state := `{"plugins":[{"name":"ok.example","status":"active"}]}`
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	// "{" is incomplete JSON → Unmarshal returns SyntaxError.
	if err := os.WriteFile(filepath.Join(dir, "plugin-catalog.json"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Server{profile: "default"}
	rec, ok := s.loadPluginResourceRecord("ok.example")
	if !ok || rec == nil {
		t.Fatalf("ok=%v rec=%v; want degraded-but-valid record", ok, rec)
	}
	// Empty (not nil) — JSON should emit [] not null per §13.
	if rec.VariantIDs == nil {
		t.Errorf("VariantIDs=nil; want empty slice (malformed catalog should not crash record)")
	}
	if len(rec.VariantIDs) != 0 {
		t.Errorf("VariantIDs=%v; want empty (catalog unreadable)", rec.VariantIDs)
	}
}

// TestCollectPluginVariantIDsFiltersNonObjectsAndOtherOwners pins
// collectPluginVariantIDs's two continue arms (plugin_resource.go:194,
// 197): non-object entries in variants[] MUST be skipped (line 194),
// and rows whose owner_plugin doesn't match the queried name MUST be
// excluded (line 197). The owner_plugin filter is what keeps each
// plugin's variant_ids list scoped per spec §13 line 3161.
func TestCollectPluginVariantIDsFiltersNonObjectsAndOtherOwners(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	dir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock := `{"plugins":[{"name":"target.example","version":"1.0.0"}]}`
	if err := os.WriteFile(filepath.Join(dir, "plugins.lock"), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	state := `{"plugins":[{"name":"target.example","status":"active"}]}`
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	// Variant 1: non-object string → triggers `if !ok { continue }` (line 194).
	// Variant 2: owner_plugin="other.example" → triggers owner-mismatch
	//            continue (line 197).
	// Variants 3-4: matching owner → included (sort.Strings yields
	//               [target.beta, target.zeta]).
	catalog := `{"variants":[
		"not-an-object",
		{"variant_id":"other.x","owner_plugin":"other.example"},
		{"variant_id":"target.zeta","owner_plugin":"target.example"},
		{"variant_id":"target.beta","owner_plugin":"target.example"}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "plugin-catalog.json"), []byte(catalog), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Server{profile: "default"}
	rec, ok := s.loadPluginResourceRecord("target.example")
	if !ok || rec == nil {
		t.Fatalf("ok=%v rec=%v; want record", ok, rec)
	}
	want := []string{"target.beta", "target.zeta"}
	if !reflect.DeepEqual(rec.VariantIDs, want) {
		t.Errorf("VariantIDs=%v; want %v (non-object skipped, other-owner filtered, matches sorted)", rec.VariantIDs, want)
	}
	if rec.VariantCount != 2 {
		t.Errorf("VariantCount=%d; want 2", rec.VariantCount)
	}
}
