package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/embedded"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	catalogResourceURI      = "gum://catalog"
	canariesResourceURI     = "gum://status/canaries"
	pluginsResourceURI      = "gum://plugins"
	statusHealthResourceURI = "gum://status/health"
	noAutoInjectAnnotation  = "x-gum-do-not-auto-inject"
)

// staticHealthSubsystems is the closed v0.1.0 enum for gum://status/health
// rows. Spec §13 line 3149 pins this set; widening it requires a minor-version
// spec PR (tracked under gum-nb85).
var staticHealthSubsystems = []string{
	"audit_log",
	"cache_sqlite",
	"canary_runner",
	"gain_ledger",
	"keychain",
	"tee_filesystem",
}

// registerStaticResources wires the four spec §13 static resources (catalog,
// status/canaries, plugins, status/health) that complete the v0.1.0 quintet
// alongside gum://help/topics. Each handler is intentionally lightweight here;
// deeper data wiring lives in sibling beads (gum-nb85 status/health probes,
// gum-k9k templates, gum-99f prompt-cache hints).
func (s *Server) registerStaticResources() {
	catalogBytes := embedded.CatalogJSON
	s.sdkSrv.AddResource(
		&sdkmcp.Resource{
			Name:        "gum_catalog",
			Title:       "GUM operation catalog",
			Description: "Full catalog snapshot embedded at build time. Never auto-injected; clients fetch on demand only.",
			URI:         catalogResourceURI,
			MIMEType:    "application/json",
			Size:        int64(len(catalogBytes)),
			Meta:        sdkmcp.Meta{noAutoInjectAnnotation: true},
		},
		s.handleCatalogRead,
	)

	s.sdkSrv.AddResource(
		&sdkmcp.Resource{
			Name:        "gum_status_canaries",
			Title:       "GUM plugin canary statuses",
			Description: "TOON rows of plugin canary outcomes (plugin canaries only; first-party Google APIs rely on status.google.com).",
			URI:         canariesResourceURI,
			MIMEType:    "text/plain",
		},
		s.handleCanariesRead,
	)

	s.sdkSrv.AddResource(
		&sdkmcp.Resource{
			Name:        "gum_plugins",
			Title:       "GUM installed plugins",
			Description: "Compact plugin inventory for the active profile. installed_pending_restart rows are filtered (visible via `gum plugin list` CLI).",
			URI:         pluginsResourceURI,
			MIMEType:    "text/plain",
		},
		s.handlePluginsRead,
	)

	s.sdkSrv.AddResource(
		&sdkmcp.Resource{
			Name:        "gum_status_health",
			Title:       "GUM local infrastructure health",
			Description: "TOON rows for the six local subsystems (audit_log, cache_sqlite, tee_filesystem, keychain, gain_ledger, canary_runner). Local-only — no upstream calls.",
			URI:         statusHealthResourceURI,
			MIMEType:    "text/plain",
		},
		s.handleStatusHealthRead,
	)
}

// handleCatalogRead returns the embedded catalog JSON verbatim. The byte count
// matches the Size annotation reported in resources/list.
func (s *Server) handleCatalogRead(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	if len(embedded.CatalogJSON) == 0 {
		return nil, resourceNotFoundError(req.Params.URI, "catalog snapshot empty")
	}
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(embedded.CatalogJSON),
			},
		},
	}, nil
}

// handleCanariesRead returns the canary roster as TOON with status="stale" for
// every row (spec §13 line 3147 "initial state" rule). Until the §8.5 passive
// canary runner is wired into the MCP server, every row is stale; a future
// bead will replace this with the in-memory canary-runner state.
func (s *Server) handleCanariesRead(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	var b strings.Builder
	b.WriteString("op: gum.status.canaries\n")
	b.WriteString("variant: gum.status.canaries.v1\n")
	b.WriteString("format_version: 1\n")
	b.WriteString("fields: canary_id,op_id,variant_id,status,last_run_at,latency_ms,error_code\n")
	b.WriteString("count: 0\n")
	b.WriteString("\n")
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{URI: req.Params.URI, MIMEType: "text/plain", Text: b.String()},
		},
	}, nil
}

// handlePluginsRead returns the plugin inventory for the active profile. Rows
// in status=installed_pending_restart are filtered out per spec §13 line 3148.
// Errors that prevent loading the registry surface as an empty list rather
// than a hard failure; the operator can still inspect `gum plugin list`.
func (s *Server) handlePluginsRead(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	rows := s.loadPluginInventoryRows()
	var b strings.Builder
	b.WriteString("op: gum.plugins\n")
	b.WriteString("variant: gum.plugins.v1\n")
	b.WriteString("format_version: 1\n")
	b.WriteString("fields: name,version,shape,status,tos,risk,variant_count\n")
	fmt.Fprintf(&b, "count: %d\n\n", len(rows))
	for _, r := range rows {
		b.WriteString(csvField(r.Name))
		b.WriteByte(',')
		b.WriteString(csvField(r.Version))
		b.WriteByte(',')
		b.WriteString(csvField(r.Shape))
		b.WriteByte(',')
		b.WriteString(csvField(r.Status))
		b.WriteByte(',')
		b.WriteString(csvField(r.ToS))
		b.WriteByte(',')
		b.WriteString(csvField(r.Risk))
		b.WriteByte(',')
		fmt.Fprintf(&b, "%d\n", r.VariantCount)
	}
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{URI: req.Params.URI, MIMEType: "text/plain", Text: b.String()},
		},
	}, nil
}

