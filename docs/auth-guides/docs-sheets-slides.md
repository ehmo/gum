# Docs, Sheets, and Slides

Enable the Google Docs API, Google Sheets API, and Google Slides API for the
files you plan to work with. Add only the needed scopes to the OAuth consent
screen.

Scopes:

- Docs: `https://www.googleapis.com/auth/documents.readonly`, `https://www.googleapis.com/auth/documents`
- Sheets: `https://www.googleapis.com/auth/spreadsheets.readonly`, `https://www.googleapis.com/auth/spreadsheets`
- Slides: `https://www.googleapis.com/auth/presentations.readonly`, `https://www.googleapis.com/auth/presentations`

Setup:

```shell
gum login --service docs,sheets,slides
gum read docs.documents.get --args '{"documentId":"<doc-id>"}'
gum read sheets.spreadsheets.get --args '{"spreadsheetId":"<sheet-id>"}'
gum read slides.presentations.get --args '{"presentationId":"<presentation-id>"}'
```

Batch update endpoints are writes. Use read scopes for inspection and full
scopes only when the workflow must edit documents.
