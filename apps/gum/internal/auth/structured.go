package auth

import (
	"github.com/ehmo/gum/internal/dispatch"
)

// AsStructuredError renders the §7 auth envelope as the dispatch kernel's own
// error type, so the code and every envelope field survive the trip to the CLI
// and to MCP.
//
// Without this the kernel sees a plain error and rewrites it as AUTH_REQUIRED
// with only the Error() string, which collapses AUTH_KEYCHAIN_UNAVAILABLE,
// BYO_OAUTH_CLIENT_NOT_CONFIGURED, AUTH_STRATEGY_NOT_IMPLEMENTED and
// GUM_OAUTH_MANAGED_CLIENT_NOT_READY into one code and drops the
// auth_strategy / missing_components / setup_command trio that spec §7
// lines 1378-1381 make mandatory for every non-gum_oauth failure.
//
// Empty fields are omitted so the envelope keeps the same key set as
// MarshalJSON. Retryable is always set, because false is the meaningful
// default for an auth failure the user must act on.
func (e *AuthError) AsStructuredError() *dispatch.StructuredError {
	if e == nil {
		return nil
	}

	code := e.Code
	if code == "" {
		code = "AUTH_REQUIRED"
	}

	user := e.UserMessage
	if user == "" {
		user = e.HumanRemediation
	}

	se := dispatch.NewStructuredError(dispatch.ErrorCode(code), e.HumanRemediation).
		WithRetryable(e.Retryable)

	for key, value := range map[string]string{
		"auth_strategy": e.Strategy,
		"op_id":         e.OpID,
		"setup_command": e.SetupCommand,
		"user_message":  user,
	} {
		if value != "" {
			se = se.WithDetail(key, value)
		}
	}

	for key, value := range map[string][]string{
		"missing_components": e.MissingComponents,
		"required_scopes":    e.RequiredScopes,
		"have_scopes":        e.HaveScopes,
	} {
		if len(value) > 0 {
			se = se.WithDetail(key, value)
		}
	}

	return se
}
