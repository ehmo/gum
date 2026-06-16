# Search Console

Enable the Search Console API in Google Cloud. The Google account must have
access to the target Search Console property.

Scopes:

- `https://www.googleapis.com/auth/webmasters.readonly`
- `https://www.googleapis.com/auth/webmasters`

Setup:

```shell
gum login --service searchconsole
gum read searchconsole.sites.list
```

Use verified property identifiers such as `sc-domain:example.com` or a full URL
property. Write operations such as sitemap submit/delete and site add/delete
need the full `webmasters` scope.
