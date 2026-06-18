---
title: gum v1.0.1 release notes
date: 2026-06-18
status: release
---

# gum v1.0.1

`gum auth probe` now checks the OAuth grant you created with `gum auth login`
before it tries ADC.

## Highlights

- `gum auth probe` defaults to `--strategy auto`.
- Auto mode checks the stored BYO OAuth client and grant first.
- `--strategy adc` keeps the old ADC-only check.
- Short scope names such as `adwords` work in probe mode.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ehmo/gum/main/install.sh | GUM_VERSION=v1.0.1 bash
gum --version
gum doctor
```

## Upgrade notes

None.

## Added

None.

## Changed

- `gum auth probe` now prefers BYO OAuth in auto mode when a BYO OAuth client is
  configured.
- Operators can pass `--strategy byo_oauth` or `--strategy adc` to test one auth
  path.

## Fixed

- `gum auth probe --scopes https://www.googleapis.com/auth/adwords` no longer
  ignores a fresh BYO OAuth login and fails against stale ADC credentials with
  `invalid_rapt`.
- Probe scopes are normalized before token resolution.

## Security

No security fixes.

## Known limitations

None.

## Token savings

Measured on the in-tree release fixtures before tagging. This patch does not
change output shaping.

| Default format | Total calls | Total tokens in | Total tokens saved | Aggregate savings |
| --- | ---: | ---: | ---: | ---: |
| `toon` | 10 | 3,922 | 0 | 0 % |
| `json` | 10 | 3,922 | -12 | 0.31 % overhead |

## Reproducibility

```bash
git checkout v1.0.1
cd apps/gum
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=v1.0.1' ./cmd/gum
sha256sum gum
```
