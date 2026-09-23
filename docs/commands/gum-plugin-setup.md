# `gum plugin setup`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Reads the plugin's credential_descriptors from its manifest, prompts for
each missing credential by display_name and setup_hint, stores secrets in the
OS keychain, then runs
'gum canary --plugin=<name> --live' to verify the plugin is functional.

Typed secrets are not echoed when stdin is a terminal. Piping the secrets in,
one line per descriptor in manifest order, also works.

On canary success the plugin state is set to 'active'.
On canary failure the plugin is quarantined with CANARY_FAILED;
run 'gum plugin reload <name>' after correcting credentials.

## Usage

```bash
gum plugin setup <name>
```

## Parent

- [gum plugin](gum-plugin.md)

## Arguments

| Name | Help |
| --- | --- |
| `name` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--no-warn-lossy` | `bool` | false | Suppress the OVERRIDE_DISABLES_LOSSY_STAGE warning when a profile override drops a lossy-compression stage |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum plugin](gum-plugin.md)
- [Command index](README.md)
