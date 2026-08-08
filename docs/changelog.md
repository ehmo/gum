---
title: Changelog
description: "Release notes and version history for gum."
---

# Changelog

Release notes record user-visible changes, upgrade notes, security notes, and
fixture-backed token-savings measurements.

## Releases

| Version | Date | Notes |
| --- | --- | --- |
| `v1.1.0` | 2026-08-07 | [Gmail attachment retrieval and release-coupled docs deployment.](release-notes-v1.1.0.md) |
| `v1.0.3` | 2026-08-06 | [MCP revision 2026-07-28, with new resource-not-found and annotation wire shapes.](release-notes-v1.0.3.md) |
| `v1.0.2` | 2026-08-06 | [MIT license, public docs site, and fixes for silently dropped fields and unshaped cache hits.](release-notes-v1.0.2.md) |
| `v1.0.1` | 2026-06-18 | [`gum auth probe` checks BYO OAuth before ADC.](release-notes-v1.0.1.md) |
| `v1.0.0` | 2026-06-17 | [Public release candidate for the CLI and MCP server.](release-notes-v1.0.0.md) |

## Release process

- Release-gated proof obligations live in [`Test Matrix`](test-matrix.md).
- Every release publishes SHA-256 checksums and a SLSA provenance statement
  next to the archives; `install.sh` verifies both.
