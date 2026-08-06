# `gum call`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

gum call is the deterministic CLI entry point for catalog operations.
Positional args use key=value, key:=json, and @file forms. The risk
flag MUST match the resolved variant's risk_class or the call returns
RISK_TOOL_MISMATCH before any upstream request.

## Usage

```bash
gum call <op_id> --risk=<read|write|destructive> [args...] [flags]
```

## Parent

- [gum](gum.md)

## Arguments

| Name | Help |
| --- | --- |
| `op_id` |  |
| `args` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--confirmed` | `bool` | false | Set the signed-confirmation flag for destructive variants |
| `--csv` | `bool` | false | Render output as CSV |
| `--fields` | `string` |  | Field mask sent to the upstream API (host control) |
| `--json` | `bool` | false | Render output as JSON (default when piped) |
| `--markdown` | `bool` | false | Render output as Markdown |
| `--no-field-mask` | `bool` | false | Disable upstream field_mask injection |
| `-o`<br>`--output` | `string` |  | Output format: table\|json\|toon\|csv\|markdown\|raw\|value(<path>) (default: table on a terminal, json when piped) |
| `--page-size` | `int` | 0 | Page size for paginated reads (host control) |
| `--page-token` | `string` |  | Page token for paginated reads (host control) |
| `--raw` | `bool` | false | Return the raw upstream JSON without shaping |
| `--risk` | `string` |  | Risk path: read\|write\|destructive (required) |
| `--skeleton` | `bool` | false | Print a fillable template of the op's request fields and exit (no upstream call) |
| `--token` | `string` |  | HMAC-SHA256 confirmation token returned by a prior destructive attempt |
| `--toon` | `bool` | false | Render output as TOON |
| `--variant-id` | `string` |  | Pin variant_id (default: op's default_variant_id) |
| `--yes` | `bool` | false | Deprecated: destructive variants require --confirmed --token |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)
