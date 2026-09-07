---
title: Service coverage
description: "The current generated Google API catalog and where to find service-specific setup notes."
---

# Service coverage

The generated catalog contains 228 operations across 33 services. gum supports
more products than its top-level command list suggests because product coverage
lives in operation IDs such as `gmail.users.messages.list`,
`drive.files.export`, and `sheets.spreadsheets.values.update`.

```bash
gum search "calendar events"
gum describe calendar.events.list
```

Use [Operations by service](services/) when you want to start from a product
page. Use [Command index](commands/) when you need CLI flags, aliases, and
subcommands.

## Covered families

| Family | Examples |
| --- | --- |
| Workspace | Gmail, Calendar, Drive, Docs, Sheets, Slides, Tasks, Admin SDK, Vault, Chat, Meet, Classroom, Forms, Apps Script, People |
| Media and public APIs | YouTube, Photos Library, Search Console, Maps, Custom Search |
| Ads | Google Ads Keyword Planner, GAQL reporting, guarded mutations, conversion uploads, and Data Manager ingestion and request diagnostics |
| Bundled plugins | Flights, Scholar, Patents, Trends, YouTube transcripts |

Some services require setup outside gum: enabled APIs on the OAuth project,
Workspace admin privileges, Photos Library app configuration, Google Ads
developer-token approval, an API key, or a Programmable Search Engine `cx`.

Detailed setup lives in [auth guides](auth-guides/), including the separate
[Data Manager OAuth setup](auth-guides/google-ads.md#data-manager).
[Service matrix](service-matrix.md) records operation depth and dated live checks.

## Why service pages matter

gog exposes many product-specific commands. gum uses fewer commands because the
catalog is the product surface. The generated service pages close that lookup
gap: each page lists operation IDs, risk class, auth strategy, and the command
shape to use for the first call.
