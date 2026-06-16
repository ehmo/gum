# Maps and Custom Search

Maps and Custom Search use API keys in the v1 catalog.

Setup:

```shell
printf '%s' "$GOOGLE_API_KEY" | gum auth use-api-key --stdin
gum read maps.timezone.get --args '{"location":"37.7749,-122.4194","timestamp":0}'
gum read customsearch.cse.list --args '{"q":"gum","cx":"<search-engine-id>"}'
```

Enable the required Maps Platform APIs or Custom Search API in Google Cloud.
Custom Search also needs a Programmable Search Engine ID in `cx`.
