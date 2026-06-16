package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/adapters/googleads"
	"github.com/ehmo/gum/internal/auditlog"
	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/config"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embedded"
	"github.com/ehmo/gum/internal/notify"
	outprofile "github.com/ehmo/gum/internal/output/profile"
	"github.com/ehmo/gum/internal/plugins"
	profilepkg "github.com/ehmo/gum/internal/profile"
	"github.com/spf13/cobra"
)

// defaultSemanticCache returns the in-process semantic response cache used
// by the default dispatcher (spec §10.3). Per-op TTLs come from the
// package-level cache.PerOpTTL table; max entries is conservative to bound
// memory in long-running MCP server sessions.
func defaultSemanticCache() *cache.SemanticCache {
	return cache.NewSemanticCache(cache.SemanticConfig{
		MaxEntries: 1024,
	})
}

// defaultAuditBufferSize is the channel depth used when the audit writer is
// constructed in buffered-channel mode (gum-dxpy). v0.1.0 picks 256 as a
// reasonable burst capacity — bigger than the worst gum_parallel fan-out
// (spec §6.3 caps at 16) yet small enough that a runaway producer is bounded.
const defaultAuditBufferSize = 256

type auditRuntimeConfig struct {
	maxSizeBytes  int64
	maxFiles      int
	retentionDays int
	unbounded     bool
	drainTimeout  time.Duration
}

func defaultAuditRuntimeConfig() auditRuntimeConfig {
	return auditRuntimeConfig{
		maxSizeBytes:  auditlog.DefaultMaxSizeBytes,
		maxFiles:      auditlog.DefaultMaxFiles,
		retentionDays: auditlog.DefaultRetentionDays,
		drainTimeout:  auditlog.DefaultDrainTimeout,
	}
}

// Command group IDs for `gum --help` output (gum-4gey.4 / gum-me29.3).
// Cobra renders subcommands grouped by GroupID; commands without one fall
// into "Additional Commands". Order here defines display order.
const (
	groupDiscover = "discover"
	groupRead     = "read"
	groupWrite    = "write"
	groupAuth     = "auth"
	groupPlugin   = "plugin"
	groupProfile  = "profile"
	groupAdmin    = "admin"
)

// newRootCmd builds the root cobra command with all subcommands attached.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "gum",
		Short: "Google Universal MCP — CLI and MCP stdio server",
		Long: "gum is a single Go binary that exposes the same dispatch kernel via a CLI surface and an MCP stdio server. See spec.md for the full contract.\n\n" +
			"Version note: release builds report a semver tag. Local source builds report \"dev\" unless main.version is injected with release ldflags. To check for updates, run `gum config set notify.enabled=true`.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// gum-4gey.1: catalog-aware "did you mean" via cobra's built-in
		// Levenshtein matcher. Distance 2 catches gmail→gain, search→serch.
		SuggestionsMinimumDistance: 2,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := applyProfileSelection(cmd); err != nil {
				return err
			}
			return applyLoggingFlags(cmd)
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")

	root.AddGroup(
		&cobra.Group{ID: groupDiscover, Title: "Discover:"},
		&cobra.Group{ID: groupRead, Title: "Read:"},
		&cobra.Group{ID: groupWrite, Title: "Write & destructive:"},
		&cobra.Group{ID: groupAuth, Title: "Auth:"},
		&cobra.Group{ID: groupPlugin, Title: "Plugin:"},
		&cobra.Group{ID: groupProfile, Title: "Profile & config:"},
		&cobra.Group{ID: groupAdmin, Title: "Admin:"},
	)

	add := func(c *cobra.Command, group string) *cobra.Command {
		c.GroupID = group
		return c
	}

	root.AddCommand(
		add(newSearchCmd(), groupDiscover),
		add(newDescribeCmd(), groupDiscover),
		add(newCatalogCmd(), groupDiscover),
		add(newReadCmd(), groupRead),
		add(newCallCmd(), groupRead),
		add(newWriteCmd(), groupWrite),
		add(newDestructiveCmd(), groupWrite),
		add(newCodeCmd(), groupWrite),
		add(newAuthCmd(), groupAuth),
		add(newTopLevelLoginCmd(), groupAuth),
		add(newTopLevelLogoutCmd(), groupAuth),
		add(newPluginCmd(), groupPlugin),
		add(newProfileCmd(), groupProfile),
		add(newConfigCmd(), groupProfile),
		add(newCacheCmd(), groupProfile),
		add(newSetupCmd(), groupAdmin),
		add(newAgentsCmd(), groupAdmin),
		add(newSkillsCmd(), groupAdmin),
		add(newInitCmd(), groupAdmin),
		add(newMCPCmd(), groupAdmin),
		add(newGainCmd(), groupAdmin),
		add(newCanaryCmd(), groupAdmin),
		add(newDoctorCmd(), groupAdmin),
		add(newVersionCmd(), groupAdmin),
	)
	if root.PersistentFlags().Lookup("profile") == nil {
		root.PersistentFlags().String("profile", "default", "Profile name to read/write config under")
	}
	root.PersistentFlags().String("log-level", "info", "Log level: debug|info|warn|error (overrides GUM_LOG_LEVEL)")
	root.PersistentFlags().String("log-format", "json", "Log format: json|text (spec §14.1 rule 3)")
	return root
}

