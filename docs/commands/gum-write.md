# `gum write`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Invoke a write-class catalog op. --allow-write is required for the policy gate to admit the dispatch.

## Usage

```bash
gum write <op_id> [flags]
```

## Parent

- [gum](gum.md)

## Arguments

| Name | Help |
| --- | --- |
| `op_id` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--allow-write` | `bool` | false | Authorise this write |
| `--args` | `string` |  | JSON object of op arguments |
| `--format` | `string` |  | Output format (toon\|json\|raw) |
| `-o`<br>`--output` | `string` |  | Human output format: table\|json\|toon\|csv\|markdown\|raw\|value(<path>) (default: kernel TOON) |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)
