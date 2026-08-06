# Plugin Author Guide — Shape 1 MCP Subprocess

This is the **author-facing** companion to `docs/plugin-contract.md` (the normative contract) and `docs/catalog-abi.md` (the runtime catalog ABI). The contract tells you *what* the host validates and rejects; this guide walks you through *how to ship* a Shape 1 MCP-subprocess plugin from scratch — manifest, ABI wire format, packaging, install workflow, and a complete worked example.

In v0.1.0 through v0.3.x, Shape 1 (MCP subprocess) is the only externally authorable plugin shape. Shape 2 (gRPC subprocess) is reserved for a v0.4.0 freeze; third-party Shape 2 manifests are rejected at install time with `PLUGIN_SHAPE_UNSUPPORTED`.

---

## 1. What a Shape 1 plugin is

A Shape 1 plugin is a **subprocess** that GUM launches when one of its tools is invoked. The subprocess speaks **MCP over stdio** (JSON-RPC 2.0 with the standard MCP envelope) — the same wire format any MCP client uses to talk to any MCP server.

Concretely you ship:

1. **A manifest** (`manifest.json`) declaring identity, capabilities, sandbox requirements, and the tools the subprocess advertises.
2. **An executable** that, when launched with no arguments and its stdio connected to the host, behaves as a conforming MCP stdio server. It MUST implement `initialize`, `tools/list`, and `tools/call`.
3. **A package** (currently any of: GitHub release artifact, Git repo at pinned commit, PyPI wheel/sdist, or a local directory for development).

GUM verifies the executable's SHA-256 at install time, records `(executable_path, executable_sha256, argv_normalized, install_root)` in the active profile's `plugins.lock`, and re-hashes on every spawn (spec §8 line 1690). Shell interpreters in `executable_path` and path escapes outside the install root are rejected with `PLUGIN_EXECUTABLE_UNTRUSTED`.

---

## 2. Manifest reference

The manifest is the v1 schema enforced by `plugins.LoadManifest`. Every field below is required unless marked optional.

```json
{
  "manifest_schema_version": 1,
  "plugin_id": "fli",
  "name": "Flight Search",
  "version": "0.1.0",
  "namespace_owner": "io.example.flights",
  "shape": "mcp-plugin",
  "executable": "bin/fli-mcp",
  "advertised_tools": [
    {
      "name": "search",
      "description": "Search Google Flights for a route on a date range",
      "risk_class": "read"
    }
  ],
  "declared_capabilities": {
    "network": true,
    "fs_write_dir": "",
    "env_allow": ["FLI_USER_AGENT"]
  }
}
```

### Field-by-field

| Field | Type | Constraint | Failure code |
|---|---|---|---|
| `manifest_schema_version` | integer | Must be exactly `1` in v0.1.0. Must be a **sibling** of `[plugin]` in TOML manifests, never nested. | `PLUGIN_MANIFEST_SCHEMA_UNSUPPORTED` |
| `plugin_id` | string | Matches `^[a-z][a-z0-9-]{0,63}$`. | `PLUGIN_MANIFEST_INVALID` |
| `name` | string | Free-form display name. | `PLUGIN_MANIFEST_INVALID` if empty |
| `version` | string | Free-form (typically semver). | — |
| `namespace_owner` | string | Reverse-DNS (`io.example.foo`) or package-registry identity. Required for third-party plugins; first-party bundled fixtures may omit. | `PLUGIN_NAMESPACE_CONFLICT` if missing on a third-party install |
| `shape` | string | Must be `"mcp-plugin"`. | `PLUGIN_SHAPE_UNSUPPORTED` |
| `executable` | string | Path **relative** to the install directory; absolute paths are rejected during install. | `PLUGIN_MANIFEST_INVALID`, `PLUGIN_EXECUTABLE_UNTRUSTED` at install |
| `advertised_tools[].name` | string | Unprefixed. Host adds `plug.<plugin_id>.` prefix. | `PLUGIN_MANIFEST_INVALID` if empty |
| `advertised_tools[].risk_class` | string | One of `read`, `write`, `destructive`. | `PLUGIN_MANIFEST_INVALID` |
| `declared_capabilities.network` | bool | Enforced for hosted plugin subprocesses on supported OS backends. Unsupported OS backends fail closed. | `PLUGIN_SANDBOX_UNSUPPORTED` or sandbox spawn error |
| `declared_capabilities.fs_write_dir` | string | Enforced for hosted plugin subprocesses. Empty means no writes outside the host-managed data dir. A relative path scopes writes to that subtree. | `PLUGIN_FS_WRITE_OUTSIDE_SANDBOX` |
| `declared_capabilities.env_allow` | string[] | Env-var names allowed through to the subprocess. Subject to the **`GUM_` prefix denylist** and the exact-name denylist (`GOOGLE_APPLICATION_CREDENTIALS`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `_GUM*`). See `docs/plugin-contract.md` §needs_user_creds. | `PLUGIN_ENV_PROHIBITED` |

