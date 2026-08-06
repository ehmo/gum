# `gum completion fish`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

	gum completion fish | source

To load completions for every new session, execute once:

	gum completion fish > ~/.config/fish/completions/gum.fish

You will need to start a new shell for this setup to take effect.

## Usage

```bash
gum completion fish [flags]
```

## Parent

- [gum completion](gum-completion.md)

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--no-descriptions` | `bool` | false | disable completion descriptions |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum completion](gum-completion.md)
- [Command index](README.md)
