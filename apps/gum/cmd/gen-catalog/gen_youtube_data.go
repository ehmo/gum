package main

import "github.com/ehmo/gum/internal/catalog"

// YouTube Data API v3 OAuth scopes.
const (
	scopeYouTubeReadonly = "https://www.googleapis.com/auth/youtube.readonly"
	scopeYouTube         = "https://www.googleapis.com/auth/youtube"
)

// BuildYouTubeDataOps returns the OFFICIAL YouTube Data API v3 surface (read +
// playlist mutations), distinct from the unofficial youtube.transcripts.get
// plugin op. search/list reads cover videos/channels/playlists/subscriptions;
// writes cover playlist + playlistItem create/delete. Every read/list method
// requires the `part` query param (enriched from the Discovery doc).
//
// Uses makeWorkspaceOp (typed-rest-sdk, byo_oauth). Quota note: the YouTube
// Data API bills quota units per call (search.list = 100 units); gum does not
// model that — callers are responsible for their daily quota.
func BuildYouTubeDataOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scopes []string, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "youtube", riskClass: risk, scopes: scopes,
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/youtube/v3", goCall: goCall,
		})
	}
	ro := []string{scopeYouTubeReadonly}
	rw := []string{scopeYouTube}
	const base = "https://youtube.googleapis.com/youtube/v3"
	return []catalog.Op{
		// --- reads ---
		op("youtube.search.list", "youtube.v3.rest.search.list", "Search YouTube",
			"Search YouTube for videos, channels, and playlists (part=snippet; q, type, channelId, order, maxResults, …). Costs 100 quota units.",
			catalog.RiskClassRead, ro, "GET", base+"/search", "Search.List"),
		op("youtube.videos.list", "youtube.v3.rest.videos.list", "List Videos",
			"Fetch video resources by id (part=snippet,contentDetails,statistics) or chart=mostPopular.",
			catalog.RiskClassRead, ro, "GET", base+"/videos", "Videos.List"),
		op("youtube.channels.list", "youtube.v3.rest.channels.list", "List Channels",
			"Fetch channel resources by id, forUsername, or mine=true (part=snippet,statistics,contentDetails).",
			catalog.RiskClassRead, ro, "GET", base+"/channels", "Channels.List"),
		op("youtube.playlists.list", "youtube.v3.rest.playlists.list", "List Playlists",
			"Fetch playlist resources by id, channelId, or mine=true (part=snippet,contentDetails).",
			catalog.RiskClassRead, ro, "GET", base+"/playlists", "Playlists.List"),
		op("youtube.playlistItems.list", "youtube.v3.rest.playlistItems.list", "List Playlist Items",
			"Fetch the items of a playlist (part=snippet,contentDetails; playlistId or id).",
			catalog.RiskClassRead, ro, "GET", base+"/playlistItems", "PlaylistItems.List"),
		op("youtube.subscriptions.list", "youtube.v3.rest.subscriptions.list", "List Subscriptions",
			"Fetch subscription resources (part=snippet; mine=true or channelId).",
			catalog.RiskClassRead, ro, "GET", base+"/subscriptions", "Subscriptions.List"),
		// --- playlist writes ---
		op("youtube.playlists.insert", "youtube.v3.rest.playlists.insert", "Create a Playlist",
			"Create a new playlist (part=snippet,status; args.body: snippet.title, status.privacyStatus).",
			catalog.RiskClassWrite, rw, "POST", base+"/playlists", "Playlists.Insert"),
		op("youtube.playlists.delete", "youtube.v3.rest.playlists.delete", "Delete a Playlist",
			"Delete a playlist by id. Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, rw, "DELETE", base+"/playlists", "Playlists.Delete"),
		op("youtube.playlistItems.insert", "youtube.v3.rest.playlistItems.insert", "Add a Playlist Item",
			"Add a video to a playlist (part=snippet; args.body: snippet.playlistId, snippet.resourceId.videoId).",
			catalog.RiskClassWrite, rw, "POST", base+"/playlistItems", "PlaylistItems.Insert"),
		op("youtube.playlistItems.delete", "youtube.v3.rest.playlistItems.delete", "Remove a Playlist Item",
			"Remove an item from a playlist by id. Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, rw, "DELETE", base+"/playlistItems", "PlaylistItems.Delete"),
	}
}