### Cross-references

- Namespace ownership rules: `docs/plugin-contract.md` §third-party namespace ownership.
- Reserved first-party prefixes (`gmail`, `drive`, `calendar`, and similar Google service prefixes): `docs/plugin-contract.md` §third-party namespace ownership.
- Credential descriptors required when `env_allow` carries OAuth-bearing vars: `docs/plugin-contract.md` §credential descriptors.

---

## 3. Wire ABI

The host launches the executable, connects its stdin/stdout to the JSON-RPC transport, and immediately issues an MCP `initialize`. The subprocess MUST:

1. Respond to `initialize` with its capabilities (in particular `tools: {listChanged: true}` if tools change at runtime — most plugins set this to `false`).
2. Respond to `tools/list` with the list of advertised tools and their inputSchemas. Tool names returned here MUST match the `advertised_tools[].name` in the manifest (unprefixed; the host adds the `plug.<plugin_id>.` prefix downstream).
3. Respond to `tools/call` with either a successful MCP `CallToolResult` or an MCP error envelope.

### 3.1 The four messages your plugin must handle

**(a) `initialize` request:**

```jsonc
// host → plugin
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
  "protocolVersion":"2025-06-18",
  "capabilities":{},
  "clientInfo":{"name":"gum","version":"0.1.0"}
}}

// plugin → host
{"jsonrpc":"2.0","id":1,"result":{
  "protocolVersion":"2025-06-18",
  "capabilities":{"tools":{"listChanged":false}},
  "serverInfo":{"name":"Flight Search","version":"0.1.0"}
}}
```

**(b) `tools/list`:**

```jsonc
// host → plugin
{"jsonrpc":"2.0","id":2,"method":"tools/list"}

// plugin → host
{"jsonrpc":"2.0","id":2,"result":{"tools":[
  {
    "name":"search",
    "description":"Search Google Flights for a route on a date range",
    "inputSchema":{
      "type":"object",
      "additionalProperties":false,
      "properties":{
        "origin":{"type":"string","minLength":3,"maxLength":3},
        "destination":{"type":"string","minLength":3,"maxLength":3},
        "depart_date":{"type":"string","format":"date"},
        "return_date":{"type":"string","format":"date"}
      },
      "required":["origin","destination","depart_date"]
    }
  }
]}}
```

**(c) `tools/call`:**

```jsonc
// host → plugin
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{
  "name":"search",
  "arguments":{"origin":"SFO","destination":"JFK","depart_date":"2026-06-15"}
}}

// plugin → host  (success)
{"jsonrpc":"2.0","id":3,"result":{
  "content":[
    {"type":"text","text":"{\"success\":true,\"data\":{\"itineraries\":[…]}}"}
  ],
  "isError":false
}}
```

**(d) Plugin-local error envelope (spec §8 lines 1624–1641):**

When the operation fails, the plugin returns a `CallToolResult` with `isError: true` and the body string is a JSON object:

```jsonc
{"success": false,
 "error_code": "RATE_LIMIT|AUTH_EXPIRED|PARSE_FAILURE|SERVICE_DOWN|INVALID_INPUT",
 "error": "human-readable message",
 "retryable": true,
 "retry_after_ms": 5000}
```

The host maps these to stable GUM codes (`plugins.MapPluginError`):

