package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

	// canaryStatusStale is the §13 initial state: a known canary
	// that has not run. It is the only status gum emits until the §8.5
	// passive runner lands.
	canaryStatusStale = "stale"
)

// staticHealthSubsystems is the closed enum for gum://status/health
// rows. Spec §13 pins this set; widening it requires a minor-version
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
// status/canaries, plugins, status/health) that complete the quintet
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

// canaryStatusRow is one gum://status/canaries row. Field order matches the
// §13 column order.
type canaryStatusRow struct {
	CanaryID  string
	OpID      string
	VariantID string
	Status    string
	LastRunAt string
	LatencyMS string
	ErrorCode string
}

// canaryRoster returns one row per known plugin canary, sorted by canary_id.
//
// A plugin canary is per-plugin, not per-op: `gum canary --plugin=<id>` spawns
// the subprocess once and reports whether it booted, so canary_id is the
// plugin name and op_id/variant_id stay empty. Naming one of the plugin's N
// advertised variants would claim a target the canary does not have.
//
// The roster is LoadPluginInventory unfiltered. gum://plugins drops
// installed_pending_restart rows because those plugins cannot dispatch, but
// §13 requires the opposite here: "every known plugin canary,
// including freshly-installed-but-never-run canaries".
//
// Every row is stale with an empty last_run_at. No §8.5 passive runner is
// wired into the server and runCanary persists no result, so there is no run
// to report; stale + empty last_run_at is the spec's "never run" encoding, not
// an error. latency_ms is omitted for stale and error_code is set only when
// status is fail, so both stay empty.
func (s *Server) canaryRoster() []canaryStatusRow {
	installed := LoadPluginInventory(s.profilePluginDir())
	rows := make([]canaryStatusRow, 0, len(installed))
	for _, p := range installed {
		rows = append(rows, canaryStatusRow{CanaryID: p.Name, Status: canaryStatusStale})
	}
	return rows
}

// handleCanariesRead renders the canary roster as TOON. It is regenerated on
// every resources/read, so a plugin installed after startup shows up on the
// next read.
func (s *Server) handleCanariesRead(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
	rows := s.canaryRoster()
	var b strings.Builder
	b.WriteString("op: gum.status.canaries\n")
	b.WriteString("variant: gum.status.canaries.v1\n")
	b.WriteString("format_version: 1\n")
	b.WriteString("fields: canary_id,op_id,variant_id,status,last_run_at,latency_ms,error_code\n")
	fmt.Fprintf(&b, "count: %d\n\n", len(rows))
	for _, r := range rows {
		b.WriteString(csvField(r.CanaryID))
		b.WriteByte(',')
		b.WriteString(csvField(r.OpID))
		b.WriteByte(',')
		b.WriteString(csvField(r.VariantID))
		b.WriteByte(',')
		b.WriteString(csvField(r.Status))
		b.WriteByte(',')
		b.WriteString(csvField(r.LastRunAt))
		b.WriteByte(',')
		b.WriteString(csvField(r.LatencyMS))
		b.WriteByte(',')
		b.WriteString(csvField(r.ErrorCode))
		b.WriteByte('\n')
	}
	return &sdkmcp.ReadResourceResult{
		Contents: []*sdkmcp.ResourceContents{
			{URI: req.Params.URI, MIMEType: "text/plain", Text: b.String()},
		},
	}, nil
}

// handlePluginsRead returns the plugin inventory for the active profile. Rows
// in status=installed_pending_restart are filtered out per spec §13.
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
// spec §13 no probe may make an upstream network call. The
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

// loadPluginInventoryRows returns the rows gum://plugins may show: the full
// profile inventory minus installed_pending_restart, which spec §13
// filters out because those plugins cannot dispatch in this session. The CLI
// reads the same inventory unfiltered through mcp.LoadPluginInventory.
func (s *Server) loadPluginInventoryRows() []PluginInventoryRow {
	all := LoadPluginInventory(s.profilePluginDir())
	out := make([]PluginInventoryRow, 0, len(all))
	for _, row := range all {
		if row.Status == "installed_pending_restart" {
			continue
		}
		out = append(out, row)
	}
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
// inventory status (spec §13). Precedence: quarantined →
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
