package auth

// Gmail scope capability chain. gmail.metadata is the narrowest read scope AND
// is "poisonous": when it is present in an access token, Google's Gmail API
// rejects messages.get / threads.get with format=FULL (HTTP 403 "Metadata scope
// doesn't allow format FULL") even when a broader gmail scope is ALSO granted.
// So a login union that requested both gmail.metadata and gmail.readonly would
// silently break the most common Gmail read.
const (
	scopeGmailMetadata = "https://www.googleapis.com/auth/gmail.metadata"
	scopeGmailReadonly = "https://www.googleapis.com/auth/gmail.readonly"
	scopeGmailModify   = "https://www.googleapis.com/auth/gmail.modify"
	scopeGmailFull     = "https://mail.google.com/"
)

// gmailFullReadScopes grant a superset of gmail.metadata's read capability
// (they permit format=FULL) and therefore subsume it.
var gmailFullReadScopes = []string{scopeGmailReadonly, scopeGmailModify, scopeGmailFull}

// scopeSubsumedBy reports whether `required` is granted by some BROADER scope in
// `have` (not merely an exact match). Today this encodes only the one Google
// scope relationship gum must act on — gmail.metadata is subsumed by any
// full-read gmail scope — because gmail.metadata is the sole scope whose mere
// presence changes API behavior. Extend deliberately; over-broad subsumption
// could mask a genuinely missing scope.
func scopeSubsumedBy(required string, have map[string]bool) bool {
	if required == scopeGmailMetadata {
		for _, s := range gmailFullReadScopes {
			if have[s] {
				return true
			}
		}
	}
	return false
}

// ExpandGrantedScopes returns granted plus any scope that is SUBSUMED by a scope
// already present, so the dispatch policy gate (an exact-match allowlist) accepts
// an op whose required scope is covered by a broader grant. It is the inverse of
// PruneLoginScopes: login deliberately drops gmail.metadata from the token, so
// without this the two ops that declare gmail.metadata (users.history.list,
// users.getProfile) would be permanently SCOPE_MISSING after a standard login
// even though the granted gmail.readonly fully covers them. Additive and
// order-preserving; mirrors scopeSubsumedBy, so it stays as conservative.
func ExpandGrantedScopes(granted []string) []string {
	have := make(map[string]bool, len(granted))
	for _, s := range granted {
		have[s] = true
	}
	out := append([]string(nil), granted...)
	if !have[scopeGmailMetadata] && scopeSubsumedBy(scopeGmailMetadata, have) {
		out = append(out, scopeGmailMetadata)
	}
	return out
}

// PruneLoginScopes drops a scope from a login union when a broader scope that
// subsumes it is also present. Concretely it removes the FULL-format-poisoning
// gmail.metadata when a full-read gmail scope is in the set, so a single
// `gum login` does not mint a token that can't fetch full messages. Order is
// preserved; the input is not mutated.
func PruneLoginScopes(scopes []string) []string {
	have := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		have[s] = true
	}
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if scopeSubsumedBy(s, have) {
			continue
		}
		out = append(out, s)
	}
	return out
}
