# MCP Client Setup

gum runs as an MCP stdio server. It does not open an HTTP listener.

## Health Check

```shell
gum setup --dry-run
gum mcp --stdio --help
gum doctor --format=json
```

Use an absolute path if another program named `gum` is installed.

```shell
command -v gum
gum --version
```

## Guided Setup

For common coding agents, let gum write the skill files and MCP config:

```shell
gum setup --target all --features skills,mcp --dry-run
gum setup --target all --features skills,mcp --yes
```

Use one target when you only want one client:

```shell
gum setup --target codex --scope user --yes
gum setup --target claude --scope project --yes
gum setup --target cursor --scope project --yes
gum setup --target gemini --scope user --yes
```

`gum setup` prints the files it writes and the next auth checks. Existing MCP
config is merged. Existing skill files are skipped unless you pass `--force`.

## Claude Desktop

Edit `claude_desktop_config.json`.

macOS path:

```text
~/Library/Application Support/Claude/claude_desktop_config.json
```

Config:

```json
{
  "mcpServers": {
    "gum": {
      "command": "/absolute/path/to/gum",
      "args": ["mcp", "--stdio"]
    }
  }
}
```

Restart Claude Desktop after editing the file.

## Claude Code

Use a project `.mcp.json` or the user-level Claude Code MCP config.

```json
{
  "mcpServers": {
    "gum": {
      "command": "/absolute/path/to/gum",
      "args": ["mcp", "--stdio"]
    }
  }
}
```

## Cursor

Edit `~/.cursor/mcp.json`.

```json
{
  "mcpServers": {
    "gum": {
      "command": "/absolute/path/to/gum",
      "args": ["mcp", "--stdio"]
    }
  }
}
```

## Exposed Surface

At startup gum exposes a compact meta-tool surface plus selected convenience
tools. The meta-tools cover search, describe, read, write, destructive
confirmation, sandboxed code, gain/cache inspection, and polling. Use
`gum mcp --stdio --help` or `gum describe <op_id>` for the exact surface in the
binary you installed.

The server also exposes `skills_list` and `skills_get`. Agents can use those
tools to load the same guidance shipped by `gum skills list` and
`gum skills show mcp`.

The server also supports MCP resources, prompts, completions, roots-aware
project lookup, cancellation, progress, and structured errors where the pinned
MCP SDK supports them. It does not advertise unimplemented server capabilities.

## Failure Checks

If the client does not show gum tools:

1. Run `gum mcp --stdio --help`.
2. Run `gum doctor --format=json`.
3. Replace `command: "gum"` with an absolute path.
4. Restart the client.
5. Read the client's stderr log. gum must not print normal logs to stdout in
   MCP mode before the JSON-RPC handshake.

If auth fails inside the MCP client, fix it in the terminal first:

```shell
gum auth status
gum login --service gmail,calendar
gum doctor
```
