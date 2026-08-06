# `gum destructive`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Invoke a destructive op (requires a confirmation token)

## Usage

```bash
gum destructive <op_id> [flags]
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
| `--args` | `string` |  | JSON object of op arguments |
| `--confirmed` | `bool` | false | Set the confirmed flag |
| `--format` | `string` |  | Output format (toon\|json\|raw) |
| `-o`<br>`--output` | `string` |  | Human output format: table\|json\|toon\|csv\|markdown\|raw\|value(<path>) (default: kernel TOON) |
| `--token` | `string` |  | HMAC-SHA256 confirmation token |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)
