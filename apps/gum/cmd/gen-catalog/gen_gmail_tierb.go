package main

import "github.com/ehmo/gum/internal/catalog"

// Gmail v1 OAuth scopes (canonical strings — used in catalog variants and
// scope-manifest cross-references).
const (
	scopeGmailReadonly = "https://www.googleapis.com/auth/gmail.readonly"
	scopeGmailSend     = "https://www.googleapis.com/auth/gmail.send"
	scopeGmailModify   = "https://www.googleapis.com/auth/gmail.modify"
	scopeGmailLabels   = "https://www.googleapis.com/auth/gmail.labels"
	scopeGmailCompose  = "https://www.googleapis.com/auth/gmail.compose"
	scopeGmailMetadata = "https://www.googleapis.com/auth/gmail.metadata"
	scopeGmailSettings = "https://www.googleapis.com/auth/gmail.settings.basic"
	scopeGmailAddrs    = "https://www.googleapis.com/auth/gmail.addons.current.message.metadata"
	scopeGmailFullMail = "https://mail.google.com/"
)

// BuildGmailTierBOps returns the Tier B Gmail v1 surface beyond the four
// dispatch-curated Tier A ops (list, get, send, trash). Spec §5.4 curation
// rule "full defensible surface" requires every Workspace service to expose
// list/get/create/modify/delete coverage so that BM25 search returns relevant
// ops for natural-language queries (gum-ho3 acceptance: `gum search 'gmail
// labels'` returns relevant ops).
//
// Coverage:
//   - threads.{list,get,modify,untrash}        — read+write
//   - threads.trash                             — destructive (matches messages.trash)
//   - messages.{untrash,modify,batchModify}    — write
//   - messages.{delete,batchDelete}            — destructive
//   - labels.{list,get,create,update,patch,delete} — full CRUD
//   - drafts.{list,get,update,send,delete}     — read+write+destructive
//   - history.list                              — change feed (incremental sync)
//   - users.getProfile                          — surface profile metadata
//
// These ops are hardcoded (not discovery-walked) for the same reason
// BuildDriveOps / BuildTasksOps are hardcoded: the Tier B surface is curator-
// curated. Spec §5.4 daily CI regenerates the discovery-walked ops but the
// curated Tier B list is editorially fixed.
func BuildGmailTierBOps() []catalog.Op {
	return []catalog.Op{
		// ── threads ──────────────────────────────────────────────────────
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.threads.list",
			variantID:  "gmail.v1.rest.users.threads.list",
			title:      "List Gmail threads",
			summary:    "List threads in the user's mailbox. Threads group related messages.",
			service:    "gmail",
			riskClass:  catalog.RiskClassRead,
			scopes:     []string{scopeGmailReadonly},
			httpMethod: "GET",
			httpPath:   "/gmail/v1/users/{userId}/threads",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Threads.List",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.threads.get",
			variantID:  "gmail.v1.rest.users.threads.get",
			title:      "Get Gmail thread",
			summary:    "Get a specific thread, including all messages on it.",
			service:    "gmail",
			riskClass:  catalog.RiskClassRead,
			scopes:     []string{scopeGmailReadonly},
			httpMethod: "GET",
			httpPath:   "/gmail/v1/users/{userId}/threads/{id}",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Threads.Get",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.threads.modify",
			variantID:  "gmail.v1.rest.users.threads.modify",
			title:      "Modify Gmail thread labels",
			summary:    "Add or remove labels on every message in a thread.",
			service:    "gmail",
			riskClass:  catalog.RiskClassWrite,
			scopes:     []string{scopeGmailModify},
			httpMethod: "POST",
			httpPath:   "/gmail/v1/users/{userId}/threads/{id}/modify",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Threads.Modify",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:      "gmail.users.threads.trash",
			variantID: "gmail.v1.rest.users.threads.trash",
			title:     "Trash Gmail thread",
			summary:   "Move every message in a thread to the Trash. Permanently deleted after 30 days.",
			service:   "gmail",
			// Destructive to match gmail.users.messages.trash: trashing a whole
			// thread moves every message to Trash (auto-deleted after 30 days), so
			// it must carry the same confirmation gate as single-message trash.
			riskClass:  catalog.RiskClassDestructive,
			scopes:     []string{scopeGmailModify},
			httpMethod: "POST",
			httpPath:   "/gmail/v1/users/{userId}/threads/{id}/trash",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Threads.Trash",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.threads.untrash",
			variantID:  "gmail.v1.rest.users.threads.untrash",
			title:      "Untrash Gmail thread",
			summary:    "Restore a previously trashed thread from Trash back to the mailbox.",
			service:    "gmail",
			riskClass:  catalog.RiskClassWrite,
			scopes:     []string{scopeGmailModify},
			httpMethod: "POST",
			httpPath:   "/gmail/v1/users/{userId}/threads/{id}/untrash",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Threads.Untrash",
		}),
		// ── messages (additional) ────────────────────────────────────────
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.messages.untrash",
			variantID:  "gmail.v1.rest.users.messages.untrash",
			title:      "Untrash Gmail message",
			summary:    "Restore a previously trashed message from Trash back to the mailbox.",
			service:    "gmail",
			riskClass:  catalog.RiskClassWrite,
			scopes:     []string{scopeGmailModify},
			httpMethod: "POST",
			httpPath:   "/gmail/v1/users/{userId}/messages/{id}/untrash",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Messages.Untrash",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.messages.modify",
			variantID:  "gmail.v1.rest.users.messages.modify",
			title:      "Modify Gmail message labels",
			summary:    "Add or remove labels on a single Gmail message.",
			service:    "gmail",
			riskClass:  catalog.RiskClassWrite,
			scopes:     []string{scopeGmailModify},
			httpMethod: "POST",
			httpPath:   "/gmail/v1/users/{userId}/messages/{id}/modify",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Messages.Modify",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.messages.batchModify",
			variantID:  "gmail.v1.rest.users.messages.batchModify",
			title:      "Batch-modify Gmail message labels",
			summary:    "Add or remove labels on up to 1000 messages in a single request.",
			service:    "gmail",
			riskClass:  catalog.RiskClassWrite,
			scopes:     []string{scopeGmailModify},
			httpMethod: "POST",
			httpPath:   "/gmail/v1/users/{userId}/messages/batchModify",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Messages.BatchModify",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.messages.delete",
			variantID:  "gmail.v1.rest.users.messages.delete",
			title:      "Permanently delete Gmail message",
			summary:    "Permanently delete a message. Bypasses Trash; unrecoverable.",
			service:    "gmail",
			riskClass:  catalog.RiskClassDestructive,
			scopes:     []string{scopeGmailFullMail},
			httpMethod: "DELETE",
			httpPath:   "/gmail/v1/users/{userId}/messages/{id}",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Messages.Delete",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.messages.batchDelete",
			variantID:  "gmail.v1.rest.users.messages.batchDelete",
			title:      "Batch-delete Gmail messages",
			summary:    "Permanently delete up to 1000 messages. Bypasses Trash; unrecoverable.",
			service:    "gmail",
			riskClass:  catalog.RiskClassDestructive,
			scopes:     []string{scopeGmailFullMail},
			httpMethod: "POST",
			httpPath:   "/gmail/v1/users/{userId}/messages/batchDelete",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Messages.BatchDelete",
		}),
		// ── labels ───────────────────────────────────────────────────────
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.labels.get",
			variantID:  "gmail.v1.rest.users.labels.get",
			title:      "Get Gmail label",
			summary:    "Get details for a single label by ID.",
			service:    "gmail",
			riskClass:  catalog.RiskClassRead,
			scopes:     []string{scopeGmailReadonly},
			httpMethod: "GET",
			httpPath:   "/gmail/v1/users/{userId}/labels/{id}",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Labels.Get",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.labels.create",
			variantID:  "gmail.v1.rest.users.labels.create",
			title:      "Create Gmail label",
			summary:    "Create a new label in the user's mailbox.",
			service:    "gmail",
			riskClass:  catalog.RiskClassWrite,
			scopes:     []string{scopeGmailLabels},
			httpMethod: "POST",
			httpPath:   "/gmail/v1/users/{userId}/labels",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Labels.Create",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.labels.update",
			variantID:  "gmail.v1.rest.users.labels.update",
			title:      "Update Gmail label",
			summary:    "Replace a label's definition. Use patch for partial updates.",
			service:    "gmail",
			riskClass:  catalog.RiskClassWrite,
			scopes:     []string{scopeGmailLabels},
			httpMethod: "PUT",
			httpPath:   "/gmail/v1/users/{userId}/labels/{id}",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Labels.Update",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.labels.patch",
			variantID:  "gmail.v1.rest.users.labels.patch",
			title:      "Patch Gmail label",
			summary:    "Partial-update a label's definition (only supplied fields are changed).",
			service:    "gmail",
			riskClass:  catalog.RiskClassWrite,
			scopes:     []string{scopeGmailLabels},
			httpMethod: "PATCH",
			httpPath:   "/gmail/v1/users/{userId}/labels/{id}",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Labels.Patch",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.labels.delete",
			variantID:  "gmail.v1.rest.users.labels.delete",
			title:      "Delete Gmail label",
			summary:    "Permanently delete a user-created label. Messages keep their other labels.",
			service:    "gmail",
			riskClass:  catalog.RiskClassDestructive,
			scopes:     []string{scopeGmailLabels},
			httpMethod: "DELETE",
			httpPath:   "/gmail/v1/users/{userId}/labels/{id}",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Labels.Delete",
		}),
		// ── drafts ───────────────────────────────────────────────────────
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.drafts.list",
			variantID:  "gmail.v1.rest.users.drafts.list",
			title:      "List Gmail drafts",
			summary:    "List unsent draft messages in the user's mailbox.",
			service:    "gmail",
			riskClass:  catalog.RiskClassRead,
			scopes:     []string{scopeGmailReadonly},
			httpMethod: "GET",
			httpPath:   "/gmail/v1/users/{userId}/drafts",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Drafts.List",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.drafts.get",
			variantID:  "gmail.v1.rest.users.drafts.get",
			title:      "Get Gmail draft",
			summary:    "Get a specific draft including its current message contents.",
			service:    "gmail",
			riskClass:  catalog.RiskClassRead,
			scopes:     []string{scopeGmailReadonly},
			httpMethod: "GET",
			httpPath:   "/gmail/v1/users/{userId}/drafts/{id}",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Drafts.Get",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.drafts.update",
			variantID:  "gmail.v1.rest.users.drafts.update",
			title:      "Update Gmail draft",
			summary:    "Replace the contents of an existing draft.",
			service:    "gmail",
			riskClass:  catalog.RiskClassWrite,
			scopes:     []string{scopeGmailCompose},
			httpMethod: "PUT",
			httpPath:   "/gmail/v1/users/{userId}/drafts/{id}",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Drafts.Update",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.drafts.send",
			variantID:  "gmail.v1.rest.users.drafts.send",
			title:      "Send Gmail draft",
			summary:    "Send an existing draft. Deletes the draft and creates a sent message.",
			service:    "gmail",
			riskClass:  catalog.RiskClassWrite,
			scopes:     []string{scopeGmailSend},
			httpMethod: "POST",
			httpPath:   "/gmail/v1/users/{userId}/drafts/send",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Drafts.Send",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.drafts.delete",
			variantID:  "gmail.v1.rest.users.drafts.delete",
			title:      "Delete Gmail draft",
			summary:    "Permanently delete a draft. Unrecoverable.",
			service:    "gmail",
			riskClass:  catalog.RiskClassDestructive,
			scopes:     []string{scopeGmailCompose},
			httpMethod: "DELETE",
			httpPath:   "/gmail/v1/users/{userId}/drafts/{id}",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Drafts.Delete",
		}),
		// ── history ──────────────────────────────────────────────────────
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.history.list",
			variantID:  "gmail.v1.rest.users.history.list",
			title:      "List Gmail history",
			summary:    "List the history of mailbox changes since a given historyId. Used for incremental sync.",
			service:    "gmail",
			riskClass:  catalog.RiskClassRead,
			scopes:     []string{scopeGmailMetadata},
			httpMethod: "GET",
			httpPath:   "/gmail/v1/users/{userId}/history",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.History.List",
		}),
		// ── profile ──────────────────────────────────────────────────────
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.getProfile",
			variantID:  "gmail.v1.rest.users.getProfile",
			title:      "Get Gmail profile",
			summary:    "Get the user's Gmail profile: email address, totals, history id.",
			service:    "gmail",
			riskClass:  catalog.RiskClassRead,
			scopes:     []string{scopeGmailMetadata},
			httpMethod: "GET",
			httpPath:   "/gmail/v1/users/{userId}/profile",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.GetProfile",
		}),
		// ── settings ─────────────────────────────────────────────────────
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.settings.getVacation",
			variantID:  "gmail.v1.rest.users.settings.getVacation",
			title:      "Get Gmail vacation responder",
			summary:    "Get vacation responder settings for the user's mailbox.",
			service:    "gmail",
			riskClass:  catalog.RiskClassRead,
			scopes:     []string{scopeGmailSettings},
			httpMethod: "GET",
			httpPath:   "/gmail/v1/users/{userId}/settings/vacation",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Settings.GetVacation",
		}),
		makeWorkspaceOp(workspaceOpSpec{
			opID:       "gmail.users.settings.updateVacation",
			variantID:  "gmail.v1.rest.users.settings.updateVacation",
			title:      "Update Gmail vacation responder",
			summary:    "Replace vacation responder settings for the user's mailbox.",
			service:    "gmail",
			riskClass:  catalog.RiskClassWrite,
			scopes:     []string{scopeGmailSettings},
			httpMethod: "PUT",
			httpPath:   "/gmail/v1/users/{userId}/settings/vacation",
			goPkg:      "google.golang.org/api/gmail/v1",
			goCall:     "Users.Settings.UpdateVacation",
		}),
	}
}