// applyProfileSelection normalizes the root --profile flag before any command
// resolves per-profile config/data/cache paths. CLI wins over GUM_PROFILE.
func applyProfileSelection(cmd *cobra.Command) error {
	if cmd == nil || cmd.Root() == nil {
		return nil
	}
	f := cmd.Root().PersistentFlags().Lookup("profile")
	if f == nil {
		return nil
	}
	name, err := profilepkg.Resolve(f.Value.String(), f.Changed)
	if err != nil {
		return err
	}
	return f.Value.Set(name.String())
}

// applyLoggingFlags resolves --log-level (with GUM_LOG_LEVEL fallback) and
// --log-format, then re-installs slog.Default with the chosen handler. Spec
// §14.1 rule 3: CLI > env > config precedence.
func applyLoggingFlags(cmd *cobra.Command) error {
	level := slog.LevelInfo
	if env := strings.TrimSpace(os.Getenv("GUM_LOG_LEVEL")); env != "" {
		if l, ok := parseLogLevel(env); ok {
			level = l
		}
	}
	if flag := cmd.Flags().Lookup("log-level"); flag != nil && flag.Changed {
		if l, ok := parseLogLevel(flag.Value.String()); ok {
			level = l
		} else {
			return fmt.Errorf("invalid --log-level %q: want debug|info|warn|error", flag.Value.String())
		}
	}

	format := "json"
	if flag := cmd.Flags().Lookup("log-format"); flag != nil {
		format = strings.ToLower(strings.TrimSpace(flag.Value.String()))
	}

	opts := &slog.HandlerOptions{Level: level, AddSource: false}
	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, opts)
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	default:
		return fmt.Errorf("invalid --log-format %q: want json|text", format)
	}
	slog.SetDefault(slog.New(handler))
	return nil
}

// parseLogLevel maps the closed enum debug|info|warn|error to slog.Level.
func parseLogLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return 0, false
}

// newVersionCmd handles `gum version` (alias to root --version).
//
// When the per-profile config sets notify.enabled=true (off by default,
// gum-afcv.5), the command also surfaces a one-line stderr warning when a
// newer release is cached. The actual GitHub releases API check runs
// asynchronously and never blocks the version output — the result is cached
// for 24h and surfaces on the NEXT invocation, matching npm/cargo/brew.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the gum version (with optional update notifier — see `gum config set notify.enabled=true`)",
		Long: "Print the gum version.\n\n" +
			"A literal \"dev\" output means the binary was built without the release ldflags. " +
			"That happens with `go install` and `go build` because main.version is injected by goreleaser at release time. " +
			"Install an official build from the GitHub releases page to see the real semver, or set notify.enabled=true to get a heads-up when a newer release exists.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), version)
			maybeNotifyUpdate(cmd)
			return nil
		},
	}
}

