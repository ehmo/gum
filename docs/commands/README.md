# Commands

Every `gum` CLI command has a generated docs page. Product coverage is documented under [Operations by service](../services/); this index is for command names, flags, aliases, arguments, and generated help.

Generated pages: 63.

## Top-level commands

- [gum agents](gum-agents.md) - Install gum skills and MCP config for coding agents
- [gum auth](gum-auth.md) - Manage Google OAuth credentials
- [gum cache](gum-cache.md) - Inspect or clear the dispatcher response cache
- [gum call](gum-call.md) - gum call is the deterministic CLI entry point for catalog operations.
- [gum canary](gum-canary.md) - gum canary --plugin=<id> [--live] resolves the named plugin under the active install root, spawns it once via the plugin host, and reports the outcome as a stable JSON envelope on stdout. A failed canary surfaces SERVICE_DOWN.
- [gum catalog](gum-catalog.md) - Inspect the resolved catalog
- [gum code](gum-code.md) - Run a Risor v2 script in the gum sandbox.
- [gum completion](gum-completion.md) - Generate the autocompletion script for gum for the specified shell.
- [gum config](gum-config.md) - Read or write values in the active profile's config.toml. The active profile is selected via --profile (default: 'default').
- [gum describe](gum-describe.md) - Return the catalog entry for an op_id (with example_args)
- [gum destructive](gum-destructive.md) - Invoke a destructive op (requires a confirmation token)
- [gum doctor](gum-doctor.md) - Runs a single pre-flight that reports the health of each gum subsystem:
- [gum gain](gum-gain.md) - Print cumulative gain (token-savings) stats from the local ledger, or replay a fixture set with --fixture-replay.
- [gum help](gum-help.md) - Help provides help for any command in the application.
- [gum init](gum-init.md) - gum init bootstraps GUM for a new user or project. Default behavior is diff-and-prompt: gum init never silently patches a security-sensitive file. Use --target to pick the host (claude-code | claude-desktop | cursor). Use --refresh to regenerate GUM.md only after a gum upgrade.
- [gum login](gum-login.md) - Authorize gum (alias for `gum auth login`)
- [gum logout](gum-logout.md) - Clear gum's stored OAuth credentials (switch or sign out of a Google account)
- [gum mcp](gum-mcp.md) - Run the gum MCP server. The public release supports --stdio transport.
- [gum plugin](gum-plugin.md) - Manage gum plugins: install, list, run, and curate third-party subprocess
- [gum profile](gum-profile.md) - Validate or test an expression profile
- [gum read](gum-read.md) - Invoke a read-class catalog op
- [gum schema](gum-schema.md) - Print a JSON description of the active gum command tree, including command paths, aliases, arguments, and flags.
- [gum search](gum-search.md) - BM25 search the embedded catalog (TTY table, pipe JSON)
- [gum setup](gum-setup.md) - Guided setup for a local gum install. It writes agent skills and MCP config for Codex, Claude, Cursor, or Gemini, then prints the Google OAuth and doctor commands needed for first success.
- [gum skills](gum-skills.md) - List, print, export, or install version-matched agent skills
- [gum version](gum-version.md) - Print the gum version.
- [gum write](gum-write.md) - Invoke a write-class catalog op. --allow-write is required for the policy gate to admit the dispatch.

## All commands