| Plugin emits | Host translates to | Retry/timing rules |
|---|---|---|
| `RATE_LIMIT` | `RATE_LIMITED` | preserve `retryable` and `retry_after_ms` (positive only) |
| `AUTH_EXPIRED` | `AUTH_REQUIRED` | force `retryable=false`; drop `retry_after_ms` |
| `PARSE_FAILURE` | `SERVICE_DOWN` | preserve `retryable` from envelope |
| `SERVICE_DOWN` | `SERVICE_DOWN` | preserve both |
| `INVALID_INPUT` | `INVALID_ARGS` | force `retryable=false`; drop `retry_after_ms` |
| anything else | `SERVICE_DOWN` | force `retryable=false`; `source_error_code` preserved |

**Plugins MUST NOT emit stable GUM codes directly** (`RATE_LIMITED`, `AUTH_REQUIRED`, etc.) — those are the host's vocabulary. Always emit one of the five plugin-local codes above.

---

## 4. Packaging

### 4.1 Directory layout

```text
my-plugin/
├── manifest.json            # the v1 manifest above
├── bin/
│   └── fli-mcp              # the executable (executable bit set)
├── README.md                # optional but recommended
└── LICENSE                  # required for non-local installs
```

For Python (FastMCP-style) plugins, ship a console-script entry point:

```text
my-plugin/
├── manifest.json
├── pyproject.toml           # declares console_scripts = fli-mcp = fli_mcp:main
├── src/
│   └── fli_mcp/
│       ├── __init__.py
│       └── server.py
└── requirements.lock
```

Remote package installers are not part of the v1 CLI. Package the plugin as a
local directory with a manifest and executable, then run `gum plugin install`
on that directory.

### 4.2 Install commands

| Source | Command |
|---|---|
| Local directory (dev) | `gum plugin install ./my-plugin` |

Run `gum plugin list` to confirm the install:

```sh
$ gum plugin list
fli    0.1.0   Flight Search
```

To clear a quarantine after a fix:

```sh
$ gum plugin reload fli
reloaded fli
```

`gum plugin remove fli` removes the install but **preserves the `namespace_owner` entry** in `plugins.lock`, so a reinstall by the same owner succeeds without re-asserting consent (spec §5.1 transfer procedure).

> **v0.1.0 note**: a dedicated `gum plugin validate` subcommand is not yet wired. Until it lands (planned for a v0.2.0 ergonomics pass), validate by running `gum plugin install ./my-plugin` against a scratch profile (`--profile=dev` + `XDG_DATA_HOME=/tmp/...`); the install path runs the full v1 manifest validator, namespace check, executable-binding rehash, and (if defined) the canary.

---

## 5. End-to-end example: a `hello` plugin in Python

This minimal plugin advertises one tool, `hello`, which returns a deterministic greeting. It uses the `mcp` Python SDK's stdio server.

**Directory layout:**

```text
hello-plugin/
├── manifest.json
├── pyproject.toml
└── src/hello_plugin/__init__.py
```

**`manifest.json`:**

```json
{
  "manifest_schema_version": 1,
  "plugin_id": "hello",
  "name": "Hello",
  "version": "0.1.0",
  "namespace_owner": "io.example.hello",
  "shape": "mcp-plugin",
  "executable": "hello-mcp",
  "advertised_tools": [
    {
      "name": "hello",
      "description": "Return a deterministic greeting for the given name",
      "risk_class": "read"
    }
  ],
  "declared_capabilities": {
    "network": false,
    "fs_write_dir": "",
    "env_allow": []
  }
}
```

**`pyproject.toml`:**

```toml
[project]
name = "hello-plugin"
version = "0.1.0"
dependencies = ["mcp>=1.0.0"]

[project.scripts]
hello-mcp = "hello_plugin:main"

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"
```

**`src/hello_plugin/__init__.py`:**

```python
import json
import sys
from mcp.server.fastmcp import FastMCP

app = FastMCP("Hello")

@app.tool()
def hello(name: str) -> str:
    """Return a deterministic greeting for the given name."""
    # GUM expects either a successful payload or a plugin-local error envelope
    # per spec §8 lines 1624–1641.
    if not name or not isinstance(name, str):
        return json.dumps({
            "success": False,
            "error_code": "INVALID_INPUT",
            "error": "name must be a non-empty string",
            "retryable": False,
        })
    return json.dumps({"success": True, "data": {"greeting": f"Hello, {name}!"}})

def main() -> None:
    app.run()  # stdio transport, blocks until parent closes stdin
```

