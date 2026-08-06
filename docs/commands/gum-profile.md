# `gum profile`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Validate or test an expression profile

## Usage

```bash
gum profile
```

## Parent

- [gum](gum.md)

## Subcommands

- [gum profile test](gum-profile-test.md) - When --input is set, applies the profile to that file (optionally comparing against --golden). When --input is omitted, runs every [[tests]] fixture in the profile file through the expression pipeline and prints a ProfileFixtureResult[] JSON envelope (--format=json).
- [gum profile validate](gum-profile-validate.md) - Parse an expression-profile DSL file and report any errors. Use this in CI to catch malformed catalog profiles before release.

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)
