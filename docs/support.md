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

## OAuth connection timeouts

If `AUTH_REFRESH_FAILED` reports a timeout at `oauth2.googleapis.com/token`,
check network access before replacing credentials. A firewall can require a
new approval after a Homebrew upgrade changes the executable's versioned path.

```bash
command -v gum
gum --version
brew --prefix gum
gum auth probe --strategy byo_oauth --scopes datamanager
```

For a Homebrew installation, the executable is under the formula prefix at
`libexec/gum`. Approve its HTTPS access to `oauth2.googleapis.com` and the API
you intend to call, such as `datamanager.googleapis.com`. If the firewall needs
interactive approval, wait until the operator is present, then retry the probe.

When the probe succeeds, repeat the validation-only request from the
[Data Manager guide](auth-guides/google-ads.md#data-manager).

## Contributing

Contributor workflow lives in
the README's [development section](https://github.com/ehmo/gum#development).
New Google API coverage should also follow [`Catalog ABI`](catalog-abi.md) and
the [`Test Matrix`](test-matrix.md).
