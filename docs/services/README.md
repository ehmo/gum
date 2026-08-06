---
title: Operations by service
description: "Generated service pages for the Google API and plugin operations in gum."
---

# Operations by service

gum ships 222 catalog operations across 32 services. The CLI does not expose one top-level command per Google product; product coverage lives in the catalog. Use these pages to start from a service, then call the selected operation with `gum read`, `gum write`, or `gum destructive`.

```bash
gum search "gmail messages"
gum describe gmail.users.messages.list
gum read gmail.users.messages.list --args '{"userId":"me","maxResults":5}' --output json
```

## Workspace documents

- [Apps Script](script.md) - 5 operations; 3 read, 2 write.
- [Docs](docs.md) - 3 operations; 1 read, 2 write.
- [Drive](drive.md) - 15 operations; 2 destructive, 8 read, 5 write.
- [Forms](forms.md) - 5 operations; 3 read, 2 write.
- [Sheets](sheets.md) - 9 operations; 3 read, 6 write.
- [Slides](slides.md) - 3 operations; 1 read, 2 write.

## Workspace communication

- [Calendar](calendar.md) - 31 operations; 4 destructive, 12 read, 15 write.
- [Chat](chat.md) - 8 operations; 1 destructive, 5 read, 2 write.
- [Gmail](gmail.md) - 30 operations; 6 destructive, 11 read, 13 write.
- [Meet](meet.md) - 6 operations; 5 read, 1 write.
- [Tasks](tasks.md) - 12 operations; 2 destructive, 4 read, 6 write.

## Workspace administration

- [Admin Reports](adminreports.md) - 4 operations; 4 read.
- [Admin SDK](admin.md) - 14 operations; 3 destructive, 6 read, 5 write.
- [Cloud Identity](cloudidentity.md) - 4 operations; 4 read.
- [Groups Settings](groupssettings.md) - 2 operations; 1 read, 1 write.
- [Vault](vault.md) - 6 operations; 1 destructive, 2 read, 3 write.

## People and education

- [Classroom](classroom.md) - 10 operations; 1 destructive, 6 read, 3 write.
- [People](people.md) - 10 operations; 2 destructive, 5 read, 3 write.

## Search and media

- [Custom Search](customsearch.md) - 1 operation; 1 read.
- [Indexing](indexing.md) - 2 operations; 1 read, 1 write.
- [Photos Library](photoslibrary.md) - 6 operations; 5 read, 1 write.
- [Search Console](searchconsole.md) - 10 operations; 2 destructive, 6 read, 2 write.
- [YouTube](youtube.md) - 11 operations; 2 destructive, 7 read, 2 write.

## Ads and maps

- [Google Ads](googleads.md) - 3 operations; 3 read.
- [Maps](maps.md) - 3 operations; 3 read.
- [Places](places.md) - 2 operations; 2 read.
- [Routes](routes.md) - 2 operations; 2 read.

## Research and travel

- [Flights](flights.md) - 1 operation; 1 read.
- [Patents](patents.md) - 1 operation; 1 read.
- [Scholar](scholar.md) - 1 operation; 1 read.
- [Trends](trends.md) - 1 operation; 1 read.

## Internal

- [Sandbox](meta.md) - 1 operation; 1 read.

## How to read these pages

- `Risk` is the variant risk class gum enforces at dispatch time.
- `Auth` is the credential strategy required by the default variant.
- Request fields are listed on individual operation descriptions from `gum describe <op_id>`.
- Google project setup still matters. Enable the API and authorize scopes before calling an operation.
