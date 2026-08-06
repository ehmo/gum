# `gum version`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Print the gum version.

A literal "dev" output means the binary was built without the release ldflags. That happens with `go install` and `go build` because main.version is injected by goreleaser at release time. Install an official build from the GitHub releases page to see the real semver, or set notify.enabled=true to get a heads-up when a newer release exists.

## Usage

```bash
gum version
```

## Parent

- [gum](gum.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)
