package main_test

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"

	gencatalog "github.com/ehmo/gum/cmd/gen-catalog"
)

// minimalGmailJSON is the smallest gmail discovery doc that passes
// every "required resource/method" gate in GenerateFromDiscoveries.
// Trim a single field per test to drive each gate's miss arm.
const minimalGmailJSON = `{
  "name": "gmail",
  "version": "v1",
  "resources": {
    "users": {
      "resources": {
        "messages": {
          "methods": {
            "list":  {"id": "gmail.users.messages.list", "httpMethod": "GET",  "path": "/gmail/v1/users/{userId}/messages"},
            "get":   {"id": "gmail.users.messages.get",  "httpMethod": "GET",  "path": "/gmail/v1/users/{userId}/messages/{id}"},
            "send":  {"id": "gmail.users.messages.send", "httpMethod": "POST", "path": "/gmail/v1/users/{userId}/messages/send"},
            "trash": {"id": "gmail.users.messages.trash","httpMethod": "POST", "path": "/gmail/v1/users/{userId}/messages/{id}/trash"}
          }
        },
        "labels": {"methods": {"list":   {"id": "gmail.users.labels.list",  "httpMethod": "GET",  "path": "/gmail/v1/users/{userId}/labels"}}},
        "drafts": {"methods": {"create": {"id": "gmail.users.drafts.create","httpMethod": "POST", "path": "/gmail/v1/users/{userId}/drafts"}}}
      }
    }
  }
}`

const minimalCalendarJSON = `{
  "name": "calendar",
  "version": "v3",
  "resources": {
    "events":       {"methods": {"list": {"id": "calendar.events.list",        "httpMethod": "GET", "path": "/calendar/v3/calendars/{calendarId}/events"}}},
    "calendarList": {"methods": {"list": {"id": "calendar.calendarList.list",  "httpMethod": "GET", "path": "/calendar/v3/users/me/calendarList"}}}
  }
}`