**Install + smoke-test:**

```sh
# 1. Build the wheel.
python -m build hello-plugin

# 2. Install into a scratch profile.
GUM_PROFILE=dev gum plugin install ./hello-plugin

# 3. Invoke through the host.
gum plugin run hello hello '{"name":"world"}'
# → {"success":true,"data":{"greeting":"Hello, world!"}}

# 4. Trigger the validator with a bad arg.
gum plugin run hello hello '{"name":""}'
# → host maps INVALID_INPUT → INVALID_ARGS envelope
```

**What was validated end-to-end:**

- Manifest schema (`manifest_schema_version=1`, `shape=mcp-plugin`, plugin_id regex, namespace_owner).
- Namespace-ownership lock for the `hello.` prefix in `plugins.lock`.
- Executable binding (SHA-256 hashed into `plugins.lock`, re-hashed on every spawn).
- MCP handshake (`initialize`, `tools/list`, `tools/call`).
- Plugin-local error envelope mapped to a stable GUM code (`INVALID_INPUT` → `INVALID_ARGS`).

---

## 6. Common authoring mistakes

| Symptom | Likely cause | Fix |
|---|---|---|
| Install fails with `PLUGIN_MANIFEST_SCHEMA_UNSUPPORTED` | Forgot to set `manifest_schema_version: 1`, or placed it inside `[plugin]` in TOML | Hoist it to the top level. |
| Install fails with `PLUGIN_NAMESPACE_CONFLICT` | Another plugin already owns the prefix in this profile's lock, or `namespace_owner` is missing on a third-party manifest | Either rename your plugin_id, declare the actual owner string the previous install used, or — in dev only — re-run with `--dev-allow-namespace-conflict`. |
| Spawn fails with `PLUGIN_EXECUTABLE_UNTRUSTED` | `executable` resolves outside the install root, points to a shell interpreter (`sh`, `bash`, `python`), or the file's SHA-256 changed since install | Repackage with a real entry-point binary; rerun `gum plugin install` to record the new digest. |
| Calls fail with `SERVICE_DOWN` and `source_error_code: <something-weird>` | Plugin emitted an unknown error code; host maps unknowns to `SERVICE_DOWN` per spec §8 line 1641 | Use only the five plugin-local codes: `RATE_LIMIT`, `AUTH_EXPIRED`, `PARSE_FAILURE`, `SERVICE_DOWN`, `INVALID_INPUT`. |
| `gum plugin run` returns `PLUGIN_ENV_PROHIBITED` | `env_allow` lists a `GUM_*` var, `GOOGLE_APPLICATION_CREDENTIALS`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or `_GUM*` | Drop the denylisted entry; if you need an OAuth credential, declare it under `[requirements].credential_descriptors` instead. |
| Plugin crashes on second invocation | Plugin is not stdio-safe: it wrote `print(...)` to stdout outside the JSON-RPC framing | Route all logging to stderr; stdout is reserved for the JSON-RPC transport. |

---

## 7. Going further

- **Output profiles**: ship an `output_profile` per tool to keep responses compact. See `docs/profile-dsl-reference.md` for the operator catalogue and worked examples.
- **Canaries**: declare a `[requirements].canary` block so `cmd/gen-catalog` can run a known-good call at build time. Use relative date specifiers (`+7d`, `+2w`) to avoid stale fixtures.
- **Quarantine + crash recovery**: spec §8.6 describes the exponential-backoff window and the `gum plugin reload` / `gum plugin unquarantine` recovery commands.
- **Plugin-shipped profiles**: see `docs/plugin-contract.md` §profiles for the rules that govern profiles bundled inside a plugin.

For runtime catalog ABI (variant IDs, capability atoms, `binding` shape), read `docs/catalog-abi.md`. For the normative install contract and field-level wire requirements, read `docs/plugin-contract.md`.
