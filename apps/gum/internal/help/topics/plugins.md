# Plugin install, inventory, and lifecycle

Plugins extend GUM with additional ops, variants, and tools without modifying
the embedded catalogue. Shape 1 plugins are third-party subprocesses: installing
one means you trust its executable, manifest, declared capabilities, and any
plugin-managed credential flow it implements. GUM binds the installed executable
by digest and validates catalog/namespace metadata. On macOS, GUM launches
plugins under `sandbox-exec`; on Linux, GUM uses a Landlock filesystem ruleset
and a network namespace. Both backends enforce `network=false` plus
`fs_write_dir`; unsupported platforms fail closed until their OS sandbox
backends exist.

## Plugin shapes

- **Shape 1 (MCP plugin)** — subprocess that speaks JSON-RPC over stdio.
  GUM acts as a gateway, forwarding `tools/call` and `resources/read` from
  the host client to the plugin's MCP server. This is the v0.1.0 supported
  shape.
- **Shape 2 (in-process Risor module)** — declarative scripts executed in
  the embedded sandbox. Deferred to v0.2.0.

## Install protocol

`gum plugin install <local-dir> --yes` runs the spec §8.7 atomic three-file
transaction for a local plugin source directory. URL/PyPI/GitHub/git install
sources are not implemented in v0.1.x.

1. Acquire `plugins.install.lock` (advisory file lock, 30 s timeout).
2. Resolve the local source directory and stage the executable inside the
   active plugin install root.
3. Hash the executable, record `executable_path`, `executable_sha256`, and
   `argv_normalized` in `plugins.lock`.
4. Write `plugin-catalog.json.tmp.<txid>`, `plugins.lock.tmp.<txid>`, and
   `plugin-state.json.tmp.<txid>`, each carrying the same
   `install_generation` and `install_txid`.
5. Rename all three temp files atomically, fsync the directory.
6. Release the lock.

If any step fails the previous complete generation remains authoritative and
the staged artifacts are cleaned up. A crash mid-rename leaves a mixed
generation; startup picks the last complete shared `(install_generation, install_txid)`
pair and refuses dispatch from the incomplete one.

## Executable binding (security)

Every plugin spawn re-hashes `executable_path` and compares it to the value
recorded in `plugins.lock`. A mismatch quarantines the plugin with
`PLUGIN_EXECUTABLE_UNTRUSTED` and refuses to launch. Shell interpreters
(`sh`, `bash`, `zsh`, `cmd`, `powershell`) are denied as executable paths
outside dev profiles regardless of digest.

The `--yes` flag is a trust acknowledgment, not a security bypass. Use it only
after reviewing `manifest.json`, the executable path, declared capabilities,
and any `requirements.credential_descriptors`. For `auth_strategy=plugin_managed`
operations, the plugin owns its credential handling; GUM does not mint or scope
OAuth tokens for that plugin.

`fs_write_dir` is relative to the plugin install directory. Empty
`fs_write_dir` means `<install_dir>/data`; non-empty values allow writes only
under that subdirectory. Child processes inherit the same OS sandbox profile.

## Lifecycle states

`plugin-state.json` is the lifecycle authority:

| State                    | Trigger                                          |
|--------------------------|--------------------------------------------------|
| `installed`              | Install transaction committed.                   |
| `activated`              | Profile activation set `activated_at`.           |
| `quarantined`            | Digest mismatch, manifest violation, or operator.|
| `needs_configuration`    | Credential alias missing; install OK but unusable.|

Use `gum plugin reload <name>` after correcting a quarantined plugin, or
`gum plugin unquarantine <name>` only after independently verifying the plugin
is healthy.

## Quick commands

- `gum plugin install <local-dir> --yes` — install a reviewed local plugin
  directory after acknowledging subprocess trust.
- `gum plugin list` — show installed plugins for the active profile.
- `gum plugin remove <name>` — remove the installed plugin directory.
- `gum plugin setup <name>` — prompt for declared credentials and run a live
  canary.
- `gum plugin reload <name>` — clear quarantine and run a passive spawn canary.
- `gum plugin unquarantine <name>` — clear quarantine without restart.

## Errors

- `PLUGIN_MANIFEST_INVALID` — manifest schema or content rejected.
- `PLUGIN_EXECUTABLE_UNTRUSTED` — digest mismatch, shell interpreter spawn, or
  install-root escape.
- `PLUGIN_CATALOG_SCHEMA_UNSUPPORTED` / `PLUGIN_LOCK_SCHEMA_UNSUPPORTED` /
  `PLUGIN_STATE_SCHEMA_UNSUPPORTED` — ABI version mismatch; upgrade GUM.
