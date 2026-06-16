package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// newCallDispatcher is the dispatcher constructor `gum call` uses. It takes the
// resolved --profile so the call uses that profile's audit log, scope
// allowlist, and config (not a hardcoded "default"). It is a package var so
// tests can inject a fake that fails the first dispatch with AUTH_REQUIRED or
// SCOPE_MISSING and succeeds the retry, exercising the JIT re-dispatch path.
var newCallDispatcher = newDefaultDispatcherForProfile

// jitStdinInteractive reports whether stdin is an interactive terminal. JIT
// authorization only prompts when true, so agents and piped input never block
// on a Y/n question — they receive the structured auth error to act on
// programmatically. It is a package var so tests can force interactivity
// without allocating a real PTY.
var jitStdinInteractive = func(cmd *cobra.Command) bool {
	return readerIsTTY(cmd.InOrStdin())
}

// maybeJITLogin offers a just-in-time browser authorization when `gum call`
// fails for lack of credentials, then reports whether the caller should retry
// the dispatch once. It fires only when ALL hold:
//
//   - the failure is an AUTH_REQUIRED or SCOPE_MISSING structured error,
//   - the resolved variant uses byo_oauth (the only strategy a loopback login
//     can satisfy),
//   - an OAuth client is registered (`gum auth use-oauth-client`),
//   - stdin is an interactive terminal, and
//   - the operator answers yes to the [Y/n] prompt.
//
// It authorizes exactly the resolved variant's scopes so the stored refresh
// token is keyed to match what the retry's resolver will look up. Any other
// case returns false, leaving the original structured error untouched for the
// CLI/agent to surface.
func maybeJITLogin(cmd *cobra.Command, opID, variantID string, derr error) bool {
	if !jitEligibleAuthError(derr) {
		return false
	}
	scopes, ok := jitByoScopes(loadCatalog(), opID, variantID)
	if !ok {
		return false
	}
	if !jitStdinInteractive(cmd) {
		return false
	}
	client, ok, err := auth.LoadByoClient(auth.NewOSKeyring(), resolveProfileFlag(cmd))
	if err != nil || !ok {
		// No client (or unreadable keychain): the structured error already
		// points the operator at `gum auth use-oauth-client`.
		return false
	}
	if !confirmAuthorize(cmd.InOrStdin(), cmd.ErrOrStderr(), scopes) {
		return false
	}
	if _, lerr := interactiveByoLogin(cmd.Context(),
		auth.ByoOAuthConfig{ClientID: client.ClientID, ClientSecret: client.ClientSecret, Profile: resolveProfileFlag(cmd), Scopes: scopes},
		newBrowserOpener(cmd.ErrOrStderr(), false, isHeadless),
	); lerr != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "gum: authorization failed: %v\n", lerr)
		return false
	}
	return true
}

func jitEligibleAuthError(err error) bool {
	return dispatch.IsStructuredError(err, dispatch.ErrCodeAuthRequired) ||
		dispatch.IsStructuredError(err, dispatch.ErrCodeScopeMissing)
}

// jitByoScopes returns the OAuth scopes a JIT login must authorize for opID,
// but only when the resolved variant uses the byo_oauth strategy. variantID
// pins a specific variant; empty selects the op's default_variant_id. Returns
// ok=false for an unknown op, a missing/unknown variant, a non-byo_oauth
// strategy, or a variant that declares no scopes — all cases where a loopback
// login would not help.
func jitByoScopes(cat *catalog.Catalog, opID, variantID string) ([]string, bool) {
	if cat == nil {
		return nil, false
	}
	for i := range cat.Ops {
		op := &cat.Ops[i]
		if op.OpID != opID {
			continue
		}
		v := defaultVariant(op)
		if variantID != "" {
			v = nil
			for j := range op.Variants {
				if op.Variants[j].VariantID == variantID {
					v = &op.Variants[j]
					break
				}
			}
		}
		if v == nil || v.AuthStrategy != catalog.AuthStrategyBYOOAuth || len(v.Scopes) == 0 {
			return nil, false
		}
		return append([]string{}, v.Scopes...), true
	}
	return nil, false
}

// confirmAuthorize prints the scopes about to be granted and a `[Y/n]` prompt,
// then reads a yes/no answer. Empty input (a bare Enter) defaults to yes,
// matching the capitalized default; a line beginning with y/yes is yes;
// anything else is no.
func confirmAuthorize(in io.Reader, out io.Writer, scopes []string) bool {
	_, _ = fmt.Fprintln(out, "gum needs your authorization to access:")
	for _, s := range scopes {
		_, _ = fmt.Fprintf(out, "  • %s\n", s)
	}
	_, _ = fmt.Fprint(out, "Open your browser to authorize now? [Y/n] ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return true
	default:
		return false
	}
}

// readerIsTTY reports whether r is a character device (an interactive
// terminal). Dependency-free: it stats the underlying *os.File and checks the
// ModeCharDevice bit, so non-file readers and regular files / pipes are not
// treated as interactive.
func readerIsTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
