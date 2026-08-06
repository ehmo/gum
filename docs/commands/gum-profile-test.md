# `gum profile test`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

When --input is set, applies the profile to that file (optionally comparing against --golden). When --input is omitted, runs every [[tests]] fixture in the profile file through the expression pipeline and prints a ProfileFixtureResult[] JSON envelope (--format=json).

## Usage

```bash
gum profile test <profile-path> [flags]
```

## Parent

- [gum profile](gum-profile.md)

## Arguments

| Name | Help |
| --- | --- |
| `profile-path` |  |

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--format` | `string` |  | Output format: in fixture-runner mode 'json' (default); in single-fixture mode overrides profile default_format (toon\|json\|raw) |
| `--golden` | `string` |  | Path to golden output; if set, compare byte-for-byte |
| `--input` | `string` |  | Path to input JSON (single-fixture mode) |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum profile](gum-profile.md)
- [Command index](README.md)
