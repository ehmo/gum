# `gum plugin`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Manage gum plugins: install, list, run, and curate third-party subprocess
plugins. Installing a plugin launches an untrusted subprocess, so 'install'
requires --yes to acknowledge that trust boundary. See the subcommands below.

## Usage

```bash
gum plugin
```

## Parent

- [gum](gum.md)

## Subcommands

- [gum plugin install](gum-plugin-install.md) - Installs a plugin through an atomic registry update: validates the manifest,
- [gum plugin list](gum-plugin-list.md) - List installed plugins with their quarantine state
- [gum plugin reload](gum-plugin-reload.md) - Clears any quarantine state for the named plugin, then spawns the subprocess once via the supervisor to act as a passive canary. A spawn failure re-quarantines the plugin.
- [gum plugin remove](gum-plugin-remove.md) - Remove a plugin by ID
- [gum plugin run](gum-plugin-run.md) - Call a tool on a running plugin
- [gum plugin setup](gum-plugin-setup.md) - Reads the plugin's credential_descriptors from its manifest, prompts for
- [gum plugin transfer-namespace](gum-plugin-transfer-namespace.md) - Updates the namespace_owner binding for <prefix> in the active profile's
- [gum plugin unquarantine](gum-plugin-unquarantine.md) - Resets quarantined, retry_count, backoff_step, and next_retry_at in plugin-state.json so the plugin can be invoked on the next call. Use when the operator has independently verified the plugin is healthy and wants to bypass the exponential-backoff window.

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)
