# `gum`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

gum is a single Go binary that exposes the same dispatch kernel via a CLI surface and an MCP stdio server. See the public docs for setup, safety, and command reference.

Version note: release builds report a semver tag. Local source builds report "dev" unless main.version is injected with release ldflags. To check for updates, run `gum config set notify.enabled=true`.

## Usage

```bash
gum [flags]
```

## Subcommands

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

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [Command index](README.md)
