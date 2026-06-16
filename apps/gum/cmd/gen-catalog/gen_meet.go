package main

import "github.com/ehmo/gum/internal/catalog"

// Google Meet API v2 OAuth scopes.
const (
	scopeMeetSpaceCreated  = "https://www.googleapis.com/auth/meetings.space.created"
	scopeMeetSpaceReadonly = "https://www.googleapis.com/auth/meetings.space.readonly"
)

// BuildMeetOps returns the Google Meet API v2 surface: create/get a meeting
// space, and read conference records + their recordings/transcripts. Spaces,
// records, recordings, and transcripts are addressed by resource name via
// {+name}/{+parent}. typed-rest-sdk, byo_oauth.
func BuildMeetOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scope, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "meet", riskClass: risk, scopes: []string{scope},
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/meet/v2", goCall: goCall,
		})
	}
	const base = "https://meet.googleapis.com/v2"
	return []catalog.Op{
		op("meet.spaces.create", "meet.v2.rest.spaces.create", "Create a Meet Space",
			"Create a new Meet meeting space; returns the meetingUri + space name.",
			catalog.RiskClassWrite, scopeMeetSpaceCreated, "POST", base+"/spaces", "Spaces.Create"),
		op("meet.spaces.get", "meet.v2.rest.spaces.get", "Get a Meet Space",
			"Fetch a meeting space by resource name (spaces/<id>) or meeting code.",
			catalog.RiskClassRead, scopeMeetSpaceReadonly, "GET", base+"/{+name}", "Spaces.Get"),
		op("meet.conferenceRecords.list", "meet.v2.rest.conferenceRecords.list", "List Conference Records",
			"List past conference records for meetings the caller organized (filter, pageSize).",
			catalog.RiskClassRead, scopeMeetSpaceReadonly, "GET", base+"/conferenceRecords", "ConferenceRecords.List"),
		op("meet.conferenceRecords.get", "meet.v2.rest.conferenceRecords.get", "Get a Conference Record",
			"Fetch a conference record by resource name (conferenceRecords/<id>).",
			catalog.RiskClassRead, scopeMeetSpaceReadonly, "GET", base+"/{+name}", "ConferenceRecords.Get"),
		op("meet.conferenceRecords.recordings.list", "meet.v2.rest.conferenceRecords.recordings.list", "List Conference Recordings",
			"List the recordings of a conference record (parent=conferenceRecords/<id>).",
			catalog.RiskClassRead, scopeMeetSpaceReadonly, "GET", base+"/{+parent}/recordings", "ConferenceRecords.Recordings.List"),
		op("meet.conferenceRecords.transcripts.list", "meet.v2.rest.conferenceRecords.transcripts.list", "List Conference Transcripts",
			"List the transcripts of a conference record (parent=conferenceRecords/<id>).",
			catalog.RiskClassRead, scopeMeetSpaceReadonly, "GET", base+"/{+parent}/transcripts", "ConferenceRecords.Transcripts.List"),
	}
}
