# `gum completion bash`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Generate the autocompletion script for the bash shell.

This script depends on the 'bash-completion' package.
If it is not installed already, you can install it via your OS's package manager.

To load completions in your current shell session:

	source <(gum completion bash)

To load completions for every new session, execute once:

#### Linux:

	gum completion bash > /etc/bash_completion.d/gum

#### macOS:

	gum completion bash > $(brew --prefix)/etc/bash_completion.d/gum

You will need to start a new shell for this setup to take effect.

## Usage

```bash
gum completion bash
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
