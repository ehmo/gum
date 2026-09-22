package adapters

import (
	"fmt"
	"net/http"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// Google refuses some calls for a reason no re-login can clear: a Workspace
// administrator has blocked the client, or an organisation policy forbids the
// service. Spec §7 already has names for those gaps in the auth_components
// enum, and the compound pre-flight reports them before a call goes out. This
// file carries the other half: when the refusal only becomes visible in the
// response, the same component name reaches the caller.
//
// Note on "admin_policy_enforced": that string is an OAuth 2.0 authorization
// endpoint error ("Error 400: admin_policy_enforced"), raised while the user
// is still on Google's consent page. It is not a value of
// google.api.ErrorReason and can never appear in an API response body, so it
// is deliberately absent from the table below. Its runtime counterpart, for a
// token that was issued and then blocked, is USER_BLOCKED_BY_ADMIN.

// policyRefusalReasons maps a Google refusal reason onto the spec §7 auth
// component the caller is missing. It is the only place in the codebase that
// spells a Google reason string.
//
// Reason strings and their meanings come from
// google/api/error_reason.proto (googleapis master). The one lowercase entry
// is a legacy Discovery reason from error.errors[].reason, which Drive v3
// still emits and which has no ErrorReason equivalent.
var policyRefusalReasons = map[string]catalog.AuthComponentKind{
	// The caller's Workspace customer blocks its users from the service.
	"USER_BLOCKED_BY_ADMIN": catalog.AuthComponentWorkspaceAdminTrust,
	// Drive v3: "The domain administrators have disabled Drive apps."
	"domainPolicy": catalog.AuthComponentWorkspaceAdminTrust,

	// Org policy service restrictions. All three are cleared by an
	// organisation policy administrator, not by the end user.
	"RESOURCE_USAGE_RESTRICTION_VIOLATED": catalog.AuthComponentOrgPolicyException,
	"ENDPOINT_USAGE_RESTRICTION_VIOLATED": catalog.AuthComponentOrgPolicyException,
	"ORG_RESTRICTION_VIOLATION":           catalog.AuthComponentOrgPolicyException,
}

// policyRefusalMessages explains each component in the caller's terms. Keyed
// by component so both reasons for one gap share a single sentence.
var policyRefusalMessages = map[catalog.AuthComponentKind]string{
	catalog.AuthComponentWorkspaceAdminTrust: "A Google Workspace administrator has blocked this application for your domain. " +
		"Ask an administrator to trust the OAuth client, then run the setup command again.",
	catalog.AuthComponentOrgPolicyException: "An organisation policy forbids this service or endpoint for your resource. " +
		"Ask an organisation policy administrator for an exception, then run the setup command again.",
}

// AsStructuredError satisfies dispatch.StructuredErrorCarrier. It returns the
// spec §7 auth envelope when the upstream refusal names a policy gap, and nil
// for every other failure so the dispatch boundary keeps its own mapping.
//
// The envelope is scoped to HTTP 403. A mapped reason on another status is
// not a policy refusal: 401 means the credential itself is bad, and the
// existing auth path already handles that.
func (e *UpstreamError) AsStructuredError() *dispatch.StructuredError {
	if e == nil || e.HTTPStatus != http.StatusForbidden {
		return nil
	}
	component, mapped := policyRefusalReasons[e.ErrorReason]
	if !mapped {
		return nil
	}

	se := dispatch.NewStructuredError(dispatch.ErrCodeAuthRequired, e.Message).
		WithRetryable(false).
		WithDetail("missing_components", []string{string(component)}).
		WithDetail("error_reason", e.ErrorReason).
		WithDetail("http_status", e.HTTPStatus).
		WithDetail("user_message", policyRefusalMessages[component])

	if e.ErrorDomain != "" {
		se = se.WithDetail("error_domain", e.ErrorDomain)
	}
	if e.AuthStrategy != "" {
		se = se.WithDetail("auth_strategy", e.AuthStrategy)
	}
	if e.OpID != "" {
		se = se.WithDetail("op_id", e.OpID).
			WithDetail("setup_command", fmt.Sprintf("gum auth setup %s", e.OpID))
	}
	return se
}
