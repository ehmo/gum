package main

import "github.com/ehmo/gum/internal/catalog"

// Google Photos Library API v1 OAuth scopes.
//
// As of 2025-03-31 Google REMOVED the broad photoslibrary / photoslibrary.readonly
// scopes — third-party apps may only access media they themselves created. We use
// the current valid scopes (…appcreateddata / appendonly); the old broad
// photoslibrary.readonly is invalid and, if requested, fails the WHOLE OAuth grant
// (it poisons the login union — same class as gmail.metadata). The list/get/search
// ops therefore only see app-created media (typically none for gum); they remain
// wired + gate-correct for when gum uploads via appendonly.
const (
	scopePhotosReadonly   = "https://www.googleapis.com/auth/photoslibrary.readonly.appcreateddata"
	scopePhotosAppendonly = "https://www.googleapis.com/auth/photoslibrary.appendonly"
)

// BuildPhotosOps returns a curated Google Photos Library API v1 surface: albums
// list/get/create and mediaItems list/get/search. Album/mediaItem ids use
// {+albumId}/{+mediaItemId} reserved expansion. typed-rest-sdk, byo_oauth.
func BuildPhotosOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scopes []string, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "photoslibrary", riskClass: risk, scopes: scopes,
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/photoslibrary/v1", goCall: goCall,
		})
	}
	const base = "https://photoslibrary.googleapis.com/v1"
	ro := []string{scopePhotosReadonly}
	return []catalog.Op{
		op("photoslibrary.albums.list", "photoslibrary.v1.rest.albums.list", "List Photo Albums",
			"List the albums in the user's Photos library (pageSize, pageToken).",
			catalog.RiskClassRead, ro, "GET", base+"/albums", "Albums.List"),
		op("photoslibrary.albums.get", "photoslibrary.v1.rest.albums.get", "Get a Photo Album",
			"Fetch an album by id.",
			catalog.RiskClassRead, ro, "GET", base+"/albums/{+albumId}", "Albums.Get"),
		op("photoslibrary.albums.create", "photoslibrary.v1.rest.albums.create", "Create a Photo Album",
			"Create a new album (args.body.album.title).",
			catalog.RiskClassWrite, []string{scopePhotosAppendonly}, "POST", base+"/albums", "Albums.Create"),
		op("photoslibrary.mediaItems.list", "photoslibrary.v1.rest.mediaItems.list", "List Media Items",
			"List the media items in the user's Photos library (pageSize, pageToken).",
			catalog.RiskClassRead, ro, "GET", base+"/mediaItems", "MediaItems.List"),
		op("photoslibrary.mediaItems.get", "photoslibrary.v1.rest.mediaItems.get", "Get a Media Item",
			"Fetch a single media item by id.",
			catalog.RiskClassRead, ro, "GET", base+"/mediaItems/{+mediaItemId}", "MediaItems.Get"),
		op("photoslibrary.mediaItems.search", "photoslibrary.v1.rest.mediaItems.search", "Search Media Items",
			"Search media items by album, date, or content category (args.body: albumId or filters).",
			catalog.RiskClassRead, ro, "POST", base+"/mediaItems:search", "MediaItems.Search"),
	}
}
