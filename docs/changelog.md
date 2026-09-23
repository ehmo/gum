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
| `v2.2.1` | 2026-09-22 | [Keyword Planner geo and language targeting named in the catalog and carried in the `gum describe` example, curated `example_args`, catalog regen, lint gate green again, macOS notarization dropped.](release-notes-v2.2.1.md) |
| `v2.2.0` | 2026-09-22 | [Outbound HTTP and ETag revalidation, managed-scope re-consent over MCP elicitation, capability atoms on every shipped variant, macOS signing secrets reach the release pipeline.](release-notes-v2.2.0.md) |
| `v2.1.0` | 2026-09-21 | [Strict-client tools/list fix, remote plugin package sources, §9.0 TOON wire documents, plugin variants in the session catalog, profile-account binding.](release-notes-v2.1.0.md) |
| `v2.0.1` | 2026-09-20 | [No code change: the release tree now passes its own `gofmt` gate, which the v2.0.0 tree did not.](release-notes-v2.0.1.md) |
| `v2.0.0` | 2026-09-20 | [Specification conformance across dispatch, MCP, shaping, plugins, and auth, with five security fixes.](release-notes-v2.0.0.md) |
| `v1.5.0` | 2026-09-18 | [Keyword Planner location and language defaults, and a merged-keyword input map.](release-notes-v1.5.0.md) |
| `v1.4.0` | 2026-09-18 | [Default Google Ads account, Keyword Planner location guidance, and gRPC security update.](release-notes-v1.4.0.md) |
| `v1.3.0` | 2026-09-07 | [Data Manager event ingestion, diagnostics, and upload safety fixes.](release-notes-v1.3.0.md) |
| `v1.2.0` | 2026-09-06 | [Google Ads reporting, guarded mutations, and conversion uploads.](release-notes-v1.2.0.md) |
| `v1.1.0` | 2026-08-07 | [Gmail attachment retrieval and release-coupled docs deployment.](release-notes-v1.1.0.md) |
| `v1.0.3` | 2026-08-06 | [MCP revision 2026-07-28, with new resource-not-found and annotation wire shapes.](release-notes-v1.0.3.md) |
| `v1.0.2` | 2026-08-06 | [MIT license, public docs site, and fixes for silently dropped fields and unshaped cache hits.](release-notes-v1.0.2.md) |
| `v1.0.1` | 2026-06-18 | [`gum auth probe` checks BYO OAuth before ADC.](release-notes-v1.0.1.md) |
| `v1.0.0` | 2026-06-17 | [Public release candidate for the CLI and MCP server.](release-notes-v1.0.0.md) |

## Release process

- Release-gated proof obligations live in [`Test Matrix`](test-matrix.md).
- Every release publishes SHA-256 checksums and a SLSA provenance statement
  next to the archives. `install.sh` verifies checksums and attempts the optional
  provenance check when `slsa-verifier` is installed. The release workflow checks
  provenance against every published archive.
