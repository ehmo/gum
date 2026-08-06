# `gum doctor`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Runs a single pre-flight that reports the health of each gum subsystem:

  - auth:    checks BYO OAuth, API key, service account, and ADC readiness
  - audit:   reports audit.broken and confirms the per-profile data dir is writable
  - cache:   confirms the per-profile cache dir exists and is writable
  - plugin:  lists installed plugins (a fast read-only walk of the plugin root)
  - config:  loads and validates the active profile's config.toml

Exits non-zero if any subsystem is broken. Use --format=json for machine output.

## Usage

```bash
gum doctor [flags]
```

## Parent

- [gum](gum.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--format` | `string` | text | Output format: text\|json |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)