// handleStatusHealthRead returns the closed six-subsystem health table.
// Rows are sourced from healthProbes via the 5s TTL snapshot cache; per
// spec §13 line 3149 no probe may make an upstream network call. The
// row order is stable (lexicographic by subsystem name) so test fixtures
// and consumers can compare without re-sorting.
func (s *Server) handleStatusHealthRead(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	rows := s.healthCache.snapshot(time.Now().UTC(), s.profilePluginDir())
	var b strings.Builder
	b.WriteString("op: gum.status.health\n")
	b.WriteString("variant: gum.status.health.v1\n")
	b.WriteString("format_version: 1\n")
	b.WriteString("fields: subsystem,status,detail,last_check_at\n")
	fmt.Fprintf(&b, "count: %d\n\n", len(rows))
	for _, r := range rows {
		b.WriteString(csvField(r.Subsystem))
		b.WriteByte(',')
		b.WriteString(csvField(r.Status))
		b.WriteByte(',')
		b.WriteString(csvField(r.Detail))
		b.WriteByte(',')
		b.WriteString(r.LastCheckAt.Format(time.RFC3339))
		b.WriteByte('\n')
	}
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{URI: req.Params.URI, MIMEType: "text/plain", Text: b.String()},
		},
	}, nil
}

// pluginInventoryRow is the shape of one gum://plugins row before TOON
// rendering. Fields mirror spec §13 line 3148.
type pluginInventoryRow struct {
	Name         string
	Version      string
	Shape        string
	Status       string
	ToS          string
	Risk         string
	VariantCount int
}

// loadPluginInventoryRows reads plugin-state.json + plugins.lock for the
// active profile and returns the visible-in-MCP row set. Missing files or
// load errors yield an empty list — the MCP resource never fails on a missing
// registry; that's a fresh-install or no-plugins-yet scenario.
func (s *Server) loadPluginInventoryRows() []pluginInventoryRow {
	profileDir := s.profilePluginDir()
	if profileDir == "" {
		return nil
	}
	statePath := filepath.Join(profileDir, "plugin-state.json")
	lockPath := filepath.Join(profileDir, "plugins.lock")
	stateRows := loadPluginRowsFromFile(statePath)
	lockRows := loadPluginRowsFromFile(lockPath)
	lockByName := make(map[string]map[string]any, len(lockRows))
	for _, row := range lockRows {
		if n, _ := row["name"].(string); n != "" {
			lockByName[n] = row
		}
	}
	out := make([]pluginInventoryRow, 0, len(stateRows))
	for _, row := range stateRows {
		name, _ := row["name"].(string)
		if name == "" {
			continue
		}
		status := resolvePluginStatus(row)
		if status == "installed_pending_restart" {
			continue // spec §13 line 3148 MCP filter
		}
		lock := lockByName[name]
		out = append(out, pluginInventoryRow{
			Name:         name,
			Version:      stringFromRow(lock, "version"),
			Shape:        stringFromRow(lock, "shape"),
			Status:       status,
			ToS:          stringFromRow(lock, "tos"),
			Risk:         stringFromRow(lock, "risk"),
			VariantCount: intFromRow(lock, "variant_count"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// profilePluginDir returns the active profile's plugin registry directory.
// Honours XDG_DATA_HOME; empty when neither XDG nor HOME resolves.
func (s *Server) profilePluginDir() string {
	dir, err := s.profile.DataDir()
	if err != nil {
		return ""
	}
	return dir
}

func loadPluginRowsFromFile(path string) []map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var parsed struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}
	return parsed.Plugins
}

// resolvePluginStatus folds the per-row state-flag enum down to the closed
// inventory status (spec §13 line 3176). Precedence: quarantined →
// needs_configuration → installed_pending_restart → active. This mirrors the
// catalog model's ordering for unambiguous reporting when several flags overlap.
func resolvePluginStatus(row map[string]any) string {
	if v, _ := row["quarantined"].(bool); v {
		return "quarantined"
	}
	if v, _ := row["status"].(string); v != "" {
		return v
	}
	return "active"
}

func stringFromRow(row map[string]any, key string) string {
	if row == nil {
		return ""
	}
	if v, ok := row[key].(string); ok {
		return v
	}
	return ""
}

func intFromRow(row map[string]any, key string) int {
	if row == nil {
		return 0
	}
	switch v := row[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}
