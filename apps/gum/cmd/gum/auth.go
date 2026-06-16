package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/spf13/cobra"
)

// newAuthCmd implements `gum auth status|login|setup|use-*`.
func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Google OAuth credentials",
	}
	parentHelpOnly(cmd)
	cmd.AddCommand(newAuthStatusCmd(), newAuthLoginCmd(), newAuthProbeCmd(), newAuthUseAPIKeyCmd(), newAuthUseServiceAccountCmd(), newAuthUseOAuthClientCmd(), newAuthUseAdsDeveloperTokenCmd(), newAuthSetupCmd())
	return cmd
}

// newAuthUseOAuthClientCmd registers a user-supplied ("bring your own")
// Desktop OAuth client so gum can run the loopback+PKCE login flow itself —
// no gcloud, no managed client. The client_id is not secret and
// is accepted as a flag; the client_secret (when the client has one) is read
// from stdin or a file so it never lands in shell history or `ps`. Public
// PKCE clients have no secret and need neither flag.
func newAuthUseOAuthClientCmd() *cobra.Command {
	var (
		clientID    string
		secretStdin bool
		secretFile  string
		profile     string
	)
	cmd := &cobra.Command{
		Use:   "use-oauth-client --client-id <id> [--secret-stdin | --secret-file <path>]",
		Short: "Register your own Google OAuth client for the byo_oauth strategy (spec §7)",
		Long: "Register a Desktop-app OAuth client you created in the Google Cloud console.\n" +
			"gum then runs the browser login itself (loopback + PKCE) and refreshes tokens\n" +
			"automatically — no gcloud dependency. Create one at:\n" +
			"  https://console.cloud.google.com/apis/credentials → Create credentials → OAuth client ID → Desktop app\n" +
			"Google issues a client_secret for Desktop-app clients and its token endpoint REQUIRES it\n" +
			"even with PKCE, so pipe the secret via --secret-stdin (it never enters shell history).\n" +
			"Only a true public client — rare for Google — may omit the secret.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientID = strings.TrimSpace(clientID)
			if clientID == "" {
				return fmt.Errorf("CLI_ARG_INVALID: --client-id is required (the OAuth client ID from the Google Cloud console)")
			}
			secret, serr := readClientSecret(cmd, secretStdin, secretFile)
			if serr != nil {
				return serr
			}
			// Honor the root persistent --profile (gum-s985): use the shared
			// resolver instead of a shadowing local flag.
			profile = resolveProfileFlag(cmd)
			out := cmd.OutOrStdout()
			if err := auth.StoreByoClient(auth.NewOSKeyring(), profile, auth.ByoClient{ClientID: clientID, ClientSecret: secret}); err != nil {
				return fmt.Errorf("gum auth use-oauth-client: %w", err)
			}
			_, _ = fmt.Fprintf(out, "gum auth use-oauth-client: stored OAuth client in OS keychain under profile %q.\n", profile)
			if secret == "" {
				_, _ = fmt.Fprintln(out, "(public PKCE client — no secret stored)")
			}
			_, _ = fmt.Fprintln(out, "Next: run `gum login` to authorize, or just run a `gum call` and approve when prompted.")
			return nil
		},
	}
	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth client ID from the Google Cloud console (required)")
	cmd.Flags().BoolVar(&secretStdin, "secret-stdin", false, "Read the client secret from stdin (kept out of shell history)")
	cmd.Flags().StringVar(&secretFile, "secret-file", "", "Read the client secret from this file")
	return cmd
}

