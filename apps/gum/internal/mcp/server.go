// Package mcp is the thin MCP server presentation layer (spec.md §14).
//
// Registers meta-tools and convenience tools against a dispatch.Dispatcher.
// Presentation layers stay thin; internal/dispatch owns the invocation lifecycle.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"sync"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embed"
	"github.com/ehmo/gum/internal/embedded"
	"github.com/ehmo/gum/internal/lro"
	profilepkg "github.com/ehmo/gum/internal/profile"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// metaToolNames is the canonical ordered list of Tier A meta-tools.
var metaToolNames = []string{
	"gum.search_apis",
	"gum.describe_op",
	"gum.read",
	"gum.write",
	"gum.destructive",
	"gum.code",
	"gum.poll",
	"gum.cache_stats",
	"gum.gain",
}

// lroPoller is the minimal interface the handler depends on. *lro.Poller
// satisfies it; tests inject fakes.
type lroPoller interface {
	Poll(ctx context.Context, operationName string) (any, error)
}

// pollerFactory constructs a Poller for one handlePoll invocation. The factory
// pattern lets handlePoll inject an OnTick callback that knows the request-
// specific progress token. Tests substitute fakes by overriding this field.
type pollerFactory func(onTick func(elapsed time.Duration)) lroPoller

// sdkServer wraps *sdkmcp.Server to provide a 2-argument Connect method
// (ctx, transport) so test helpers can obtain a *ServerSession handle without
// supplying ServerSessionOptions. The embedded *sdkmcp.Server promotes AddTool
// and Run so all existing call-sites remain unchanged.
type sdkServer struct {
	*sdkmcp.Server
}

// Connect delegates to the underlying Server with nil options.
func (w *sdkServer) Connect(ctx context.Context, t sdkmcp.Transport) (*sdkmcp.ServerSession, error) {
	return w.Server.Connect(ctx, t, nil)
}

// Server wraps the go-sdk MCP server and registers all 27 Tier A tools
// before accepting connections. All tool registrations MUST happen before
// Run is called (spec.md §4.2).
type Server struct {
	disp                 dispatch.Dispatcher
	snapshot             *catalog.Catalog
	bm25                 *embed.Index
	bm25Once             sync.Once // guards the lazy bm25 build (goroutine-per-session)
	bm25Err              error
	sdkSrv               *sdkServer
	convenienceToolNames []string
	pollerFactory        pollerFactory   // injectable; nil → default production poller
	profile              profilepkg.Name // active profile; resolves <data home>/gum/<profile>/audit.broken
	healthCache          healthSnapshotCache
	roots                rootsCache // per-session roots/list cache (§9.2)
}

// SetProfile sets the active profile used for per-profile filesystem paths
// (currently only audit.broken sentinel resolution in handleCacheStats).
// An empty value resets to the "default" profile.
func (s *Server) SetProfile(name string) error {
	profileName, err := profilepkg.Parse(name)
	if err != nil {
		return err
	}
	s.profile = profileName
	return nil
}

// defaultPollerFactory constructs an *lro.Poller wired to the §5.7 routing
// table (internal/lro/routing) plus the two GET fallback templates. The
// LastHost field stays empty in v0.1.0 — fallback steps 2/3 activate only
// once dispatch starts threading the per-session last-upstream-host into the
// factory (a follow-up §5.7 wiring task). With LastHost empty, the fetcher
// still serves operation names that hit a routing-table entry (Compute,
// Cloud Run, google.longrunning.Operations) and surfaces ErrUnroutable for
// everything else — matching the documented v0.1.0 LRO behaviour without
// the previous "all calls fail" stub.
func (s *Server) defaultPollerFactory(onTick func(elapsed time.Duration)) lroPoller {
	return &lro.Poller{
		Fetcher: &lro.HTTPFetcher{},
		OnTick:  onTick,
	}
}

// Version is the advertised server version. Wired from main.version at process
// boot via SetVersion; defaults to "0.1.0-dev" for tests.
var Version = "0.1.0-dev"

// SetVersion overrides the package-level Version. Callers must invoke before
// NewServer.
func SetVersion(v string) {
	if v != "" {
		Version = v
	}
}

// NewServer constructs a Server wrapping disp. The catalog is loaded from
// the embedded snapshot, so all 27 tools are registered with real handlers
// that can route through the dispatcher.
func NewServer(disp dispatch.Dispatcher) *Server {
	return NewServerWithCatalog(disp, defaultCatalog())
}

