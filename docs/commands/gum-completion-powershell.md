# `gum completion powershell`

> Generated from `gum schema --json`. Do not edit this page by hand; run `make docs-commands`.

Generate the autocompletion script for powershell.

To load completions in your current shell session:

	gum completion powershell | Out-String | Invoke-Expression

To load completions for every new session, add the output of the above command
to your powershell profile.

## Usage

```bash
gum completion powershell [flags]
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
