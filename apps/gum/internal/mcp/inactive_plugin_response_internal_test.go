package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInactivePluginResponseForBranches exercises every status arm of
// the helper so a regression that surfaces a plugin in the "wrong"
// resource shape (e.g. needs_configuration returning a quarantine
// envelope) is caught. The helper is the §13 inactive-plugin gate
// between resources/list and resources/read.
func TestInactivePluginResponseForBranches(t *testing.T) {
	mkSrv := func(t *testing.T, stateJSON string) *Server {
		t.Helper()
		dataHome := t.TempDir()
		t.Setenv("XDG_DATA_HOME", dataHome)
		dir := filepath.Join(dataHome, "gum", "default")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte(stateJSON), 0o600); err != nil {
			t.Fatal(err)
		}
		return &Server{profile: "default"}
	}

	t.Run("no_state_row_returns_false", func(t *testing.T) {
		s := mkSrv(t, `{"plugins":[]}`)
		res, jerr, handled := s.inactivePluginResponseFor("uri://x", "missing.plugin", "op", "v1")
		if handled || res != nil || jerr != nil {
			t.Errorf("got (%v,%v,%v); want (nil,nil,false)", res, jerr, handled)
		}
	})

	t.Run("quarantined_flag_emits_envelope", func(t *testing.T) {
		s := mkSrv(t, `{"plugins":[{"name":"q.plugin","quarantined":true,"reason":"hash_mismatch"}]}`)
		res, jerr, handled := s.inactivePluginResponseFor("uri://x", "q.plugin", "op", "v1")
		if !handled || jerr == nil || res != nil {
			t.Errorf("got (%v,%v,%v); want (nil, *jerr, true)", res, jerr, handled)
		}
		if jerr != nil && !strings.Contains(string(jerr.Data), "VARIANT_QUARANTINED") {
			t.Errorf("data=%s; want VARIANT_QUARANTINED envelope", jerr.Data)
		}
	})

	t.Run("installed_pending_restart_emits_schema_only", func(t *testing.T) {
		s := mkSrv(t, `{"plugins":[{"name":"p.plugin","status":"installed_pending_restart"}]}`)
		res, jerr, handled := s.inactivePluginResponseFor("uri://x", "p.plugin", "op", "v1")
		if !handled || jerr != nil || res == nil {
			t.Errorf("got (%v,%v,%v); want (*res, nil, true)", res, jerr, handled)
		}
		if res == nil || len(res.Contents) == 0 {
			t.Fatalf("contents empty: %+v", res)
		}
		raw := res.Contents[0].Text
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("not JSON: %v\nraw=%s", err, raw)
		}
		if payload["execution_support"] != "schema_only" {
			t.Errorf("execution_support=%v; want schema_only", payload["execution_support"])
		}
		if payload["status"] != "installed_pending_restart" {
			t.Errorf("status=%v; want installed_pending_restart", payload["status"])
		}
	})

	t.Run("needs_configuration_emits_credential_aliases", func(t *testing.T) {
		s := mkSrv(t, `{"plugins":[{"name":"n.plugin","status":"needs_configuration","credential_descriptors":[{"alias":"google_api_key"}]}]}`)
		res, jerr, handled := s.inactivePluginResponseFor("uri://x", "n.plugin", "op", "v1")
		if !handled || jerr != nil || res == nil {
			t.Errorf("got (%v,%v,%v); want (*res, nil, true)", res, jerr, handled)
		}
		if res == nil || len(res.Contents) == 0 {
			t.Fatalf("contents empty")
		}
		var payload map[string]any
		_ = json.Unmarshal([]byte(res.Contents[0].Text), &payload)
		if payload["status"] != "needs_configuration" {
			t.Errorf("status=%v; want needs_configuration", payload["status"])
		}
	})

	t.Run("status_quarantined_via_string_emits_envelope", func(t *testing.T) {
		// The belt-and-braces branch: status="quarantined" without the
		// boolean flag still produces VARIANT_QUARANTINED.
		s := mkSrv(t, `{"plugins":[{"name":"qs.plugin","status":"quarantined","reason":"signature_invalid"}]}`)
		res, jerr, handled := s.inactivePluginResponseFor("uri://x", "qs.plugin", "op", "v1")
		if !handled || jerr == nil || res != nil {
			t.Errorf("got (%v,%v,%v); want (nil, *jerr, true)", res, jerr, handled)
		}
	})

	t.Run("active_status_falls_through", func(t *testing.T) {
		// Active plugins are NOT inactive; the helper returns (nil,nil,false)
		// so the caller proceeds with the normal resources/read code path.
		s := mkSrv(t, `{"plugins":[{"name":"active.plugin","status":"active"}]}`)
		res, jerr, handled := s.inactivePluginResponseFor("uri://x", "active.plugin", "op", "v1")
		if handled || jerr != nil || res != nil {
			t.Errorf("got (%v,%v,%v); want (nil, nil, false) for active plugin", res, jerr, handled)
		}
	})
}
