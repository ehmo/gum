package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ehmo/gum/internal/auth"
)

// newTopLevelLogoutCmd is the `gum logout` command — the one-keystroke
// counterpart to `gum login`. It clears gum's locally-stored OAuth credentials
// for the active profile so the next `gum login` starts clean. The primary use
// is switching Google accounts: `gum logout` then `gum login` (whose consent
// flow offers the account chooser via prompt=select_account).
func newTopLevelLogoutCmd() *cobra.Command {
	return newLogoutCmd("logout", "Clear gum's stored OAuth credentials (switch or sign out of a Google account)")
}

// newLogoutCmd builds the logout command body. It is a constructor (mirroring
// newLoginCmd) so the same body can back a future `gum auth logout` alias.
func newLogoutCmd(use, short string) *cobra.Command {
	var forgetClient bool
	cmd := &cobra.Command{
		Use:           use,
		Short:         short,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogout(cmd, forgetClient)
		},
	}
	cmd.Flags().BoolVar(&forgetClient, "forget-client", false,
		"Also remove the registered BYO OAuth client, not just the login grant")
	return cmd
}

// runLogout clears the stored OAuth credentials for the resolved profile and
// reports what was cleared as JSON. Clearing an absent grant is not an error —
// logout is idempotent (running it when already logged out is safe).
func runLogout(cmd *cobra.Command, forgetClient bool) error {
	profile := resolveProfileFlag(cmd)
	res, err := auth.Logout(cmd.Context(), auth.NewOSKeyring(), profile, forgetClient)
	if err != nil {
		return fmt.Errorf("gum logout: %w", err)
	}
	next := "already logged out; nothing to clear"
	switch {
	case res.GrantCleared || res.ClientForgotten:
		next = "run `gum login` to authorize (the browser will offer an account picker)"
		if res.ForgetClientSkipped {
			next += "; --forget-client had nothing to forget (no BYO OAuth client was registered)"
		}
	case res.ForgetClientSkipped:
		next = "no BYO OAuth client was registered to forget; nothing to clear"
	}
	out := map[string]any{
		"profile":          profile,
		"grant_cleared":    res.GrantCleared,
		"client_forgotten": res.ClientForgotten,
		"using_managed":    res.UsingManaged,
		"cleared_at":       time.Now().UTC().Format(time.RFC3339),
		"next":             next,
	}
	if res.ForgetClientSkipped {
		out["forget_client_skipped"] = true
	}
	if res.GrantCleared {
		out["note"] = "OAuth credentials cleared from this machine; refresh token revoked at Google (best-effort)"
	}
	return writeJSON(cmd.OutOrStdout(), out)
}
