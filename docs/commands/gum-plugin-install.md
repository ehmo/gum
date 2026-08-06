# `gum plugin install`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Installs a plugin through an atomic registry update: validates the manifest,
runs the namespace-ownership check against the active profile's
plugins.lock, and writes plugin-catalog.json + plugins.lock + plugin-state.json
through one fsync'd transaction.

Plugins are third-party subprocesses. Installing one trusts its executable and
manifest-declared capabilities; plugin-managed auth means the plugin, not GUM's
OAuth resolver, is responsible for any credentials it requests or forwards.
Review the source and manifest, then pass --yes to acknowledge that trust.

Use --dev-allow-namespace-conflict on a dev profile (set profile.is_dev=true via
gum config set) to install a plugin whose prefix collides with an existing
namespace_owner. Outside a dev profile the flag is ignored and the install
fails with PLUGIN_NAMESPACE_CONFLICT.

## Usage

```bash
gum plugin install <local-dir> [flags]
```

## Parent

- [gum plugin](gum-plugin.md)

## Arguments

| Name | Help |
| --- | --- |
| `local-dir` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--dev-allow-namespace-conflict` | `bool` | false | On a dev profile, allow installing a plugin whose prefix is already locked to a different namespace_owner. Ignored outside dev profiles. |
| `--yes` | `bool` | false | Acknowledge that the plugin subprocess and manifest-declared capabilities are trusted enough to install. |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum plugin](gum-plugin.md)
- [Command index](README.md)
