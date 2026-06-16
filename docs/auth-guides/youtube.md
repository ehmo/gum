# YouTube Data API

Enable the YouTube Data API v3 in Google Cloud. Add YouTube scopes to the OAuth
consent screen.

Scopes:

- `https://www.googleapis.com/auth/youtube.readonly`
- `https://www.googleapis.com/auth/youtube`

Setup:

```shell
gum login --service youtube
gum read youtube.videos.list --args '{"part":"snippet","chart":"mostPopular","maxResults":1}'
```

Account-specific reads may require the signed-in account to have a YouTube
channel.
