# First-party JSON Schema store

This directory holds JCS-canonical JSON Schema 2020-12 documents served by
`gum://schema/{ref}` for first-party Tier A ops. The store is populated by
the build-time `gen-catalog` step (deferred to v0.2.0 — see `bd show gum-zev5`).

Filenames are `<schema_ref>.json` where `<schema_ref>` matches the safe served-
ref grammar from spec §8.2 (`^[a-z0-9][a-z0-9._-]{0,127}$`, no `..`, no path
separators).

Files prefixed with `_` (like this README) are excluded from `go:embed`.
