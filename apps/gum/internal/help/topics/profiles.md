# Runtime profiles and expression profiles

GUM uses two different things named "profile":

- A **runtime profile** scopes config, credentials, cache, audit, plugin state,
  and tee artifacts for a running `gum` process. Select it with
  `--profile <name>` or `GUM_PROFILE=<name>`.
- An **expression profile** is a DSL file consumed by `gum profile validate`
  and `gum profile test` to validate output shaping rules.

The default runtime profile is `default`. Profile names may contain only ASCII
letters, digits, `.`, `_`, and `-`; traversal, separators, whitespace, and
control characters are rejected before filesystem access.

## Runtime profile layout

Config:

```
$XDG_CONFIG_HOME/gum/<profile>/config.toml
# default: ~/.config/gum/<profile>/config.toml
```

Data:

```
$XDG_DATA_HOME/gum/<profile>/
# default: ~/.local/share/gum/<profile>/
├── audit.jsonl              # audit log
├── audit.broken             # sentinel written after audit failures
├── plugin-catalog.json      # plugin variants
├── plugins.lock             # plugin supply-chain state
├── plugin-state.json        # plugin quarantine/lifecycle state
├── plugins.install.lock     # advisory lock for plugin install transactions
├── confirmation-replay/     # destructive confirmation replay markers
├── tee.secret               # per-profile HMAC secret
└── tee/<YYYY-MM-DD>/<op_id>/<hash>.json.gz
```

Cache:

```
$XDG_CACHE_HOME/gum/<profile>/
# default: ~/.cache/gum/<profile>/
```

## Runtime profile commands

- `gum --profile work auth status` — inspect auth for `work`.
- `GUM_PROFILE=work gum doctor --format=json` — run CI-friendly checks for
  `work`.
- `gum config list --profile work` — inspect runtime config.
- `gum config set key=value --profile work` — write runtime config.

`--profile` wins over `GUM_PROFILE`; when neither is set, `default` is used.

## Expression-profile commands

- `gum profile validate <path>` — parse an expression-profile DSL file.
- `gum profile test <path> --input fixture.json --golden golden.toon` — apply
  one fixture and compare output.
- `gum profile test <path> --format=json` — run the file's `[[tests]]` blocks
  and emit a JSON result envelope.

Expression profiles are files you pass explicitly. `gum profile` does not list
or switch runtime profiles.

## Errors

- `invalid profile name` — the runtime profile name failed validation.
- `PROFILE_NO_FIXTURES` — `gum profile test <path>` ran without `--input`, but
  the expression-profile file has no `[[tests]]` blocks.
- `PROFILE_GOLDEN_MISMATCH` — expression-profile output did not match the
  supplied golden file.

See `gum://help/auth` for profile-scoped credentials, `gum://help/plugins` for
profile-scoped plugin state, and `gum://help/recovery` for tee artifacts.
