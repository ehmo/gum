---
title: gum v2.2.0 release notes
date: 2026-09-22
status: release
---

# gum v2.2.0

This release puts the spec §10.2 HTTP and ETag cache on the outbound request
path, adds managed-scope re-consent over MCP elicitation, declares capability
atoms on all 228 shipped catalog variants, and forwards the macOS signing
secrets that the release pipeline had been dropping.

## Highlights

- Repeat reads revalidate. A read whose stored validator matches sends
  `If-None-Match`, and an upstream 304 answers
  `{"unchanged": true, "etag": "..."}` without downloading the body again.
  In v2.1.0 the store existed and no request path read or wrote it.
- An MCP client can now recover from `SCOPE_MISSING` without leaving the
  session. gum sends one elicitation form bound to the refused call, and an
  approval runs a loopback consent for exactly the scopes that were missing.
- All 228 catalog variants declare capability atoms, so the §5.8 model has
  real input. `drive.files.get` and `drive.files.export` are `partial`: they
  declare `media_download` and cannot run it, and both say so in the
  response envelope and in `gum.describe_op`.
- `gum auth login` refuses a partial consent when the caller names required
  scopes, instead of storing a grant that is missing some of them.
- The release workflow passes the five `MACOS_*` secrets to GoReleaser. The
  notarize gate had no way to see them, so it was always false.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v2.2.0 bash
gum --version
gum doctor
```

## Upgrade notes

- Conditional requests are on by default for read-class operations. A second
  identical read of an unchanged resource returns
  `{"unchanged": true, "etag": "..."}` rather than the full body. A caller
  that needs the body every time, or that needs a stored response re-shaped
  under a changed output profile, clears the validator first:

  ```bash
  gum cache clear                          # the whole store
  gum cache clear "gmail.*"                # one API
  gum cache clear gmail.users.messages.list  # one operation
  ```

  The store has no TTL and nothing evicts it, so `gum cache clear` is also
  the only way to reclaim its disk. `gum cache stats` reports its entry
  count, byte size, hits, and misses under the `http` key.
- The cache is per profile and per principal. Its key covers `op_id`, the
  resolved `variant_id`, the canonical args, and the credential subject, so
  a different profile, variant, or Google account never reuses a validator.
- No other action is required.

## Added

- Spec §10.2 HTTP and ETag revalidation on the outbound path. The kernel
  looks the validator up before the token bucket, the typed REST adapter
  sends `If-None-Match`, and a 304 short-circuits the response pipeline:
  stages 1 to 8, the field mask, the tee artifact, and the
  `gum://results/<hash>` handle are all skipped, and `_expression` is
  omitted. The gain ledger records the call with `cache_status: "etag_304"`,
  `served_from_cache: true`, `response_tokens: 0`, and the cached body in
  `raw_tokens`.
- Managed-scope re-consent over MCP elicitation (spec §13). The form binds
  `op_id`, the resolved `variant_id`, the profile, the exact sorted scope
  set, the account the profile is bound to, and a request hash recomputed
  from live state when the reply arrives. An approval returns
  `SCOPE_GRANTED`; the operation is not re-run for the caller. A binding
  mismatch, a partial grant, a consent on another account, a decline, and a
  cancel each store nothing and leave the original refusal in place. Every
  outcome writes one `managed_scope_reconsent` audit event. The form goes
  only to clients that declared the `elicitation` capability.
- Capability atoms on all 228 shipped variants, derived offline by
  `gen-catalog -apply-capabilities` from each op's request record and HTTP
  binding, with a curated table for the variants whose atoms contradict what
  the adapters run. A variant that cannot run a declared atom carries
  `_expression._unsupported_capabilities` in its success envelope, and
  `gum.describe_op` renders the same atoms as `capability_class_warnings`.
- `gum cache clear` documents its pattern argument in `--help`.

## Changed

- `gum auth login` refuses a partial consent when the caller names required
  scopes. The prior keyring grant is left untouched and the refusal names the
  missing scopes with `BYO_OAUTH_SCOPE_NOT_GRANTED`.

## Fixed

- The release workflow passes `MACOS_SIGN_P12`, `MACOS_SIGN_PASSWORD`,
  `MACOS_NOTARY_ISSUER_ID`, `MACOS_NOTARY_KEY_ID`, and `MACOS_NOTARY_KEY` to
  the `goreleaser` step. The notarize block is gated on
  `isEnvSet "MACOS_SIGN_P12"`, and the job set only `GITHUB_TOKEN`, so the
  gate was always false whatever the repository held.
- A `field_mask_mode = "dual_fetch"` recovery request no longer carries the
  masked request's validator. That validator's key includes the canonical
  args, and the recovery request removes `fields` from them, so a 304 there
  would have written a `full_result_path` artifact from an empty body.
- The spec §5.4 pipeline diagram claimed `gen-catalog` computes
  `default_fields` from §5.6 heuristics. §5.6 says the table is curated by
  hand and applied by a separate `gen-catalog -apply-default-fields` pass.

## Security

- No dependency was added or removed since v2.1.0.
- `govulncheck` reports the same result as v2.1.0: 0 vulnerabilities in code
  this project calls, and one finding, GO-2026-5932 in module
  `golang.org/x/crypto`, which has no fixed version and no called symbol.