// readClientSecret returns the OAuth client secret from --secret-file or
// --secret-stdin. When neither is requested the client is treated as a public
// PKCE client and the secret is empty. The secret is never accepted as a flag
// value to keep it out of shell history and the process listing.
func readClientSecret(cmd *cobra.Command, fromStdin bool, fromFile string) (string, error) {
	if fromFile != "" {
		b, err := os.ReadFile(fromFile)
		if err != nil {
			return "", fmt.Errorf("gum auth use-oauth-client: read --secret-file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	if !fromStdin {
		return "", nil
	}
	b, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 1<<20))
	if err != nil {
		return "", fmt.Errorf("gum auth use-oauth-client: read stdin: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// newAuthSetupCmd is the spec §7 (lines 1198, 1281, 1285, 1344, 1397-1398)
// canonical entry point for compound-auth, byo_oauth, and non-gum_oauth
// strategies. v0.1.0 prints the structured envelope shape an LLM/user would
// see at dispatch time so the operator can preview the components required
// for an op_id before invoking it. The per-component walk-through lands
// alongside the catalog component records (post-v0.1).
func newAuthSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "setup <op_id>",
		Short:         "Walk the credential prerequisites for an operation (spec §7)",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opID := strings.TrimSpace(args[0])
			if opID == "" {
				return fmt.Errorf("gum auth setup: <op_id> is empty")
			}
			envelope := &auth.AuthError{
				Code:              "AUTH_REQUIRED",
				Strategy:          "compound",
				OpID:              opID,
				MissingComponents: []string{"see_setup_command"},
				SetupCommand:      "gum auth setup " + opID,
				UserMessage:       "Compound auth for " + opID + " walks each declared component. v0.1.0 prints the envelope; per-component prompts land with the catalog component records.",
				HumanRemediation:  "Run `gum auth use-oauth-client`, `gum auth use-api-key`, or `gum auth use-service-account` as the variant requires; see spec.md §7.",
				Retryable:         false,
			}
			return writeJSON(cmd.OutOrStdout(), envelope)
		},
	}
}

// newAuthUseServiceAccountCmd prints the export line for the
// GUM_SERVICE_ACCOUNT_KEY env variable. Like use-api-key it is the v0.1.0
// surface for spec §7's "key stored in keychain" intent; the keychain
// backing lands with gum-0wv.
func newAuthUseServiceAccountCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use-service-account <key.json>",
		Short: "Configure the service_account_key auth strategy (spec §7)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := strings.TrimSpace(args[0])
			if path == "" {
				return fmt.Errorf("gum auth use-service-account: <key.json> is empty")
			}
			abs, aerr := filepath.Abs(path)
			if aerr != nil {
				return fmt.Errorf("gum auth use-service-account: resolve path: %w", aerr)
			}
			if _, serr := os.Stat(abs); serr != nil {
				return fmt.Errorf("gum auth use-service-account: cannot stat %q: %w", abs, serr)
			}
			// Validate the JSON shape before printing so the operator
			// gets the error at the configure step rather than at first
			// dispatch. NewServiceAccountResolver does the parse without
			// hitting the network.
			if _, perr := auth.NewServiceAccountResolver(abs); perr != nil {
				return fmt.Errorf("gum auth use-service-account: %w", perr)
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, "gum auth use-service-account: v0.1.0 storage is environment-based.")
			_, _ = fmt.Fprintln(out, "Add this line to your shell profile (zshrc, bashrc, etc.):")
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintf(out, "  export %s=%q\n", auth.EnvServiceAccountKeyVar, abs)
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Keychain storage lands with the gum auth keychain backend in v0.2.0.")
			return nil
		},
	}
}

