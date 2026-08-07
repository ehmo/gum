---
title: gum v1.0.3 release notes
date: 2026-08-06
status: release
---

# gum v1.0.3

The MCP server now speaks protocol revision 2026-07-28. Three details change on
the wire, project-local profile resolution asks for the client's roots a
different way, and the capability catalog is refreshed from the upstream
discovery documents.

## Highlights

- MCP server upgraded to revision 2026-07-28. Clients on the older revision are
  served by the same code path.
- `resources/read` on an unknown URI answers with JSON-RPC `-32602` instead of
  `-32002`.
- `tools/list` always carries `readOnlyHint` and `idempotentHint`, including
  when the value is `false`.
- Catalog refreshed: 222 operations before and after, seven of them changed.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v1.0.3 bash
gum --version
gum doctor
```

Or through the tap:

```bash
brew install ehmo/tap/gum
```

## Upgrade notes

No config, catalog ABI, or CLI change. Three MCP wire details change with
revision 2026-07-28, and gum tracks the new shape rather than pinning the old
one.

- A client that matches `-32002` to detect an unknown resource URI has to
  accept `-32602`. Reading `gum://help/nosuchtopic` now returns the Invalid
  Params code.
- A client that reads an absent `readOnlyHint` as "unknown" now sees an
  explicit `false` on tools that are not read-only. `destructiveHint` and
  `openWorldHint` keep their present-or-absent behaviour.
- An unknown method over stdio answers `-32601`.

Project-local profile resolution costs one extra round trip on the first tool
call of a session. Revision 2026-07-28 forbids a server from asking for the
client's roots while it serves a request, so gum returns an input request with
the tool result and the client answers on a retry of the same call. The
resolution rules, the `_meta.gumRoot` selection in multi-root sessions, and the
`PROJECT_ROOT_REQUIRED` envelope are unchanged. Callers that do not use
project-local profiles see no extra traffic.

## Changed

- The MCP server is built on `modelcontextprotocol/go-sdk` v1.7.0, which
  implements MCP revision 2026-07-28.
- Project-local profile lookup obtains the client's roots through an input
  request carried on the tool result, because the revision removed the
  server-initiated `roots/list` call it used before.
- The capability catalog was regenerated from the upstream discovery documents.
  222 operations before and after: none added, none removed, seven changed. Six
  gained an optional upstream request field (`groupIdFilter`,
  `includeSensitiveData`, `showOwnOrganizationOnly`, `eventLabelVersion`, and
  `markupSyntax` on two chat operations), along with the enum values
  `workspace_studio` and `writerWithoutPrivateAccess`. One Drive operation
  description was reworded. No risk class, scope, auth strategy, or required
  argument changed, so no existing call behaves differently.

## Security

No vulnerability fixes. `govulncheck` reports 0 vulnerabilities that gum's code
calls. It reports one advisory against a module in the graph, GO-2026-5932,
which marks `golang.org/x/crypto/openpgp` unmaintained. gum imports no symbol
from that package, and the advisory has no fixed version.

## Known limitations

- macOS release binaries are not notarized: the signing secrets are not
  provisioned, so goreleaser skips the notarize step. Check with
  `spctl --assess --type execute --verbose gum`. If it rejects the binary,
  clear the quarantine attribute: `xattr -d com.apple.quarantine gum`. The tap
  formula does this for you on install.
- The tap formula is bumped by hand after a release publishes, so it can trail
  a new tag by a short window.

## Token savings

Measured on the in-tree release fixtures before tagging. This release does not
change output shaping, so both rows match v1.0.2.

```bash
cd apps/gum
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 0 | 0 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

## Reproducibility

```bash
git checkout v1.0.3
cd apps/gum
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=v1.0.3' ./cmd/gum
sha256sum gum
```
