# gum Google Service Coverage Matrix

Current generated catalog: 222 ops across 32 services.

The v1 release supports broad catalog discovery and dispatch. Google auth is
operator-owned: users bring their own OAuth client, API key, service account,
or ADC source based on the catalog variant.

## Depth Status

| Service | Current status | Representative surface |
| --- | --- | --- |
| Drive | Covered | File create, update, copy, delete, export, permission read/update/delete |
| Docs | Covered | Document get plus `docs.documents.batchUpdate` |
| Sheets | Covered | Spreadsheet create/get/batchUpdate and values batchGet/batchUpdate/append/clear |
| Slides | Covered | Presentation create/get, page reads, batchUpdate |
| Tasks | Covered | Tasklist and task list/get/insert/update/delete |
| Admin SDK | Partial write coverage | Directory user/group/member reads plus selected group/member/user writes |

Admin SDK write coverage stays narrow because tenant-wide mutations need strict
policy gates. See [admin-write-policy-gate.md](./admin-write-policy-gate.md).

## Breadth Canary Evidence

Latest recorded live run: 2026-06-12 PDT.

| Service | Representative command | Result | Notes |
| --- | --- | --- | --- |
| YouTube Data | `gum call youtube.videos.list --risk=read part=snippet chart=mostPopular maxResults:=1 --json` | Passing | HTTP 200 |
| Search Console | `gum call searchconsole.sites.list --risk=read --json` | Passing | HTTP 200 |
| Search Console | `gum call searchconsole.searchanalytics.query --risk=read siteUrl=<verified-site> startDate=2026-06-05 endDate=2026-06-12 dimensions:='["query"]' --json` | Passing | HTTP 200 with a verified property |
| Classroom | `gum call classroom.courses.list --risk=read pageSize:=1 --json` | Passing | HTTP 200; empty course lists are valid |
| Meet | `gum call meet.conferenceRecords.list --risk=read pageSize:=1 --json` | Passing | HTTP 200 |
| People | `gum call people.people.get --risk=read resourceName=people/me personFields=names,emailAddresses --json` | Needs scope/product setup | Google returned `PERMISSION_DENIED`; grant the exact missing scope |
| Photos Library | `gum call photoslibrary.albums.list --risk=read pageSize:=1 --json` | Needs product setup | Requires Photos Library app setup |
| Chat | `gum call chat.spaces.list --risk=read pageSize:=1 --json` | Needs product setup | Google returned `Google Chat app not found` |
| Google Ads | `gum call googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics --risk=read customerId=<customer-id> keywords:='["gum"]' --json` | Needs auth/product setup | Requires `adwords`, developer token, and customer access |
| Maps | `gum call maps.timezone.get --risk=read location=37.7749,-122.4194 timestamp:=0 --json` | Needs API key | Store an API key with Maps APIs enabled |
| Custom Search | `gum call customsearch.cse.list --risk=read q=gum cx=<search-engine-id> --json` | Needs API key/product setup | Requires API key and Programmable Search Engine `cx` |

## Credential Setup

BYO OAuth:

```shell
printf '%s' "$GOOGLE_OAUTH_CLIENT_SECRET" \
  | gum auth use-oauth-client --client-id "$GOOGLE_OAUTH_CLIENT_ID" --secret-stdin
gum login --service gmail,calendar
gum doctor
```

API key:

```shell
printf '%s' "$GOOGLE_API_KEY" | gum auth use-api-key --stdin
```

Service account:

```shell
gum auth use-service-account /path/to/key.json
```

Detailed setup by API family lives in [auth-guides](./auth-guides/README.md).
