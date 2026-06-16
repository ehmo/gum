package main

import "github.com/ehmo/gum/internal/catalog"

// scopeCalendar is the OAuth scope required for Calendar write operations.
// Read-only ops use https://www.googleapis.com/auth/calendar.readonly, but the
// Tier A write ops (events.insert, events.update) need the full scope.
const scopeCalendar = "https://www.googleapis.com/auth/calendar"

// BuildCalendarWriteOps returns the Tier A Calendar v3 write operations:
//
//	write: calendar.events.insert (POST), calendar.events.update (PUT)
//
// These ops are hardcoded rather than parsed from the calendar discovery doc
// because the existing calendar discovery walk only handles list methods.
// Mirrors the BuildSearchConsoleOps pattern.
func BuildCalendarWriteOps() []catalog.Op {
	return []catalog.Op{
		makeCalendarWriteOp(calendarWriteSpec{
			opID:       "calendar.events.insert",
			variantID:  "calendar.v3.rest.events.insert",
			title:      "Insert a Calendar event",
			summary:    "Create an event on the specified calendar. JSON request body required (args.body): summary, start, end, attendees, etc.",
			httpMethod: "POST",
			httpPath:   "https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events",
			goCall:     "Events.Insert",
		}),
		makeCalendarWriteOp(calendarWriteSpec{
			opID:       "calendar.events.update",
			variantID:  "calendar.v3.rest.events.update",
			title:      "Update a Calendar event",
			summary:    "Update an existing event on the specified calendar by full replacement. JSON request body required (args.body): the entire event resource.",
			httpMethod: "PUT",
			httpPath:   "https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events/{eventId}",
			goCall:     "Events.Update",
		}),
	}
}

// calendarWriteSpec is the internal shape used to declare a Calendar write op.
type calendarWriteSpec struct {
	opID       string
	variantID  string
	title      string
	summary    string
	httpMethod string
	httpPath   string
	goCall     string
}

// makeCalendarWriteOp builds a catalog.Op for one Calendar write operation with
// the conventions shared across all write ops.
func makeCalendarWriteOp(s calendarWriteSpec) catalog.Op {
	return catalog.Op{
		OpID:             s.opID,
		OpSchemaVersion:  1,
		Title:            s.title,
		Summary:          s.summary,
		Service:          "calendar",
		ServiceFamily:    "workspace",
		DefaultVariantID: s.variantID,
		Variants: []catalog.Variant{
			{
				VariantID:            s.variantID,
				VariantSchemaVersion: 1,
				Version:              "v3",
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindTypedRestSDK,
				Preferred:            true,
				RiskClass:            catalog.RiskClassWrite,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Scopes:               []string{scopeCalendar},
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.typed-rest-sdk",
					OperationKey:         s.opID,
					HTTP: &catalog.HTTPBinding{
						Method: s.httpMethod,
						Path:   s.httpPath,
					},
					GoPkg:  "google.golang.org/api/calendar/v3",
					GoCall: s.goCall,
				},
			},
		},
	}
}
