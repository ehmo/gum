---
title: Plugins
description: "How gum treats bundled and third-party Google-adjacent plugins."
---

# Plugins

`gum` has a first-party catalog for supported Google APIs and a plugin path for
Google-adjacent services that do not fit the normal OAuth catalog model.

Bundled plugin examples include Flights, Scholar, Patents, Trends, and YouTube
transcripts.

## Inspect plugins

```bash
gum plugin list
gum plugin info <name>
gum plugin setup <name>
```

Plugin state belongs to the selected profile. A plugin can be active,
installed-pending-restart, needs-configuration, or quarantined.

## Trust boundary

Plugins are not a generic raw shell bridge. The host validates plugin manifests,
credential descriptors, risk classes, output profiles, and returned envelopes.
Shape 1 plugins still run as subprocesses, so install only plugins from sources
you trust.

For authoring details, read the [plugin contract](plugin-contract.md) and
[plugin author guide](plugin-author-guide.md).