// newAuthUseAPIKeyCmd is the operator-facing surface for the api_key
// strategy (spec §7 line 1202, gum-6hcr).
//
// The key is NEVER accepted as a positional argv — that would leak it via
// shell history (~/.zsh_history) and the process listing (`ps -ef`).
// Instead the operator pipes the key via --stdin (default) or points to a
// file via --from-file. When the OS keychain is available the key is
// persisted under the per-profile entry and nothing is echoed on stdout.
// Without a keychain backend the operator is instructed to set
// GUM_API_KEY themselves (env-var instructions only — the key bytes still
// never traverse the gum process).
func newAuthUseAPIKeyCmd() *cobra.Command {
	var (
		fromStdin bool
		fromFile  string
		profile   string
	)
	cmd := &cobra.Command{
		Use:           "use-api-key",
		Short:         "Configure the api_key auth strategy (spec §7)",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("CLI_ARG_INVALID: gum auth use-api-key does not accept positional arguments (the key would leak into shell history and `ps`); pipe the key via `--stdin` or point at a file with `--from-file <path>`")
			}
			key, err := readAPIKey(cmd, fromStdin, fromFile)
			if err != nil {
				return err
			}
			if key == "" {
				return fmt.Errorf("CLI_ARG_INVALID: api key is empty (read from --stdin/--from-file)")
			}
			// Honor the root persistent --profile (gum-s985): use the shared
			// resolver instead of a shadowing local flag.
			profile = resolveProfileFlag(cmd)
			out := cmd.OutOrStdout()
			if serr := auth.StoreAPIKey(auth.NewOSKeyring(), profile, key); serr != nil {
				// Keychain backend missing: fall back to env-var instructions.
				// Crucially we still DO NOT print the key bytes — the operator
				// already has them and can set the env var themselves.
				_, _ = fmt.Fprintln(out, "gum auth use-api-key: OS keychain backend unavailable on this platform.")
				_, _ = fmt.Fprintf(out, "Set the %s env variable manually (do not paste it in shell history — use a here-string or password manager):\n", auth.EnvAPIKeyVar)
				_, _ = fmt.Fprintln(out)
				_, _ = fmt.Fprintf(out, "  export %s=$(your-password-manager get gum-api-key)\n", auth.EnvAPIKeyVar)
				_, _ = fmt.Fprintln(out)
				return nil
			}
			_, _ = fmt.Fprintf(out, "gum auth use-api-key: stored in OS keychain under profile %q.\n", profile)
			_, _ = fmt.Fprintln(out, "Verify with: gum auth status")
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read the API key from stdin (default when no --from-file is given)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Read the API key from this file (alternative to --stdin)")
	return cmd
}