// maybeNotifyUpdate looks up the active profile's notify.enabled flag and
// dispatches the notifier. Errors are swallowed: the notifier is best-effort
// and must never break `gum version`.
func maybeNotifyUpdate(cmd *cobra.Command) {
	profile := resolveProfileFlag(cmd)
	cfg, _, err := config.Load(profile)
	if err != nil {
		return
	}
	v, _ := cfg.Get(notify.ConfigKey)
	enabled := strings.EqualFold(strings.TrimSpace(v), "true")
	notify.MaybeNotify(cmd.ErrOrStderr(), version, profile, enabled, nil, nil)
}

// loadCatalog returns the embedded catalog snapshot, or nil if unavailable.
func loadCatalog() *catalog.Catalog {
	if len(embedded.CatalogJSON) == 0 {
		return nil
	}
	var c catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &c); err != nil {
		return nil
	}
	return &c
}

// defaultAdapters returns the adapter map shared by CLI and MCP entry points.
// Phase 11 wires both code.risor (sandbox) and rest.typed-rest-sdk (live REST).
// The returned *CodeRunner is exposed so callers can post-wire a Dispatcher
// reference into it (needed by the gum_parallel builtin, spec §6.3).
//
// plugin.mcp routes Shape 1 plugin-bound variants (spec §8.2) to a Host
// rooted at the default install dir. Without an installed plugin, dispatch
// surfaces SERVICE_DOWN — the documented "plugin not installed" failure
// shape from cmd/gen-catalog/gen_flights.go.
func defaultAdapters(profile string) (map[string]dispatch.Adapter, *adapters.CodeRunner) {
	cr := adapters.NewCodeRunner()
	// rest.typed-rest-sdk, rest.discovery-rest, and rest.raw-http all share the
	// same TypedRestSDK executor in v0.1.0 — they only differ in catalog
	// metadata (interface_kind / backend_kind). v0.2.0 will split raw-http into
	// a stricter executor with per-call pre-flight validation hooks.
	rest := adapters.NewTypedRestSDK()
	pluginMCP := adapters.NewPluginMCPLazyWithStarter(func() *plugins.Host {
		return plugins.NewHost(plugins.HostConfig{})
	}, func(ctx context.Context, host *plugins.Host, pluginID string) (*plugins.Plugin, error) {
		profileDir, err := resolveProfileDir(profile)
		if err != nil {
			return nil, err
		}
		reg, err := openRegistry(profileDir, nil)
		if err != nil {
			return nil, err
		}
		return plugins.NewSupervisor(reg, host.Start, time.Now).Start(ctx, pluginID)
	})
	// Google Ads Keyword Planner (backend_kind=google-ads-sdk). The developer
	// token is a secret sourced from the OS keychain (env fallback) per profile,
	// so it never travels as an invocation arg. One adapter instance serves all
	// three keyword-planning methods, keyed by binding adapter_key.
	gadsProfile := auth.DefaultAPIKeyProfile
	if name, err := profilepkg.Parse(profile); err == nil {
		gadsProfile = name.String()
	}
	gads := googleads.NewAdapter(func() string {
		return auth.LookupDeveloperToken(auth.NewOSKeyring(), gadsProfile)
	})
	return map[string]dispatch.Adapter{
		"code.risor":                                 cr,
		"rest.typed-rest-sdk":                        rest,
		"rest.discovery-rest":                        rest,
		"rest.raw-http":                              rest,
		"plugin.mcp":                                 pluginMCP,
		"googleads.generateKeywordIdeas":             gads,
		"googleads.generateKeywordHistoricalMetrics": gads,
		"googleads.generateKeywordForecastMetrics":   gads,
	}, cr
}

