package main

import "github.com/ehmo/gum/internal/catalog"

// Google Chat API v1 OAuth scopes.
const (
	scopeChatSpacesReadonly      = "https://www.googleapis.com/auth/chat.spaces.readonly"
	scopeChatMessages            = "https://www.googleapis.com/auth/chat.messages"
	scopeChatMessagesReadonly    = "https://www.googleapis.com/auth/chat.messages.readonly"
	scopeChatMembershipsReadonly = "https://www.googleapis.com/auth/chat.memberships.readonly"
)

// BuildChatOps returns the Google Chat API v1 surface: spaces list/get,
// messages list/get/create/update/delete, members list. Spaces, messages, and
// members are addressed by resource name via {+name}/{+parent} reserved
// expansion (e.g. spaces/AAA, spaces/AAA/messages/BBB). typed-rest-sdk,
// byo_oauth. NOTE: Chat user-OAuth requires a Workspace account with Chat
// enabled; some methods are app(service-account)-only — gum surfaces the API
// error verbatim.
func BuildChatOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scopes []string, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "chat", riskClass: risk, scopes: scopes,
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/chat/v1", goCall: goCall,
		})
	}
	const base = "https://chat.googleapis.com/v1"
	roSpaces := []string{scopeChatSpacesReadonly}
	roMsg := []string{scopeChatMessagesReadonly}
	rwMsg := []string{scopeChatMessages}
	roMem := []string{scopeChatMembershipsReadonly}
	return []catalog.Op{
		op("chat.spaces.list", "chat.v1.rest.spaces.list", "List Chat Spaces",
			"List the Chat spaces the caller is a member of.",
			catalog.RiskClassRead, roSpaces, "GET", base+"/spaces", "Spaces.List"),
		op("chat.spaces.get", "chat.v1.rest.spaces.get", "Get a Chat Space",
			"Fetch a Chat space by resource name (spaces/AAA).",
			catalog.RiskClassRead, roSpaces, "GET", base+"/{+name}", "Spaces.Get"),
		op("chat.spaces.messages.list", "chat.v1.rest.spaces.messages.list", "List Chat Messages",
			"List messages in a Chat space (parent=spaces/AAA).",
			catalog.RiskClassRead, roMsg, "GET", base+"/{+parent}/messages", "Spaces.Messages.List"),
		op("chat.spaces.messages.get", "chat.v1.rest.spaces.messages.get", "Get a Chat Message",
			"Fetch a single Chat message by resource name (spaces/AAA/messages/BBB).",
			catalog.RiskClassRead, roMsg, "GET", base+"/{+name}", "Spaces.Messages.Get"),
		op("chat.spaces.messages.create", "chat.v1.rest.spaces.messages.create", "Send a Chat Message",
			"Post a message to a Chat space (parent=spaces/AAA; args.body.text).",
			catalog.RiskClassWrite, rwMsg, "POST", base+"/{+parent}/messages", "Spaces.Messages.Create"),
		op("chat.spaces.messages.update", "chat.v1.rest.spaces.messages.update", "Update a Chat Message",
			"Update a Chat message's text/cards by resource name (requires updateMask).",
			catalog.RiskClassWrite, rwMsg, "PUT", base+"/{+name}", "Spaces.Messages.Update"),
		op("chat.spaces.messages.delete", "chat.v1.rest.spaces.messages.delete", "Delete a Chat Message",
			"Delete a Chat message by resource name. Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, rwMsg, "DELETE", base+"/{+name}", "Spaces.Messages.Delete"),
		op("chat.spaces.members.list", "chat.v1.rest.spaces.members.list", "List Chat Space Members",
			"List the members of a Chat space (parent=spaces/AAA).",
			catalog.RiskClassRead, roMem, "GET", base+"/{+parent}/members", "Spaces.Members.List"),
	}
}