// TestGenerateFromDiscoveriesValidationGates pins each "required-
// resource-or-method missing" error arm. Without these gates, a
// regressed discovery doc would silently produce a partial catalog
// that fails Validate() with a much less actionable error.
func TestGenerateFromDiscoveriesValidationGates(t *testing.T) {
	cases := []struct {
		name     string
		gmail    io.Reader
		calendar io.Reader
		wantWrap string
	}{
		{
			name:     "read_gmail_err",
			gmail:    iotest.ErrReader(io.ErrUnexpectedEOF),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "read gmail discovery doc",
		},
		{
			name:     "parse_gmail_err",
			gmail:    strings.NewReader("{not json"),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "parse gmail discovery doc",
		},
		{
			name:     "read_calendar_err",
			gmail:    strings.NewReader(minimalGmailJSON),
			calendar: iotest.ErrReader(io.ErrUnexpectedEOF),
			wantWrap: "read calendar discovery doc",
		},
		{
			name:     "parse_calendar_err",
			gmail:    strings.NewReader(minimalGmailJSON),
			calendar: strings.NewReader("{not json"),
			wantWrap: "parse calendar discovery doc",
		},
		{
			name:     "missing_gmail_users",
			gmail:    strings.NewReader(`{"name":"gmail","resources":{}}`),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "resources.users",
		},
		{
			name:     "missing_gmail_users_messages",
			gmail:    strings.NewReader(`{"name":"gmail","resources":{"users":{"resources":{}}}}`),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "resources.users.resources.messages",
		},
		{
			name:     "missing_gmail_messages_list",
			gmail:    strings.NewReader(stripGmailMethod("list")),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "messages.methods.list",
		},
		{
			name:     "missing_gmail_messages_get",
			gmail:    strings.NewReader(stripGmailMethod("get")),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "messages.methods.get",
		},
		{
			name:     "missing_gmail_messages_send",
			gmail:    strings.NewReader(stripGmailMethod("send")),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "messages.methods.send",
		},
		{
			name:     "missing_gmail_messages_trash",
			gmail:    strings.NewReader(stripGmailMethod("trash")),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "messages.methods.trash",
		},
		{
			name: "missing_gmail_labels",
			gmail: strings.NewReader(`{"name":"gmail","resources":{"users":{"resources":{
				"messages":{"methods":{"list":{},"get":{},"send":{},"trash":{}}}
			}}}}`),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "resources.labels",
		},
		{
			name: "missing_gmail_labels_list",
			gmail: strings.NewReader(`{"name":"gmail","resources":{"users":{"resources":{
				"messages":{"methods":{"list":{},"get":{},"send":{},"trash":{}}},
				"labels":{"methods":{}}
			}}}}`),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "labels.methods.list",
		},
		{
			name: "missing_gmail_drafts",
			gmail: strings.NewReader(`{"name":"gmail","resources":{"users":{"resources":{
				"messages":{"methods":{"list":{},"get":{},"send":{},"trash":{}}},
				"labels":{"methods":{"list":{}}}
			}}}}`),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "resources.drafts",
		},
		{
			name: "missing_gmail_drafts_create",
			gmail: strings.NewReader(`{"name":"gmail","resources":{"users":{"resources":{
				"messages":{"methods":{"list":{},"get":{},"send":{},"trash":{}}},
				"labels":{"methods":{"list":{}}},
				"drafts":{"methods":{}}
			}}}}`),
			calendar: strings.NewReader(minimalCalendarJSON),
			wantWrap: "drafts.methods.create",
		},
		{
			name:     "missing_calendar_events",
			gmail:    strings.NewReader(minimalGmailJSON),
			calendar: strings.NewReader(`{"name":"calendar","resources":{}}`),
			wantWrap: "resources.events",
		},
		{
			name:     "missing_calendar_events_list",
			gmail:    strings.NewReader(minimalGmailJSON),
			calendar: strings.NewReader(`{"name":"calendar","resources":{"events":{"methods":{}}}}`),
			wantWrap: "events.methods.list",
		},
		{
			name:     "missing_calendar_calendarList",
			gmail:    strings.NewReader(minimalGmailJSON),
			calendar: strings.NewReader(`{"name":"calendar","resources":{"events":{"methods":{"list":{}}}}}`),
			wantWrap: "resources.calendarList",
		},
		{
			name:  "missing_calendar_calendarList_list",
			gmail: strings.NewReader(minimalGmailJSON),
			calendar: strings.NewReader(`{"name":"calendar","resources":{
				"events":{"methods":{"list":{}}},
				"calendarList":{"methods":{}}
			}}`),
			wantWrap: "calendarList.methods.list",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gencatalog.GenerateFromDiscoveries(tc.gmail, tc.calendar)
			if err == nil {
				t.Fatal("want validation error; got nil")
			}
			if !strings.Contains(err.Error(), tc.wantWrap) {
				t.Errorf("err=%v; want %q substring", err, tc.wantWrap)
			}
		})
	}
}

// stripGmailMethod builds a gmail discovery doc that's identical to
// minimalGmailJSON except the named method on resources.users.resources.messages
// is omitted. Used to drive the "missing message method" gates.
func stripGmailMethod(omit string) string {
	methods := map[string]string{
		"list":  `"list":{"id":"gmail.users.messages.list","httpMethod":"GET","path":"/gmail/v1/users/{userId}/messages"}`,
		"get":   `"get":{"id":"gmail.users.messages.get","httpMethod":"GET","path":"/gmail/v1/users/{userId}/messages/{id}"}`,
		"send":  `"send":{"id":"gmail.users.messages.send","httpMethod":"POST","path":"/gmail/v1/users/{userId}/messages/send"}`,
		"trash": `"trash":{"id":"gmail.users.messages.trash","httpMethod":"POST","path":"/gmail/v1/users/{userId}/messages/{id}/trash"}`,
	}
	delete(methods, omit)
	var parts []string
	for _, v := range methods {
		parts = append(parts, v)
	}
	return `{"name":"gmail","resources":{"users":{"resources":{
		"messages":{"methods":{` + strings.Join(parts, ",") + `}},
		"labels":{"methods":{"list":{"id":"gmail.users.labels.list","httpMethod":"GET","path":"/gmail/v1/users/{userId}/labels"}}},
		"drafts":{"methods":{"create":{"id":"gmail.users.drafts.create","httpMethod":"POST","path":"/gmail/v1/users/{userId}/drafts"}}}
	}}}}`
}
