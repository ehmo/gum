# Photos Library

Enable the Photos Library API in Google Cloud. Configure the Photos Library app
access for your OAuth project.

Scopes:

- `https://www.googleapis.com/auth/photoslibrary.readonly.appcreateddata`
- `https://www.googleapis.com/auth/photoslibrary.appendonly`

Setup:

```shell
gum login --service photoslibrary
gum read photoslibrary.albums.list --args '{"pageSize":5}'
```

Photos Library restricts what third-party apps can read. A valid OAuth grant can
still fail if the app has not created any media or lacks Photos Library product
setup.
