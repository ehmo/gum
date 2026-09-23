---
title: gum v1.1.0 release notes
date: 2026-08-07
status: release
---

# gum v1.1.0

gum can now retrieve Gmail message attachments. This release also ties the
public docs deployment to the stable-release gate.

## Highlights

- New read-only `gmail.users.messages.attachments.get` operation.
- The operation uses the Gmail v1 endpoint and the `gmail.readonly` scope.
- The catalog grows from 222 to 223 operations. No existing operation changed.
- A release cannot publish binaries until gumcli.dev serves the tagged release
  notes and changelog.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v1.1.0 bash
gum --version
gum doctor
```

Or through the tap:

```bash
brew install ehmo/tap/gum
```

## Upgrade notes

None. The catalog ABI, profile format, CLI flags, and MCP protocol are unchanged.

## Added

- `gmail.users.messages.attachments.get` retrieves the `MessagePartBody` for an
  attachment. Supply `userId`, `messageId`, and attachment `id`. The response
  carries the attachment size and base64url-encoded `data` returned by Gmail.

## Changed

- gumcli.dev now serves a Git-backed docs build from the public repository's
  `main` branch. The tagged-release workflow compares the live release-notes
  and changelog pages with the tagged source before GoReleaser can publish.

## Security

No security fix or permission expansion. The new operation is read-only and
uses the existing `gmail.readonly` OAuth scope.

## Known limitations

- Gmail returns attachment data as a base64url-encoded string. Decode the
  `data` field before writing the original attachment bytes.
- macOS binaries carry the Go linker's ad-hoc signature, not a Developer ID
  signature, and are not notarized. Checked on this release's `darwin/arm64`
  artifact: `codesign -v` exits 0, which meets the Apple Silicon requirement
  that every executable carry a signature. `curl` sets no
  `com.apple.quarantine` attribute, so neither `install.sh` nor a Homebrew
  download reaches Gatekeeper, and a quarantined copy still runs from a shell
  (checked under Darwin 25.6.0). `spctl --assess --type execute` reports
  `rejected`, and no install path gum publishes consults that verdict. An
  earlier version of this note told readers to clear quarantine by hand. That
  advice was wrong.
- The tap formula is bumped by hand after release assets publish, so it can
  trail the tag for a short window.

## Token savings

Measured on the in-tree release fixtures for this release. The new catalog entry
does not change output shaping.

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
git checkout v1.1.0
cd apps/gum
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=v1.1.0' ./cmd/gum
sha256sum gum
```
