# `gum completion`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Generate the autocompletion script for gum for the specified shell.
See each sub-command's help for details on how to use the generated script.

## Usage

```bash
gum completion
```

## Parent

- [gum](gum.md)

## Subcommands

- [gum completion bash](gum-completion-bash.md) - Generate the autocompletion script for the bash shell.
- [gum completion fish](gum-completion-fish.md) - Generate the autocompletion script for the fish shell.
- [gum completion powershell](gum-completion-powershell.md) - Generate the autocompletion script for powershell.
- [gum completion zsh](gum-completion-zsh.md) - Generate the autocompletion script for the zsh shell.

## Flags

| Flag | Type | Default | Help |
| --- | --- | --- | --- |
| `--log-format` | `string` | json | Log format: json\|text |
| `--log-level` | `string` | info | Log level: debug\|info\|warn\|error (overrides GUM_LOG_LEVEL) |
| `--profile` | `string` | default | Profile name to read/write config under |

## See also

- [gum](gum.md)
- [Command index](README.md)