// NewServerWithCatalog lets callers (tests, alternate entry points) inject
// a specific catalog snapshot instead of the embedded one.
func NewServerWithCatalog(disp dispatch.Dispatcher, snapshot *catalog.Catalog) *Server {
	s := &Server{
		disp:     disp,
		snapshot: snapshot,
		profile:  "default",
	}
	sdkSrv := &sdkServer{sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "gum", Version: Version},
		&sdkmcp.ServerOptions{
			Capabilities: &sdkmcp.ServerCapabilities{
				// ListChanged stays false in v0.1.0: spec §4.1 line 383 forbids
				// tools/list_changed and dynamic Tier B materialization. The
				// invariant is enforced by TestMCPNoSpuriousListChangedNotifications.
				Tools:       &sdkmcp.ToolCapabilities{ListChanged: false},
				Resources:   &sdkmcp.ResourceCapabilities{ListChanged: false},
				Prompts:     &sdkmcp.PromptCapabilities{ListChanged: false},
				Completions: &sdkmcp.CompletionCapabilities{},
			},
			CompletionHandler: s.handleComplete,
		},
	)}
	s.sdkSrv = sdkSrv

	// metaAnnotations is the centralized annotation map (spec.md §13); each
	// meta-tool that carries static MCP annotations is listed there.
	metaAnnotations := TierAMetaToolAnnotations()
	for _, name := range metaToolNames {
		toolName := name
		tool := &sdkmcp.Tool{
			Name:         toolName,
			Description:  metaToolDescription(toolName),
			InputSchema:  metaToolSchema(toolName),
			OutputSchema: metaToolOutputSchema(toolName),
			Annotations:  metaAnnotations[toolName],
			Meta:         promptCacheHintMeta(),
		}
		sdkSrv.AddTool(tool, s.makeMetaToolHandler(toolName))
	}
	s.registerSkillTools()

	s.registerConvenienceTools(embedded.TierARosterJSON)
	s.registerResultsResource()
	s.registerHelpResources()
	s.registerStaticResources()
	s.registerResourceTemplates()
	s.registerPrompts()

	return s
}

func (s *Server) registerSkillTools() {
	annotations := &sdkmcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: boolPtr(false),
	}
	for _, def := range []struct {
		name        string
		description string
		schema      json.RawMessage
		handler     sdkmcp.ToolHandler
	}{
		{
			name:        "skills_list",
			description: "List embedded gum agent skills without returning skill bodies.",
			schema:      skillsListSchema(),
			handler:     s.handleSkillsList,
		},
		{
			name:        "skills_get",
			description: "Return one embedded gum agent skill body.",
			schema:      skillsGetSchema(),
			handler:     s.handleSkillsGet,
		},
	} {
		toolDef := def
		s.sdkSrv.AddTool(&sdkmcp.Tool{
			Name:         toolDef.name,
			Description:  toolDef.description,
			InputSchema:  toolDef.schema,
			OutputSchema: singleObjectResultSchema(),
			Annotations:  annotations,
			Meta:         promptCacheHintMeta(),
		}, toolDef.handler)
	}
}

// defaultCatalog returns the embedded catalog snapshot, or nil if the embedded
// JSON cannot be parsed.
func defaultCatalog() *catalog.Catalog {
	if len(embedded.CatalogJSON) == 0 {
		return nil
	}
	var c catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &c); err != nil {
		return nil
	}
	return &c
}

// rosterJSON is the minimal shape of data/tier-a-roster.v1.json.
type rosterJSON struct {
	ConvenienceTools []string `json:"convenience_tools"`
}

// registerConvenienceTools parses rosterData and registers each convenience
// tool name with a real handler that routes through the catalog.
func (s *Server) registerConvenienceTools(rosterData []byte) {
	if len(rosterData) == 0 {
		return
	}
	var roster rosterJSON
	if err := json.Unmarshal(rosterData, &roster); err != nil {
		return
	}
	// Apply the same §13 read/destructive hints the meta tools get. Without this
	// every convenience tool registered with nil Annotations, and the SDK
	// serializes a nil DestructiveHint as destructiveHint=true on the wire — so
	// write tools like gmail_send were advertised to clients as destructive.
	annotations := TierAMetaToolAnnotations()
	for _, name := range roster.ConvenienceTools {
		toolName := name
		s.sdkSrv.AddTool(
			&sdkmcp.Tool{
				Name:         toolName,
				Description:  convenienceToolDescription(toolName),
				InputSchema:  convenienceToolSchema(toolName),
				OutputSchema: convenienceToolOutputSchema(toolName),
				Annotations:  annotations[toolName],
				Meta:         promptCacheHintMeta(),
			},
			s.makeConvenienceHandler(toolName),
		)
		s.convenienceToolNames = append(s.convenienceToolNames, name)
	}
}

// stringArg extracts a string value from args by key, returning "" if absent or not a string.
func stringArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// numericArg coerces a JSON-decoded value to float64. JSON numbers decode to
// float64, but a programmatic caller may pass int/int64/json.Number, so handle
// those too. Returns (0, false) for non-numeric values.
func numericArg(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// Run starts the MCP server on the given transport and blocks until the
// client disconnects or ctx is cancelled.
func (s *Server) Run(ctx context.Context, transport sdkmcp.Transport) error {
	return s.sdkSrv.Run(ctx, transport)
}

// MetaToolNames returns the 9 meta-tool names registered on this server.
func (s *Server) MetaToolNames() []string {
	names := make([]string, len(metaToolNames))
	copy(names, metaToolNames)
	return names
}

// ConvenienceToolNames returns the convenience tool names registered.
func (s *Server) ConvenienceToolNames() []string {
	if s.convenienceToolNames == nil {
		return []string{}
	}
	names := make([]string, len(s.convenienceToolNames))
	copy(names, s.convenienceToolNames)
	return names
}

// AllToolNames returns all tool names (meta + convenience).
func (s *Server) AllToolNames() []string {
	all := s.MetaToolNames()
	all = append(all, s.ConvenienceToolNames()...)
	return all
}

// Sessions returns the underlying SDK server's active sessions. Exposed so
// tests can drive session-scoped helpers (e.g. ResolveProjectRootForRequest)
// after a client has connected over an in-memory transport.
func (s *Server) Sessions() iter.Seq[*sdkmcp.ServerSession] {
	return s.sdkSrv.Sessions()
}

// ensure unused import is referenced when fmt is only used downstream.
var _ = fmt.Sprintf