- The ETag store is keyed by credential subject as well as by operation,
  variant, and canonical args, so one principal's cached body is never served
  to another. The store file is per profile under the profile cache
  directory.

## Known limitations

- 217 of 228 catalog variants declare no `default_fields`. Spec §5.6 curates the
  table by hand and it covers 11 high-traffic read operations, so every other
  variant sends no upstream field mask unless the call passes `--fields`, and
  its response comes back full size.
- macOS binaries carry the Go linker's ad-hoc signature, not a Developer ID
  signature, and are not notarized. Measured on this release's `darwin/arm64`
  artifact under Darwin 25.6.0: `codesign -v` exits 0, `curl` sets no
  `com.apple.quarantine` attribute, and a copy with quarantine forced on still
  runs from a shell. `spctl --assess --type execute` reports `rejected`, and
  no install path gum publishes consults that verdict. Notarization matters
  for a cask, a `.pkg`, or an app bundle, none of which gum ships.
- The ETag store has no TTL and no eviction. It grows until
  `gum cache clear` runs. Check its size with `gum cache stats`.
- A second gum process on the same profile cannot open the ETag store while
  the first holds it, for example a `gum call` during a long-lived MCP
  session. The second process waits 250 ms, then dispatches without
  revalidation and fetches the full body. `gum cache stats` reports
  `entries: 0` for the locked store in that case.
- The `v2.0.0` tag tree still fails `make fmt-check`. A published tag is not
  moved, because its binaries, checksums and provenance all name that commit.

## Token savings

Measured with the release fixtures using a local build stamped 2.2.0, run from
the `apps/gum` directory of the matching source checkout:

```sh
gum gain --fixture-replay --format=toon
gum gain --fixture-replay --format=json
```

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 210 | 5.35 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

Both rows are unchanged from v2.1.0. The fixture replay measures the shaping
pipeline, and no fixture in the set repeats a read, so the new ETag path
contributes nothing to these numbers. A 304 in live use saves the whole
response body: the ledger records the cached body in `raw_tokens` against
`response_tokens: 0`.

## Reproducibility

```sh
git clone https://github.com/ehmo/gum.git
cd gum && git checkout v2.2.0
cd apps/gum
GOTOOLCHAIN=go1.26.7 CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath \
  -ldflags='-s -w -X main.version=2.2.0' ./cmd/gum
sha256sum gum
```

Build from a full clone, not from a linked `git worktree`. Go embeds the commit
revision in the binary, and it silently skips that stamp in a linked worktree,
which changes the hash.

Write the rebuilt binary outside the clone. Go stamps `vcs.modified=true` when
the working tree holds any untracked file, so a `-o` path inside the checkout
changes the hash of every build after the first.

## Verification

Release run
[35701601323](https://github.com/ehmo/gum/actions/runs/35701601323), tag
`v2.2.0` at public commit `bf6de6c242fbb5c84ac4a1f2ead1fd460599061b`. All seven
jobs passed: validate semver tag, docs deployed from tag commit, pre-release
tests, govulncheck, goreleaser, reproducible-build canary, verify release
provenance against release artifacts. The goreleaser job resolved `1.26.x` to
`go1.26.7`.

Checked independently of the pipeline, against the published artifacts:

- `shasum -a 256 -c checksums.txt` reported OK for all four archives.
- The provenance attestation matched: identity and archive hashes for `v2.2.0`
  at commit `bf6de6c242fbb5c84ac4a1f2ead1fd460599061b`.
- A clean `git clone --depth 1 --branch v2.2.0` rebuilt all four binaries under
  `GOTOOLCHAIN=go1.26.7` to the hashes below, which equal
  `release-binaries.sha256`. The binaries extracted from the four published
  archives hash identically.

| Platform | Binary sha256 |
| --- | --- |
| `darwin/amd64` | `6184dbfe5a2c88159e12cc02ba0b6ed8a876f5e4490f136cda60d0baea2eb444` |
| `darwin/arm64` | `030cdf57ad020ccdae7e7da577463e70ceee0e1093a6a4d431ab4db6b07da257` |
| `linux/amd64` | `b41e647b4139c03e4f47281ae6227a1d213db233c01d93a3510a274fde9b6df5` |
| `linux/arm64` | `4869b279d6836304e3ead04143f17eda474cc7ee55b30efc3e483d9db30a557d` |

| Archive | sha256 |
| --- | --- |
| `gum_2.2.0_darwin_amd64.tar.gz` | `683a5f4005a84441795dd7a368367b3f31a2846b6db9a0c9b749aaa2e627439b` |
| `gum_2.2.0_darwin_arm64.tar.gz` | `e82532be854b609a9968d2f5f1e3d91a0a0fef39945634fd655af86a972c578f` |
| `gum_2.2.0_linux_amd64.tar.gz` | `76b2e176967b68f58d57a3d6177b53ab94dfe0a1b7df5e24313a5e3df6f21251` |
| `gum_2.2.0_linux_arm64.tar.gz` | `7f5847adf4009878ab140f284f394762e7be93fdfd71b984ba06bf6c9206499c` |

The Homebrew tap carries the same four digests at `ehmo/homebrew-tap` commit
`77a75e0`. `brew audit --strict --online --os=all --arch=all ehmo/tap/gum` and
`brew test ehmo/tap/gum` both passed, the tap-drift check passed, and the
installed binary reports `2.2.0` with `gum doctor: all checks passed`.
