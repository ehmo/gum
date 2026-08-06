# `gum completion zsh`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions in your current shell session:

	source <(gum completion zsh)

To load completions for every new session, execute once:

#### Linux:

	gum completion zsh > "${fpath[1]}/_gum"

#### macOS:

	gum completion zsh > $(brew --prefix)/share/zsh/site-functions/_gum

You will need to start a new shell for this setup to take effect.

## Usage

```bash
gum completion zsh [flags]
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
