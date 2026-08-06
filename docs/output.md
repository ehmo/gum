---
title: Output shaping
description: "How gum keeps API output smaller and script-friendly without hiding the raw operation contract."
---

# Output shaping

Google APIs often return large JSON objects. `gum` keeps operation discovery and
responses smaller with catalog search, field masks, compact output formats, and
expression profiles.

## Formats

Common CLI paths accept `--output`:

```bash
gum read gmail.users.messages.list \
  --args '{"userId":"me","maxResults":5}' \
  --output json
```

Supported output modes vary by command. Use the generated command page for the
specific flag set.

## Field masks

When an operation supports a Google partial-response `fields` parameter, pass
the narrow fields needed by the task:

```bash
gum read drive.files.list \
  --args '{"pageSize":10,"fields":"files(id,name,mimeType),nextPageToken"}' \
  --output json
```

This reduces upstream payload size before gum applies any local shaping.

## Gain replay

`gum gain --fixture-replay` runs the deterministic local fixture replay used by
the current checkout. It is a regression tool, not a billing promise.

```bash
gum gain --fixture-replay --format toon
gum gain --fixture-replay --format json
```

Report gain numbers with the command, format, binary version, and fixture set
that produced them.
