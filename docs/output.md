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

### CSV and markdown

CSV output is one table with one header row. A response that carries top-level
scalars beside its record array, such as `nextPageToken`, gets those scalars as
extra columns repeated on every row. Each array the table did not take becomes
one column holding its item count, for example `[2 items]`. When the page holds
no records, gum writes those columns on their own. A scalar whose name collides
with a record column gets a `_` prefix, so both values survive.

Markdown output does not truncate cells. Cell length is the profile's
`truncate_strings` job. The aligned `table` format still caps a cell at 60
terminal columns, because it is a terminal layout rather than a document.

## Field masks

When an operation supports a Google partial-response `fields` parameter, pass
the narrow fields needed by the task:

```bash
gum read drive.files.list \
  --args '{"pageSize":10,"fields":"files(id,name,mimeType),nextPageToken"}' \
  --output json
```

This reduces upstream payload size before gum applies any local shaping.

## Result caps

An expression profile can cap the number of rows it returns. The two builtin
Google Ads keyword profiles cap at 100 and 50 results. Pass `--max-items` to
replace that cap for one call:

```bash
gum read googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics \
  --args "$(cat keywords.json)" --max-items all --format json
```

`--max-items` takes a positive integer or `all`. `all` returns every row. The
flag is available on `gum read`, `gum write`, `gum destructive`, and `gum call`.
MCP callers pass the same value as the `max_items` argument on `gum.read`,
`gum.write`, and `gum.destructive`.

When rows are omitted, gum writes a note to stderr that states how many rows it
dropped and names the count key, for example `results_omitted_count=143`. The
override does not change the cache key, so a capped and an uncapped call are
separate cache entries.

## Gain replay

`gum gain --fixture-replay` runs the deterministic local fixture replay used by
the current checkout. It is a regression tool, not a billing promise.

```bash
gum gain --fixture-replay --format toon
gum gain --fixture-replay --format json
```

Report gain numbers with the command, format, binary version, and fixture set
that produced them.