// readAPIKey returns the api key bytes from --stdin or --from-file. Trims
// the surrounding whitespace so a piped `echo` (trailing newline) works.
func readAPIKey(cmd *cobra.Command, fromStdin bool, fromFile string) (string, error) {
	if fromFile != "" {
		b, err := os.ReadFile(fromFile)
		if err != nil {
			return "", fmt.Errorf("gum auth use-api-key: read --from-file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	// --stdin is the implicit default when no --from-file is given.
	_ = fromStdin
	b, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 1<<20))
	if err != nil {
		return "", fmt.Errorf("gum auth use-api-key: read stdin: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// newAuthUseAdsDeveloperTokenCmd stores the Google Ads API developer token in
// the OS keychain (env fallback GUM_GOOGLE_ADS_DEVELOPER_TOKEN) so the
// googleads.* Keyword Planner ops can send it as the developer-token header.
//
// The developer token is a secret and, like the api key, is NEVER accepted as a
// positional argv (shell history / `ps` leak) and NEVER travels as an
// invocation arg. It is the developer-token *component* of the googleads ops'
// compound auth; the OAuth Bearer is the separate adwords-scope grant from
// `gum auth use-oauth-client` + `gum login --service googleads`.
func newAuthUseAdsDeveloperTokenCmd() *cobra.Command {
	var (
		fromStdin bool
		fromFile  string
		profile   string
	)
	cmd := &cobra.Command{
		Use:   "use-ads-developer-token",
		Short: "Store the Google Ads API developer token for the googleads Keyword Planner ops",
		Long: "Store the Google Ads API developer token in the OS keychain so the\n" +
			"googleads.* Keyword Planner ops can send it as the developer-token header.\n" +
			"The token is a secret: pipe it via --stdin (default) or --from-file; it is\n" +
			"never accepted as a positional argument and never sent as an invocation arg.\n" +
			"The OAuth Bearer is separate — run `gum auth use-oauth-client` then\n" +
			"`gum login --service googleads` to authorize the adwords scope.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("CLI_ARG_INVALID: gum auth use-ads-developer-token does not accept positional arguments (the token would leak into shell history and `ps`); pipe it via `--stdin` or `--from-file <path>`")
			}
			tok, err := readDeveloperToken(cmd, fromFile)
			if err != nil {
				return err
			}
			if tok == "" {
				return fmt.Errorf("CLI_ARG_INVALID: developer token is empty (read from --stdin/--from-file)")
			}
			// Honor the root persistent --profile (gum-s985): use the shared
			// resolver instead of a shadowing local flag.
			profile = resolveProfileFlag(cmd)
			out := cmd.OutOrStdout()
			if serr := auth.StoreDeveloperToken(auth.NewOSKeyring(), profile, tok); serr != nil {
				_, _ = fmt.Fprintln(out, "gum auth use-ads-developer-token: OS keychain backend unavailable on this platform.")
				_, _ = fmt.Fprintf(out, "Set the %s env variable manually instead (keep it out of shell history):\n", auth.EnvGoogleAdsDeveloperToken)
				_, _ = fmt.Fprintln(out)
				_, _ = fmt.Fprintf(out, "  export %s=$(your-password-manager get gum-ads-developer-token)\n", auth.EnvGoogleAdsDeveloperToken)
				_, _ = fmt.Fprintln(out)
				return nil
			}
			_, _ = fmt.Fprintf(out, "gum auth use-ads-developer-token: stored in OS keychain under profile %q.\n", profile)
			_, _ = fmt.Fprintln(out, "Next: `gum auth use-oauth-client` (if not already) then `gum login --service googleads`.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read the developer token from stdin (default when no --from-file is given)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Read the developer token from this file (alternative to --stdin)")
	return cmd
}

// readDeveloperToken returns the developer token from --stdin or --from-file,
// trimming surrounding whitespace so a piped `echo` (trailing newline) works.
func readDeveloperToken(cmd *cobra.Command, fromFile string) (string, error) {
	if fromFile != "" {
		b, err := os.ReadFile(fromFile)
		if err != nil {
			return "", fmt.Errorf("gum auth use-ads-developer-token: read --from-file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	b, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 1<<20))
	if err != nil {
		return "", fmt.Errorf("gum auth use-ads-developer-token: read stdin: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// newAuthStatusCmd reports which ADC source is active and which scopes the
// resolver currently advertises.
func newAuthStatusCmd() *cobra.Command {
	var scopesFlag []string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print resolved auth provider and scope coverage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			status := collectADCStatus(cmd.Context(), scopesFlag)
			return writeJSON(cmd.OutOrStdout(), status)
		},
	}
	cmd.Flags().StringSliceVar(&scopesFlag, "scopes", []string{"gmail.readonly"}, "Catalog scopes to probe")
	return cmd
}

// newAuthProbeCmd attempts to acquire a token for the given scopes via the
// composite resolver and prints non-secret metadata (no Bearer token).
func newAuthProbeCmd() *cobra.Command {
	var scopesFlag []string
	cmd := &cobra.Command{
		Use:   "probe",
		Short: "Acquire a token for --scopes and print non-secret metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolver := auth.NewLiveADCResolver()
			creds, err := resolver.Resolve(cmd.Context(), scopesFlag)
			if err != nil {
				return err
			}
			// gum-4gey.12: echo the requested scope list alongside the
			// granted scopes. They differ in two failure-adjacent cases the
			// operator needs to see: (a) ADC silently drops unknown scopes,
			// (b) byo_oauth refresh returns the cached union, not the new
			// ask. Surfacing both makes "scope drift" diagnosable from a
			// single command.
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"strategy":         creds.StrategyName,
				"scopes":           creds.Scopes,
				"scopes_requested": scopesFlag,
				"token_bytes":      len(creds.Token),
				"expires_at":       creds.ExpiresAt.UTC().Format(time.RFC3339),
			})
		},
	}
	cmd.Flags().StringSliceVar(&scopesFlag, "scopes", []string{"gmail.readonly"}, "Scopes to acquire")
	return cmd
}

// newAuthLoginCmd runs the interactive byo_oauth login: the loopback + PKCE +
// CSRF-state flow against the operator's own OAuth client (registered via
// `gum auth use-oauth-client`). No --scope is required — with none given it
// pre-authorizes the full catalog scope set in a single consent screen so
// later `gum call`s never prompt. There is no gcloud dependency and no built-in
// managed OAuth fallback in v1.
func newAuthLoginCmd() *cobra.Command {
	return newLoginCmd("login", "Authorize gum via your OAuth client (loopback + PKCE; no gcloud)")
}

// newTopLevelLoginCmd is the `gum login` alias for `gum auth login` — the
// one-keystroke entry point the ergonomics redesign calls for.
func newTopLevelLoginCmd() *cobra.Command {
	return newLoginCmd("login", "Authorize gum (alias for `gum auth login`)")
}

// newLoginCmd builds the shared login command body for both `gum auth login`
// and the top-level `gum login` alias.
func newLoginCmd(use, short string) *cobra.Command {
	var scopes []string
	var services []string
	var allScopes bool
	var noBrowser bool
	cmd := &cobra.Command{
		Use:           use,
		Short:         short,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd, scopes, services, allScopes, noBrowser)
		},
	}
	cmd.Flags().StringSliceVar(&scopes, "scope", nil, "Exact OAuth scope(s) to request; repeat or comma-separate. Overrides --service/--all.")
	cmd.Flags().StringSliceVar(&services, "service", nil, "Request only these services' scopes (e.g. --service people,youtube). Comma-separate or repeat.")
	cmd.Flags().BoolVar(&allScopes, "all", false, "Request the full catalog scope union (every service). Default is the core Workspace set; the full union needs all those APIs enabled on your OAuth client.")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the URL but don't launch a browser (use for SSH/devcontainer/headless)")
	return cmd
}

// runLogin loads the registered BYO OAuth client and runs the interactive
// loopback+PKCE flow for the resolved scopes. Pre-authorizing the whole
// catalog (no --scope) or a specific subset (--scope) both land here. When no
// client is configured the operator is pointed at `gum auth use-oauth-client`.
func runLogin(cmd *cobra.Command, explicitScopes, services []string, allScopes, noBrowser bool) error {
	profile := resolveProfileFlag(cmd)
	client, ok, err := auth.LoadByoClient(auth.NewOSKeyring(), profile)
	if err != nil {
		return fmt.Errorf("gum login: read OAuth client from keychain: %w", err)
	}
	if !ok {
		return fmt.Errorf("gum login: no OAuth client configured. Create a Desktop OAuth client at https://console.cloud.google.com/apis/credentials, then run `gum auth use-oauth-client --client-id <id> --secret-stdin`")
	}
	scopes, err := resolveLoginScopes(loadCatalog(), explicitScopes, services, allScopes)
	if err != nil {
		return err
	}
	creds, err := interactiveByoLogin(cmd.Context(), auth.ByoOAuthConfig{
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		Profile:      profile,
		Scopes:       scopes,
	}, newBrowserOpener(cmd.ErrOrStderr(), noBrowser, isHeadless))
	if err != nil {
		return err
	}
	return writeJSON(cmd.OutOrStdout(), map[string]any{
		"strategy":            creds.StrategyName,
		"scopes":              creds.Scopes,
		"subject_fingerprint": creds.SubjectFingerprint,
		"token_bytes":         len(creds.Token),
		"expires_at":          creds.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// interactiveByoLogin builds a BYO resolver from cfg and runs its loopback +
// PKCE Login, opening the browser via opener. It is a package var so tests can
// stub the browser/network round-trip, and so the just-in-time auth path
// (gum call) can share the exact same login core.
var interactiveByoLogin = func(ctx context.Context, cfg auth.ByoOAuthConfig, opener func(string) error) (*auth.Credentials, error) {
	b := auth.NewDefaultByoOAuth(cfg)
	b.BrowserOpener = opener
	return b.Login(ctx)
}

// coreLoginServices is the default scope footprint for `gum login`: the
// original Workspace surface a typical BYO OAuth app already has enabled +
// consented. The full breadth catalog (~27 services / 60 scopes) is opt-in via
// --all or --service, because asking a BYO consent screen to grant scopes for
// APIs the project never enabled fails the WHOLE request ("Something went
// wrong"). Keeping the default lean keeps `gum login` working out of the box.
var coreLoginServices = []string{
	"gmail", "calendar", "drive", "docs", "sheets", "slides", "tasks",
	"searchconsole", "admin",
}

// resolveLoginScopes returns the scope set for an interactive login:
//   - explicit --scope values win (normalised to full URLs);
//   - else --service <names> selects just those services' scopes;
//   - else --all requests the whole catalog union;
//   - else (the default) the lean coreLoginServices set.
//
// The catalog is passed in (rather than loaded here) so the derivation is
// unit-testable without the embedded snapshot.
func resolveLoginScopes(cat *catalog.Catalog, explicit, services []string, all bool) ([]string, error) {
	if len(explicit) > 0 {
		return auth.NormaliseScopes(explicit), nil
	}
	if cat == nil {
		return nil, fmt.Errorf("gum login: catalog unavailable; pass --scope explicitly")
	}
	var scopes []string
	switch {
	case len(services) > 0:
		scopes = cat.ScopesForServices(services)
		if len(scopes) == 0 {
			return nil, fmt.Errorf("gum login: no OAuth scopes for service(s) %v — check names with `gum describe` or pass --scope explicitly", services)
		}
	case all:
		scopes = cat.AllScopes()
	default:
		scopes = cat.ScopesForServices(coreLoginServices)
	}
	if len(scopes) == 0 {
		return nil, fmt.Errorf("gum login: catalog declares no scopes; pass --scope explicitly")
	}
	// Drop scopes subsumed by a broader one in the union — notably the poisonous
	// gmail.metadata, whose presence makes Google reject messages.get?format=full
	// even alongside gmail.readonly. The ops that declared it (getProfile,
	// history.list) still resolve via scopesSatisfied's subsumption check.
	return auth.PruneLoginScopes(scopes), nil
}

// newBrowserOpener returns a BrowserOpener that ALWAYS prints the
// authorization URL to stderr before launching the helper. headless and
// noBrowser short-circuit the launch so a stub xdg-open / missing DISPLAY
// never hangs the flow with no recovery path (spec gum-4v5o).
func newBrowserOpener(stderr io.Writer, noBrowser bool, headlessFn func() bool) func(string) error {
	return func(authURL string) error {
		_, _ = fmt.Fprintf(stderr, "Open this URL to authenticate:\n  %s\n\n", authURL)
		if noBrowser {
			_, _ = fmt.Fprintln(stderr, "--no-browser set; not launching a browser.")
			return nil
		}
		if headlessFn != nil && headlessFn() {
			_, _ = fmt.Fprintln(stderr, "No display detected (headless); not launching a browser.")
			return nil
		}
		if err := launchBrowser(authURL); err != nil {
			// Print the failure so the user knows to fall back to copy/paste,
			// but DO NOT fail the flow — the URL is already visible and the
			// callback listener is running.
			_, _ = fmt.Fprintf(stderr, "Browser launch failed: %v (the URL above still works)\n", err)
		}
		return nil
	}
}

// launchBrowser invokes the platform helper. Errors here are reported but
// non-fatal — the printed URL is the recovery path.
func launchBrowser(authURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", authURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", authURL)
	default:
		cmd = exec.Command("xdg-open", authURL)
	}
	return cmd.Start()
}

// isHeadless detects a Linux system with no graphical session. macOS and
// Windows always have a usable session API, so the heuristic is Linux-only
// (WSL has DISPLAY==” but wslview works — we look for /proc/version
// substrings to exempt it).
func isHeadless() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return false
	}
	if b, err := os.ReadFile("/proc/version"); err == nil {
		v := strings.ToLower(string(b))
		if strings.Contains(v, "microsoft") || strings.Contains(v, "wsl") {
			return false
		}
	}
	return true
}

// adcStatus is the JSON shape printed by `gum auth status`.
type adcStatus struct {
	Provider             string   `json:"provider"`
	Source               string   `json:"source"`
	ScopesRequested      []string `json:"scopes_requested"`
	GoogleAppCredentials string   `json:"google_application_credentials,omitempty"`
	GcloudCachePath      string   `json:"gcloud_cache_path,omitempty"`
	GcloudCachePresent   bool     `json:"gcloud_cache_present"`
	Hint                 string   `json:"hint,omitempty"`
}

// collectADCStatus inspects the environment without making a network call so
// `gum auth status` is safe to run anywhere.
func collectADCStatus(ctx context.Context, scopes []string) adcStatus {
	out := adcStatus{
		Provider:        "adc",
		ScopesRequested: scopes,
	}
	if v := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); v != "" {
		out.GoogleAppCredentials = v
		out.Source = "GOOGLE_APPLICATION_CREDENTIALS"
	}
	home, _ := os.UserHomeDir()
	gcloudPath := filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
	out.GcloudCachePath = gcloudPath
	if _, err := os.Stat(gcloudPath); err == nil {
		out.GcloudCachePresent = true
		if out.Source == "" {
			out.Source = "gcloud_cache"
		}
	}
	if out.Source == "" {
		out.Hint = "run `gcloud auth application-default login` or set GOOGLE_APPLICATION_CREDENTIALS"
	}
	return out
}
