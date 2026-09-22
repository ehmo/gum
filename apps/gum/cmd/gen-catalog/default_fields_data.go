package main

// default_fields_data.go is the central, reviewable source of the curated
// spec §9.1 stage-1 default field masks. Each entry is a Google
// partial-response `fields` value that dispatch injects as the `fields` query
// arg when a profile omits `field_mask` (internal/dispatch/lifecycle.go step
// 3c). Apply it offline with:
//
//	go run ./cmd/gen-catalog -apply-default-fields
//
// Scope: high-traffic read ops whose upstream response is the FULL resource,
// so a mask removes payload the caller almost never reads. Ops are left out on
// purpose when a mask would not pay or would lose data:
//
//   - gmail.users.messages.list / threads.list / drafts.list already return
//     only ids plus paging, so a mask saves nothing.
//   - gmail.users.messages.get would have to guess whether the caller wants
//     the body; dropping `payload` by default is a silent data loss.
//   - drive.files.list already defaults to files(kind,id,name,mimeType); a
//     mask there would WIDEN the response, not narrow it.
//   - people.connections.list is already narrowed by its `personFields` arg.
//   - youtube.videos.list is narrowed by its `part` arg, which the caller
//     sets explicitly; only search.list and playlistItems.list carry the
//     thumbnail pyramid a `part` cannot exclude.
//   - docs.documents.get returns the document body, which IS the payload.
//
// Every mask here is checked against the upstream response schema by
// TestDefaultFieldsMatchDiscoverySchema, which resolves
// testdata/default-fields-schema.json. Regenerate that fixture from the live
// Discovery documents with:
//
//	go run ./cmd/gen-catalog -emit-default-fields-schema
//
// A mask naming a field the upstream schema does not define fails the test and
// the apply pass, so a typo cannot reach catalog.json and silently blank a
// response.
func tierADefaultFields() map[string]string {
	return map[string]string{
		// Label carries four counters plus colour and two visibility enums.
		// Listing labels is nearly always "which labels exist"; the counters
		// stay because a mailbox summary reads them.
		"gmail.users.labels.list": "labels(id,name,type,messagesTotal,messagesUnread)",

		// About embeds exportFormats and importFormats, two mime-to-mime maps
		// that dominate the response and that no gum caller reads.
		"drive.about.get": "user(displayName,emailAddress,photoLink)," +
			"storageQuota,maxUploadSize,appInstalled,canCreateDrives",

		// Event has 44 properties and Calendar returns all of them. This keeps
		// the 24 that describe when, what, where and who, and drops
		// conferenceData, gadget, extendedProperties, the four
		// *Properties blocks and the guestsCan* flags.
		"calendar.events.list": calendarEventsMask,

		// instances returns the same Events envelope as list.
		"calendar.events.instances": calendarEventsMask,

		// The single-event read keeps conferenceData on top of the list set:
		// one event's joining information is cheap and usually the point.
		"calendar.events.get": "id,status,summary,description,location,start,end," +
			"recurrence,recurringEventId,originalStartTime,transparency,eventType," +
			"htmlLink,hangoutLink,conferenceData,created,updated," +
			"creator(email,displayName,self),organizer(email,displayName,self)," +
			"attendees(email,displayName,responseStatus,optional,organizer,self,resource)," +
			"reminders,colorId,visibility,sequence,attachments",

		"calendar.calendarList.list": "nextPageToken,nextSyncToken," +
			"items(id,summary,summaryOverride,description,location,timeZone," +
			"primary,selected,hidden,deleted,accessRole,colorId," +
			"backgroundColor,foregroundColor)",

		// selfLink is the largest field on a Task and gum never dereferences
		// it; etag and kind are equally dead on the read path.
		"tasks.tasks.list": "nextPageToken,items(id,title,status,due,completed," +
			"notes,parent,position,updated,hidden,deleted,webViewLink)",

		"tasks.tasklists.list": "nextPageToken,items(id,title,updated)",

		// ThumbnailDetails carries up to eight sizes, each with url, width and
		// height. One size is what a caller renders, and `part` cannot drop
		// the other seven.
		"youtube.search.list": "nextPageToken,prevPageToken,regionCode,pageInfo," +
			"items(id,snippet(publishedAt,channelId,title,description," +
			"channelTitle,liveBroadcastContent,thumbnails(default)))",

		// Same thumbnail pyramid. contentDetails and status stay whole so the
		// mask never contradicts the caller's `part`.
		"youtube.playlistItems.list": "nextPageToken,prevPageToken,pageInfo," +
			"items(id,contentDetails,status,snippet(publishedAt,channelId,title," +
			"description,channelTitle,playlistId,position,resourceId," +
			"videoOwnerChannelId,videoOwnerChannelTitle,thumbnails(default)))",

		// Drops charts, conditionalFormats, filterViews, protectedRanges,
		// merges, bandedRanges, tables and the dimension groups on every
		// sheet. `sheets/data` stays: includeGridData=true callers ask for
		// exactly that, and a mask that dropped it would answer their request
		// with an empty grid.
		"sheets.spreadsheets.get": "spreadsheetId,spreadsheetUrl," +
			"properties(title,locale,timeZone,autoRecalc),namedRanges," +
			"sheets(properties(sheetId,title,index,sheetType,hidden,gridProperties),data)",
	}
}

// calendarEventsMask is shared by events.list and events.instances, which
// return the same Events envelope.
const calendarEventsMask = "nextPageToken,nextSyncToken,timeZone,accessRole,summary,updated," +
	"items(id,status,summary,description,location,start,end," +
	"recurrence,recurringEventId,originalStartTime,transparency,eventType," +
	"htmlLink,hangoutLink,created,updated," +
	"creator(email,displayName,self),organizer(email,displayName,self)," +
	"attendees(email,displayName,responseStatus,optional,organizer,self,resource)," +
	"reminders,colorId,visibility,sequence,attachments)"