- [gum](gum.md) - gum is a single Go binary that exposes the same dispatch kernel via a CLI surface and an MCP stdio server. See the public docs for setup, safety, and command reference.
  - [gum agents](gum-agents.md) - Install gum skills and MCP config for coding agents
    - [gum agents install](gum-agents-install.md) - Install gum agent files
  - [gum auth](gum-auth.md) - Manage Google OAuth credentials
    - [gum auth login](gum-auth-login.md) - Authorize gum via your OAuth client (loopback + PKCE; no gcloud)
    - [gum auth probe](gum-auth-probe.md) - Acquire a token for --scopes and print non-secret metadata
    - [gum auth setup](gum-auth-setup.md) - Walk the credential prerequisites for an operation
    - [gum auth status](gum-auth-status.md) - Print resolved auth provider and scope coverage
    - [gum auth use-ads-developer-token](gum-auth-use-ads-developer-token.md) - Store the Google Ads API developer token in the OS keychain so the
    - [gum auth use-api-key](gum-auth-use-api-key.md) - Configure the api_key auth strategy
    - [gum auth use-oauth-client](gum-auth-use-oauth-client.md) - Register a Desktop-app OAuth client you created in the Google Cloud console.
    - [gum auth use-service-account](gum-auth-use-service-account.md) - Configure the service_account_key auth strategy
  - [gum cache](gum-cache.md) - Inspect or clear the dispatcher response cache
    - [gum cache clear](gum-cache-clear.md) - Clear the dispatcher response cache
    - [gum cache migrate](gum-cache-migrate.md) - Migrate BoltDB cache (http.db) to WAL-SQLite (http-wal.db)
    - [gum cache stats](gum-cache-stats.md) - Print dispatcher cache stats
  - [gum call](gum-call.md) - gum call is the deterministic CLI entry point for catalog operations.
  - [gum canary](gum-canary.md) - gum canary --plugin=<id> [--live] resolves the named plugin under the active install root, spawns it once via the plugin host, and reports the outcome as a stable JSON envelope on stdout. A failed canary surfaces SERVICE_DOWN.
  - [gum catalog](gum-catalog.md) - Inspect the resolved catalog
    - [gum catalog list-overrides](gum-catalog-list-overrides.md) - List all variants with risk_override=true from the resolved catalog
  - [gum code](gum-code.md) - Run a Risor v2 script in the gum sandbox.
  - [gum completion](gum-completion.md) - Generate the autocompletion script for gum for the specified shell.
    - [gum completion bash](gum-completion-bash.md) - Generate the autocompletion script for the bash shell.
    - [gum completion fish](gum-completion-fish.md) - Generate the autocompletion script for the fish shell.
    - [gum completion powershell](gum-completion-powershell.md) - Generate the autocompletion script for powershell.
    - [gum completion zsh](gum-completion-zsh.md) - Generate the autocompletion script for the zsh shell.
  - [gum config](gum-config.md) - Read or write values in the active profile's config.toml. The active profile is selected via --profile (default: 'default').
    - [gum config get](gum-config-get.md) - Print the value of a config key from the active profile
    - [gum config list](gum-config-list.md) - List all config keys in the active profile
    - [gum config set](gum-config-set.md) - Persist a config key=value pair to the active profile
    - [gum config unset](gum-config-unset.md) - Remove a config key from the active profile
  - [gum describe](gum-describe.md) - Return the catalog entry for an op_id (with example_args)
  - [gum destructive](gum-destructive.md) - Invoke a destructive op (requires a confirmation token)
  - [gum doctor](gum-doctor.md) - Runs a single pre-flight that reports the health of each gum subsystem:
  - [gum gain](gum-gain.md) - Print cumulative gain (token-savings) stats from the local ledger, or replay a fixture set with --fixture-replay.
  - [gum help](gum-help.md) - Help provides help for any command in the application.
  - [gum init](gum-init.md) - gum init bootstraps GUM for a new user or project. Default behavior is diff-and-prompt: gum init never silently patches a security-sensitive file. Use --target to pick the host (claude-code | claude-desktop | cursor). Use --refresh to regenerate GUM.md only after a gum upgrade.
  - [gum login](gum-login.md) - Authorize gum (alias for `gum auth login`)
  - [gum logout](gum-logout.md) - Clear gum's stored OAuth credentials (switch or sign out of a Google account)
  - [gum mcp](gum-mcp.md) - Run the gum MCP server. The public release supports --stdio transport.
  - [gum plugin](gum-plugin.md) - Manage gum plugins: install, list, run, and curate third-party subprocess
    - [gum plugin install](gum-plugin-install.md) - Installs a plugin through an atomic registry update: validates the manifest,
    - [gum plugin list](gum-plugin-list.md) - List installed plugins with their quarantine state
    - [gum plugin reload](gum-plugin-reload.md) - Clears any quarantine state for the named plugin, then spawns the subprocess once via the supervisor to act as a passive canary. A spawn failure re-quarantines the plugin.
    - [gum plugin remove](gum-plugin-remove.md) - Remove a plugin by ID
    - [gum plugin run](gum-plugin-run.md) - Call a tool on a running plugin
    - [gum plugin setup](gum-plugin-setup.md) - Reads the plugin's credential_descriptors from its manifest, prompts for
    - [gum plugin transfer-namespace](gum-plugin-transfer-namespace.md) - Updates the namespace_owner binding for <prefix> in the active profile's
    - [gum plugin unquarantine](gum-plugin-unquarantine.md) - Resets quarantined, retry_count, backoff_step, and next_retry_at in plugin-state.json so the plugin can be invoked on the next call. Use when the operator has independently verified the plugin is healthy and wants to bypass the exponential-backoff window.
  - [gum profile](gum-profile.md) - Validate or test an expression profile
    - [gum profile test](gum-profile-test.md) - When --input is set, applies the profile to that file (optionally comparing against --golden). When --input is omitted, runs every [[tests]] fixture in the profile file through the expression pipeline and prints a ProfileFixtureResult[] JSON envelope (--format=json).
    - [gum profile validate](gum-profile-validate.md) - Parse an expression-profile DSL file and report any errors. Use this in CI to catch malformed catalog profiles before release.
  - [gum read](gum-read.md) - Invoke a read-class catalog op
  - [gum schema](gum-schema.md) - Print a JSON description of the active gum command tree, including command paths, aliases, arguments, and flags.
  - [gum search](gum-search.md) - BM25 search the embedded catalog (TTY table, pipe JSON)
  - [gum setup](gum-setup.md) - Guided setup for a local gum install. It writes agent skills and MCP config for Codex, Claude, Cursor, or Gemini, then prints the Google OAuth and doctor commands needed for first success.
  - [gum skills](gum-skills.md) - List, print, export, or install version-matched agent skills
    - [gum skills export](gum-skills-export.md) - Export installable gum skills
    - [gum skills install](gum-skills-install.md) - Install gum skills for Codex-compatible agents
    - [gum skills list](gum-skills-list.md) - List embedded gum agent skills
    - [gum skills show](gum-skills-show.md) - Print an embedded gum skill
  - [gum version](gum-version.md) - Print the gum version.
  - [gum write](gum-write.md) - Invoke a write-class catalog op. --allow-write is required for the policy gate to admit the dispatch.
