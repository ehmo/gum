package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	gummcp "github.com/ehmo/gum/internal/mcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// newMCPCmd implements `gum mcp --stdio`.
func newMCPCmd() *cobra.Command {
	var stdio bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server",
		Long:  "Run the gum MCP server. The public release supports --stdio transport.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !stdio {
				return fmt.Errorf("gum mcp: --stdio is required")
			}
			profile := resolveProfileFlag(cmd)
			return runMCPStdio(cmd.Context(), profile)
		},
	}
	cmd.Flags().BoolVar(&stdio, "stdio", false, "Run on stdio transport (required)")
	return cmd
}

func runMCPStdio(parent context.Context, profile string) error {
	ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gummcp.SetVersion(version)
	disp, closeAudit := newDefaultDispatcherWithCloser(profile, true)
	defer func() { _ = closeAudit() }()
	// Same snapshot the CLI dispatcher resolves against, built once by
	// initSessionCatalog during PersistentPreRunE. Passing it here rather
	// than letting the server re-read the embedded catalog is what keeps
	// `gum call plug.<plugin>.<tool>` and gum://op/plug.<plugin>.<tool>
	// agreeing on which plugin ops exist (spec §5 line 405, §13 line 2765).
	srv := gummcp.NewServerWithCatalog(disp, loadCatalog())
	if err := srv.SetProfile(profile); err != nil {
		return err
	}
	return srv.Run(ctx, &sdkmcp.StdioTransport{})
}
