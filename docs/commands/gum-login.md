# `gum login`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Authorize gum (alias for `gum auth login`)

## Usage

```bash
gum login [flags]
```

## Parent

- [gum](gum.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--all` | `bool` | false | Request the full catalog scope union (every service). Default is the core Workspace set; the full union needs all those APIs enabled on your OAuth client. |
| `--no-browser` | `bool` | false | Print the URL but don't launch a browser (use for SSH/devcontainer/headless) |
| `--scope` | `stringSlice` | [] | Exact OAuth scope(s) to request; repeat or comma-separate. Overrides --service/--all. |
| `--service` | `stringSlice` | [] | Request only these services' scopes (e.g. --service people,youtube). Comma-separate or repeat. |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)
