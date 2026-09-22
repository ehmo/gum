# First-party JSON Schema store

This directory holds the JSON Schema 2020-12 documents served by
`gum://schema/{ref}` for first-party ops. `gen-catalog -emit-schemas`
populates it: one `<ref>.json` per op that declares `request_fields`, derived
from those fields, with `binding.request_ref` stamped onto every variant of
that op in the same run. Ops with no `request_fields` get no ref, because an
empty-properties schema would claim they take no arguments.

The store holds request schemas only. The repository has no offline source
for response schemas (gum-wzmb), so `response_ref` stays unset.

Filenames are `<schema_ref>.json` where `<schema_ref>` matches the safe
served-ref grammar from spec §8.2 (`^[a-z0-9][a-z0-9._-]{0,127}$`, no `..`,
no path separators). The ref is the lowercased `op_id` plus `.request`:
58 shipped op_ids carry a camelCase segment the grammar rejects verbatim.

Bodies are written indented, not JCS-canonical. The §8.2 resource reader
canonicalises on read, so the wire bytes are canonical either way and the
on-disk file stays reviewable in a diff.

`test-fixture.v1.json` is hand-written, not generated. The regeneration pass
only prunes files ending in `.request.json`, so it survives.

Files prefixed with `_` (like this README) are excluded from `go:embed`.
