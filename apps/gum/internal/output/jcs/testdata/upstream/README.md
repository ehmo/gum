# Upstream RFC 8785 test vectors

These twelve files are a verbatim copy of the `testdata/input` and
`testdata/output` directories of
<https://github.com/cyberphone/json-canonicalization>, the reference corpus
RFC 8785 cites. `LICENSE` is that repository's Apache-2.0 license; the copyright
is Anders Rundgren's.

- Upstream commit fetched: `19d51d7fe467d4706a3ff08adf8a748f29fc21e0` (2024-12-13)
- Last upstream change to `testdata/`: `dc406ceaf94b5fa554fcabb92c091089c2357e83` (2021-08-23)
- Fetched 2026-09-23

`TestUpstreamJCSVectors` in `../../upstream_vectors_test.go` reads every pair:
each `input/<name>.json` canonicalized with `jcs.Marshal` must equal
`output/<name>.json` byte for byte. Do not reformat either directory. The
expected outputs are exact byte strings, and `input/structures.json` and
`input/values.json` deliberately end without a trailing newline.

Two of these vectors caught real conformance bugs:

- `values.json` pins `\n` for U+000A where gum emitted `\u000a` (bead gum-gq9q).
- `values.json` pins `1e+30` and `1e-27`, which the ES6 `Number::toString`
  rules produce and Go's `'g'` verb did not (bead gum-xvy9).

What the corpus does not cover: an integer above 2^53. Every number here fits a
float64 exactly, so none of these vectors touches the exact-digit `int64`/`uint64`
fast path that `TestJCSCanonicalLargeUint64` pins and spec §10.0 records as gum's
one deliberate departure from RFC 8785. The two behaviors do not meet.

The ES6 number vectors are not vendored. Upstream distributes them as a 4 GB
generated file, and its README gives SHA-256 checksums so an implementation can
regenerate the sequence instead of downloading it.
`TestUpstreamES6NumberVectors` does that: it regenerates the first 10000 lines,
checks them against the published checksum, and compares each one against
`es6Number`.
