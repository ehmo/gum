---
title: Support
description: "Where to look when gum setup or API calls fail."
---

# Support

Start with the local checks. They separate credential, catalog, and Google API
setup problems before you open an issue.

```bash
gum doctor
gum auth status
gum search "gmail messages"
gum describe gmail.users.messages.list
```

## Common paths

| Problem | Start here |
| --- | --- |
| Install or PATH issue | [`Install`](install.md) |
| First API call | [`Quickstart`](quickstart.md) |
| OAuth, API key, service account, or ADC setup | [`Auth`](auth.md) |
| Service-specific Google setup | [`Google Auth Guides`](auth-guides/README.md) |
| Agent client configuration | [`Agent setup`](agent-setup.md) |
| Operation lookup | [`Operations by service`](services/README.md) |
| CLI flags | [`Commands`](commands/README.md) |

## Contributing

Contributor workflow lives in
[`CONTRIBUTING.md`](https://github.com/ehmo/gum/blob/main/CONTRIBUTING.md).
New Google API coverage should also follow [`Catalog ABI`](catalog-abi.md) and
the [`Test Matrix`](test-matrix.md).
