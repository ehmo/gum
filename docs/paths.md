---
title: Paths and profiles
description: "Where gum stores profile config, local state, cache files, and credentials."
---

# Paths and profiles

A gum profile separates local config, credential lookup, cache files, audit
state, plugin registry state, and output artifacts. Use profiles when two
workflows should not share a Google account, service account, Ads developer
token, plugin setup, config defaults, or API key.

## Select a profile

Every command accepts `--profile`. Without that flag, gum reads `GUM_PROFILE`.
Without either value, it uses `default`.

```bash
gum --profile work auth status
GUM_PROFILE=lab gum doctor
```

The command-line flag wins over `GUM_PROFILE`.

Profile names may contain letters, numbers, `.`, `_`, and `-`. They cannot use
path tricks such as `..`, slashes, whitespace, or control characters.

## Filesystem layout

gum follows XDG base directories.

| Kind | Default path | Override |
| --- | --- | --- |
| Config | `~/.config/gum/<profile>/config.toml` | `XDG_CONFIG_HOME` |
| Data | `~/.local/share/gum/<profile>/` | `XDG_DATA_HOME` |
| Cache | `~/.cache/gum/<profile>/` | `XDG_CACHE_HOME` |
| Update notice cache | `~/.cache/gum/<profile>/notify.json` | `XDG_CACHE_HOME` |

Gum writes the config file with mode `0600` and creates managed parent
directories with owner-only permissions.

## What lives where

Config contains flat profile settings such as output defaults, audit settings,
cache policy, gain settings, code-mode limits, validation settings, meta-tool
settings, and update notices.

Data contains profile state that should survive cache cleanup, including audit
logs, gain ledgers, confirmation replay state, copied plugin schemas, and plugin
registry files.

Cache contains HTTP or semantic cache files and short-lived notification state.
Cache files can be removed when you want gum to rebuild local derived state.

## Credentials

Gum stores Google refresh tokens, OAuth client secrets, API keys, Google Ads
developer tokens, and plugin secrets through the OS keychain when available.
Some auth strategies can also read from the host environment, such as ADC or an
API-key fallback. Do not paste credential values into prompts or docs.

Use [Auth](auth.md) for setup commands and [Safety](safety.md) for agent-facing
secret handling.

### Keychain call bounds

Every keychain call is bounded, so a keychain that never answers fails the
command instead of hanging it. A locked Linux Secret Service collection does
exactly that: its unlock prompt has nobody to answer it on a headless host, an
SSH session, a container, or CI.

| Call | Bound |
| --- | --- |
| A credential read or write you asked for | 20 seconds |
| A read gum makes on its own behalf, such as the granted-scope lookup on the dispatch path | 2 seconds |

A call that runs past its bound fails with `AUTH_KEYCHAIN_UNAVAILABLE`. The
granted-scope lookup is best effort: when it times out, gum continues with no
recorded scopes rather than failing the command.

Set `GUM_KEYRING_TIMEOUT` to a Go duration to change the bounds. Raising it
lengthens only the first bound, which is what a slow interactive keychain
prompt needs. Lowering it shortens both. A value that is not a positive
duration is ignored.

```bash
GUM_KEYRING_TIMEOUT=60s gum auth use-oauth-client
```

## Project-local agent files

`gum setup --scope project` writes agent and MCP client config under the current
project instead of a global client config location. Run the dry run first:

```bash
gum setup --scope project --dry-run
```

The dry run prints the exact files it would write.