// newDefaultDispatcher constructs the dispatcher the way both `gum mcp --stdio`
// and CLI subcommands use it. Phase 11 wires the auth.CompositeResolver so
// catalog ops with auth_strategy=adc get a live Bearer token. After kernel
// construction, the CodeRunner is back-wired with the dispatcher so the
// gum_parallel builtin (spec §6.3) can fan out via the same lifecycle path.
//
// The audit sink (spec §11) writes to <data home>/gum/<profile>/audit.jsonl.
// The profile defaults to "default"; subcommands that override via the
// persistent --profile flag re-construct the dispatcher with the resolved
// profile when needed.
//
// Returns the bare Dispatcher for CLI one-shots where the process exits
// immediately after writing the response — the in-process synchronous Append
// is sufficient because there are no in-flight entries to drain.
func newDefaultDispatcher() dispatch.Dispatcher {
	return newDefaultDispatcherForProfile("default")
}

// newDefaultDispatcherForProfile is the profile-aware constructor. Falls
// back to a sinkless dispatcher when the profile dir is unwritable so a
// hostile filesystem (read-only volume, missing $HOME) does not block the
// CLI from running. Synchronous audit append (no buffered channel) — caller
// gets immediate persist semantics with no Close() to wire.
func newDefaultDispatcherForProfile(profile string) dispatch.Dispatcher {
	disp, _ := newDefaultDispatcherWithCloser(profile, false)
	return disp
}

// newDefaultCodeDispatcherForProfile is used by the top-level `gum code` CLI
// path. gum.code itself has auth_strategy=none, so a trivial local script must
// not block on OS-keyring profile scope loading before dispatch starts. Scripts
// that call scoped Google ops still route through the same dispatcher policy and
// fail closed without preloaded scopes.
func newDefaultCodeDispatcherForProfile(profile string) dispatch.Dispatcher {
	disp, _ := newDefaultDispatcherWithCloserAndScopeLoading(profile, false, false)
	return disp
}

// newDefaultDispatcherWithCloser is the long-running-process constructor
// (gum-dxpy). When buffered=true the audit writer runs an async drain
// goroutine with a 2s default drain timeout; the returned closer flushes
// queued entries on SIGTERM/SIGINT. When buffered=false, the closer is a
// no-op so callers can wire `defer closer()` unconditionally.
func newDefaultDispatcherWithCloser(profile string, buffered bool) (dispatch.Dispatcher, func() error) {
	return newDefaultDispatcherWithCloserAndScopeLoading(profile, buffered, true)
}

