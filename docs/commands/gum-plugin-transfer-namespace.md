# `gum plugin transfer-namespace`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Updates the namespace_owner binding for <prefix> in the active profile's
plugins.lock, recording the prior owner in transfer_history and emitting an
audit.jsonl row.

Pass either --new-owner <name> to hand the prefix to a different owner string
(matching subsequent installs succeed without --dev-allow-namespace-conflict)
or --release to clear the binding entirely (any owner may then re-bind on the
next install). --yes is mandatory in both modes; this command is destructive
to the namespace lease and has no interactive prompt.

## Usage

```bash
gum plugin transfer-namespace <prefix> [flags]
```

## Parent

- [gum plugin](gum-plugin.md)

## Arguments

| Name | Help |
| --- | --- |
| `prefix` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--new-owner` | `string` |  | Hand the prefix to this namespace_owner string. Mutually exclusive with --release. |
| `--release` | `bool` | false | Clear the existing namespace_owner binding without assigning a new one. |
| `--yes` | `bool` | false | Acknowledge the non-interactive consent gate. Required. |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum plugin](gum-plugin.md)
- [Command index](README.md)
