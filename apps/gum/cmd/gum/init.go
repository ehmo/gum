package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ehmo/gum/internal/initpkg"
	"github.com/spf13/cobra"
)

// warnIfShadowedOnPath warns when the first `gum` on PATH is a different binary
// than the one currently executing — typically charmbracelet/gum, which shares
// the name. The MCP host launches `gum` by bare name, so a shadowing binary
// would silently break the integration. Best-effort: any resolution error is
// ignored (we just skip the hint). Honors the README's documented behavior.
func warnIfShadowedOnPath(w io.Writer) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	onPath, err := exec.LookPath("gum")
	if err != nil {
		return
	}
	selfResolved, err1 := filepath.EvalSymlinks(self)
	pathResolved, err2 := filepath.EvalSymlinks(onPath)
	if err1 != nil {
		selfResolved = self
	}
	if err2 != nil {
		pathResolved = onPath
	}
	if selfResolved == pathResolved {
		return
	}
	_, _ = fmt.Fprintf(w, "warning: the first `gum` on your PATH is %s, not this binary (%s).\n", pathResolved, selfResolved)
	_, _ = fmt.Fprintln(w, "         This is usually charmbracelet/gum. The MCP host launches `gum` by name,")
	_, _ = fmt.Fprintln(w, "         so place this binary earlier on PATH or use an absolute command path in")
	_, _ = fmt.Fprintln(w, "         the host config below.")
}

// newInitCmd implements `gum init` (spec §12.2). The default behavior is
// diff-and-prompt: show the JSON patch for settings.json, write GUM.md, and
// only mutate after the user confirms (or `--yes`). `--refresh` rewrites
// GUM.md only and leaves settings.json + credentials untouched.
func newInitCmd() *cobra.Command {
	var (
		yes      bool
		global   bool
		refresh  bool
		targetFl string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "First-run installer: patch the MCP host config and write GUM.md",
		Long: "gum init bootstraps GUM for a new user or project. Default behavior is " +
			"diff-and-prompt — gum init never silently patches a security-sensitive file. " +
			"Use --target to pick the host (claude-code | claude-desktop | cursor). " +
			"Use --refresh to regenerate GUM.md only after a gum upgrade.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("init: resolve home directory: %w", err)
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("init: resolve working directory: %w", err)
			}
			out := cmd.OutOrStdout()
			if isTerminal(cmd.ErrOrStderr()) {
				warnIfShadowedOnPath(cmd.ErrOrStderr())
			}
			if refresh {
				dest, werr := initpkg.WriteGUMmd(home, cwd, version, global)
				if werr != nil {
					return werr
				}
				_, _ = fmt.Fprintf(out, "Refreshed %s\n", dest)
				return nil
			}

			target, terr := initpkg.ResolveTarget(home, cwd, os.Getenv("APPDATA"), initpkg.Target(targetFl), global)
			if terr != nil {
				return terr
			}
			plan, perr := initpkg.PlanPatch(target, "gum", initpkg.DefaultMCPEntry())
			if perr != nil {
				return perr
			}

			if plan.NoOp {
				_, _ = fmt.Fprintf(out, "No changes — %s already has the gum MCP entry.\n", plan.Path)
			} else {
				_, _ = fmt.Fprintln(out, initpkg.FormatDiff(plan))
				if !yes {
					if !promptConfirm(cmd.InOrStdin(), out, fmt.Sprintf("Apply this patch to %s? [y/N]: ", plan.Path)) {
						return fmt.Errorf("init: patch declined by user")
					}
				}
				if aerr := initpkg.Apply(target, plan, initpkg.DefaultLockTimeout); aerr != nil {
					return aerr
				}
				_, _ = fmt.Fprintf(out, "Patched %s\n", plan.Path)
			}

			dest, werr := initpkg.WriteGUMmd(home, cwd, version, global)
			if werr != nil {
				return werr
			}
			_, _ = fmt.Fprintf(out, "Wrote %s\n", dest)

			_, _ = fmt.Fprintln(out, "Next:")
			_, _ = fmt.Fprintln(out, "  1. Create a Google Desktop OAuth client.")
			_, _ = fmt.Fprintln(out, "  2. Run `gum auth use-oauth-client --client-id <id> --secret-stdin`.")
			_, _ = fmt.Fprintln(out, "  3. Run `gum login --service gmail,calendar`.")
			_, _ = fmt.Fprintln(out, "  4. Run `gum doctor`.")
			_, _ = fmt.Fprintln(out, "  5. Restart your MCP client.")
			_, _ = fmt.Fprintln(out, "GUM initialized. Run `gum mcp --stdio` to start the MCP server.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip interactive confirmation and apply the patch")
	cmd.Flags().BoolVar(&global, "global", false, "Patch ~/.claude/settings.json and write ~/GUM.md (instead of project-local; claude-code target only)")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Rewrite GUM.md only; do not touch the host config or credentials")
	cmd.Flags().StringVar(&targetFl, "target", string(initpkg.TargetClaudeCode), "MCP host to patch: claude-code | claude-desktop | cursor")
	return cmd
}

// promptConfirm reads a single y/Y answer from stdin. Anything else
// (including EOF, empty line, "n") is rejected. The prompt is written to w.
func promptConfirm(r interface{ Read([]byte) (int, error) }, w interface{ Write([]byte) (int, error) }, prompt string) bool {
	_, _ = fmt.Fprint(w, prompt)
	br := bufio.NewReader(readerOnly{r})
	line, err := br.ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// readerOnly adapts a Write-less reader to bufio. Cobra hands us an
// io.Reader; we accept the broader interface form so tests can substitute
// strings.Reader without importing io.
type readerOnly struct {
	r interface{ Read([]byte) (int, error) }
}

func (r readerOnly) Read(p []byte) (int, error) { return r.r.Read(p) }