func newDefaultDispatcherWithCloserAndScopeLoading(profile string, buffered, loadProfileScopes bool) (dispatch.Dispatcher, func() error) {
	adapterMap, codeRunner := defaultAdapters(profile)
	name, nameErr := profilepkg.Parse(profile)
	profileName := ""
	profileDataDir := ""
	if nameErr == nil {
		profileName = name.String()
		if dir, err := name.DataDir(); err == nil {
			profileDataDir = dir
		}
	}
	scopeProfile := profile
	if profileName != "" {
		scopeProfile = profileName
	}
	// Bind the auth resolver to the ACTIVE profile so it loads the client and
	// reads the byo_oauth grant under the same per-profile key that `gum login`
	// stored them under (gum-2fu0). Without this the resolver defaults to
	// "default" regardless of --profile, stranding non-default-profile grants.
	authResolver := auth.NewDefaultCompositeResolver()
	authResolver.Profile = scopeProfile
	var allowedScopes []string
	if loadProfileScopes {
		allowedScopes = auth.ExpandGrantedScopes(auth.GrantedScopes(auth.NewOSKeyring(), scopeProfile))
	}
	cfg := dispatch.DispatcherConfig{
		Auth: authResolver,
		// Spec §10.3 semantic response cache. In-process for v0.1.0; the
		// per-profile persistent semantic.db lands in v0.2.0 — until then,
		// each gum process gets its own LRU+VAAC cache that lives only as
		// long as the process. Max entries chosen to bound memory at a few
		// MB for typical 1–10 KB Google-API responses.
		SemanticCache: defaultSemanticCache(),
		// Per-profile scope allowlist for policy gate 5. Sourced from the
		// scopes recorded at `gum login`; without this the gate sees an empty
		// allowlist and rejects every scoped op with SCOPE_MISSING (gum-n9yl).
		Policy: dispatch.ProfilePolicy{
			// Expand the granted set with subsumed scopes (e.g. gmail.metadata,
			// which login drops but gmail.readonly covers) so the exact-match
			// scope gate doesn't reject ops a broader grant already satisfies
			// (gum-yn22). dispatch can't import auth (cycle), so we expand here.
			AllowedScopes: allowedScopes,
		},
		ProfileName:           profileName,
		ConfirmationReplayDir: profileDataDir,
		// §9.2 catalog-embedded (third-layer) expression-profile resolver. The
		// kernel applies a variant's output_profile when no presentation-layer
		// override is set on the invocation. Shared by CLI and MCP (both build
		// the dispatcher here).
		ProfileLookup: outprofile.BuiltinLookup,
	}
	closer := func() error { return nil }
	if dir := profileDataDir; dir != "" {
		opts := auditOptionsForProfile(profileName, buffered)
		if buffered {
			opts = append(opts,
				auditlog.WithBufferedChannel(defaultAuditBufferSize),
			)
		}
		if w, werr := auditlog.New(dir, opts...); werr == nil {
			cfg.Audit = w
			if buffered {
				closer = w.Close
			}
		}
	}
	disp := dispatch.NewDispatcherWithConfig(loadCatalog(), adapterMap, cfg)
	codeRunner.WithDispatcher(disp)
	return disp, closer
}

func auditOptionsForProfile(profile string, buffered bool) []auditlog.Option {
	resolved := resolveAuditRuntimeConfig(profile)
	opts := []auditlog.Option{
		auditlog.WithMaxSizeBytes(resolved.maxSizeBytes),
		auditlog.WithMaxFiles(resolved.maxFiles),
		auditlog.WithRetentionDays(resolved.retentionDays),
		auditlog.WithUnbounded(resolved.unbounded),
	}
	if buffered {
		opts = append(opts, auditlog.WithDrainTimeout(resolved.drainTimeout))
	}
	return opts
}

func resolveAuditRuntimeConfig(profile string) auditRuntimeConfig {
	resolved := defaultAuditRuntimeConfig()
	cfg, _, err := config.Load(profile)
	if err != nil || cfg == nil {
		return resolved
	}
	if v, ok := cfg.Get("audit.max_size_mb"); ok {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed >= 0 {
			resolved.maxSizeBytes = parsed * 1024 * 1024
		}
	}
	if v, ok := cfg.Get("audit.max_files"); ok {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			resolved.maxFiles = parsed
		}
	}
	if v, ok := cfg.Get("audit.retention_days"); ok {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			resolved.retentionDays = parsed
		}
	}
	if v, ok := cfg.Get("audit.unbounded"); ok {
		if parsed, err := strconv.ParseBool(v); err == nil {
			resolved.unbounded = parsed
		}
	}
	if v, ok := cfg.Get("audit.drain_timeout_seconds"); ok {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			resolved.drainTimeout = time.Duration(parsed) * time.Second
		}
	}
	return resolved
}

// profileAuditDir returns <data home>/gum/<profile>. data home falls back to
// $HOME/.local/share when XDG_DATA_HOME is unset (XDG Base Dir spec).
func profileAuditDir(profile string) (string, error) {
	name, err := profilepkg.Parse(profile)
	if err != nil {
		return "", err
	}
	return name.DataDir()
}
