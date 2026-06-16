package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ehmo/gum/internal/agents"
	"github.com/ehmo/gum/internal/fsatomic"
	skillreg "github.com/ehmo/gum/internal/skills"
	"github.com/spf13/cobra"
)

type setupResult struct {
	Request   setupRequest `json:"request"`
	AgentPlan agents.Plan  `json:"agent_plan"`
	Applied   bool         `json:"applied"`
	Next      []string     `json:"next"`
}

type setupRequest struct {
	Target   string   `json:"target"`
	Scope    string   `json:"scope"`
	Features []string `json:"features"`
	Toolset  string   `json:"toolset"`
	DryRun   bool     `json:"dry_run,omitempty"`
	Force    bool     `json:"force,omitempty"`
}

func newSetupCmd() *cobra.Command {
	var (
		target   string
		scope    string
		features string
		toolset  string
		mcpArgs  []string
		force    bool
		dryRun   bool
		yes      bool
		format   string
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install gum agent skills and MCP config, then print the first-run proof",
		Long:  "Guided setup for a local gum install. It writes agent skills and MCP config for Codex, Claude, Cursor, or Gemini, then prints the Google OAuth and doctor commands needed for first success.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			parsedFeatures := splitCSV(features)
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("setup: resolve home directory: %w", err)
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("setup: resolve working directory: %w", err)
			}
			plan, err := agents.Install(agents.Options{
				Target:   target,
				Scope:    scope,
				Features: parsedFeatures,
				Toolset:  toolset,
				MCPArgs:  mcpArgs,
				Force:    force,
				DryRun:   true,
				HomeDir:  home,
				WorkDir:  cwd,
			})
			if err != nil {
				return err
			}
			result := setupResult{
				Request: setupRequest{
					Target:   normalizedTarget(target),
					Scope:    normalizedScope(scope),
					Features: normalizedFeatures(parsedFeatures),
					Toolset:  normalizedToolset(toolset),
					DryRun:   dryRun,
					Force:    force,
				},
				AgentPlan: plan,
				Next:      setupNextSteps(),
			}
			if dryRun {
				return writeSetupResult(cmd, format, result)
			}
			if format != "json" {
				printSetupIntro(cmd.OutOrStdout(), result.Request)
				printAgentPlan(cmd.OutOrStdout(), plan)
			}
			if !yes && !promptConfirm(cmd.InOrStdin(), cmd.OutOrStdout(), "Apply these changes? [y/N]: ") {
				return errors.New("setup cancelled; pass --yes to run non-interactively")
			}
			plan, err = agents.Install(agents.Options{
				Target:   target,
				Scope:    scope,
				Features: parsedFeatures,
				Toolset:  toolset,
				MCPArgs:  mcpArgs,
				Force:    force,
				DryRun:   false,
				HomeDir:  home,
				WorkDir:  cwd,
			})
			if err != nil {
				return err
			}
			result.AgentPlan = plan
			result.Applied = true
			return writeSetupResult(cmd, format, result)
		},
	}
	cmd.Flags().StringVar(&target, "target", agents.TargetAll, "Agent target: codex|claude|cursor|gemini|all")
	cmd.Flags().StringVar(&scope, "scope", agents.ScopeUser, "Install scope: user|project")
	cmd.Flags().StringVar(&features, "features", "skills,mcp", "Comma-separated features: skills,mcp")
	cmd.Flags().StringVar(&toolset, "toolset", agents.DefaultToolset, "MCP toolset (core)")
	cmd.Flags().StringArrayVar(&mcpArgs, "mcp-arg", nil, "Extra argument appended to the gum mcp command")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing skill files and replace managed MCP blocks")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the write plan without changing files")
	cmd.Flags().BoolVar(&yes, "yes", false, "Apply without an interactive confirmation")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text|json")
	return cmd
}

func newAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Install gum skills and MCP config for coding agents",
	}
	cmd.AddCommand(newAgentsInstallCmd())
	return cmd
}

func newAgentsInstallCmd() *cobra.Command {
	var (
		target   string
		scope    string
		features string
		toolset  string
		mcpArgs  []string
		force    bool
		dryRun   bool
		format   string
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install gum agent files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("agents install: resolve home directory: %w", err)
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("agents install: resolve working directory: %w", err)
			}
			plan, err := agents.Install(agents.Options{
				Target:   target,
				Scope:    scope,
				Features: splitCSV(features),
				Toolset:  toolset,
				MCPArgs:  mcpArgs,
				Force:    force,
				DryRun:   dryRun,
				HomeDir:  home,
				WorkDir:  cwd,
			})
			if err != nil {
				return err
			}
			if format == "json" {
				return writeJSON(cmd.OutOrStdout(), plan)
			}
			printAgentPlan(cmd.OutOrStdout(), plan)
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", agents.TargetAll, "Agent target: codex|claude|cursor|gemini|all")
	cmd.Flags().StringVar(&scope, "scope", agents.ScopeUser, "Install scope: user|project")
	cmd.Flags().StringVar(&features, "features", "skills,mcp", "Comma-separated features: skills,mcp")
	cmd.Flags().StringVar(&toolset, "toolset", agents.DefaultToolset, "MCP toolset (core)")
	cmd.Flags().StringArrayVar(&mcpArgs, "mcp-arg", nil, "Extra argument appended to the gum mcp command")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing skill files and replace managed MCP blocks")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the write plan without changing files")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text|json")
	return cmd
}

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "List, print, export, or install version-matched agent skills",
	}
	cmd.AddCommand(newSkillsListCmd(), newSkillsShowCmd(), newSkillsExportCmd(), newSkillsInstallCmd())
	return cmd
}

func newSkillsListCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List embedded gum agent skills",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items := skillreg.DefaultRegistry().List()
			if format == "json" {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"skills": items})
			}
			for _, item := range items {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s min=%s sha256=%s bytes=%d - %s\n", item.Name, item.Version, item.MinGum, item.SHA256, item.Bytes, item.Summary)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text|json")
	return cmd
}

func newSkillsShowCmd() *cobra.Command {
	var version, format string
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Print an embedded gum skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skill, err := skillreg.DefaultRegistry().Resolve(args[0], version)
			if err != nil {
				return err
			}
			if format == "json" {
				return writeJSON(cmd.OutOrStdout(), skill)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "name: %s\nversion: %s\nmin_gum: %s\nsha256: %s\nbytes: %d\n\n%s", skill.Name, skill.Version, skill.MinGum, skill.SHA256, skill.Bytes, skill.Body)
			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", skillreg.LatestVersion, "Skill version selector")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text|json")
	return cmd
}

func newSkillsExportCmd() *cobra.Command {
	var out, format string
	var force bool
	cmd := &cobra.Command{
		Use:   "export --out <dir>",
		Short: "Export installable gum skills",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(out) == "" {
				return errors.New("skills export: --out is required")
			}
			written, err := writeInstallableSkills(out, skillreg.DefaultInstallableSkills(), force, false)
			if err != nil {
				return err
			}
			if format == "json" {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"dir": out, "written": written})
			}
			printSkillsWritten(cmd.OutOrStdout(), written, false)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "Output directory")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing files")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text|json")
	return cmd
}

func newSkillsInstallCmd() *cobra.Command {
	var target, dir, format string
	var force, dryRun bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install gum skills for Codex-compatible agents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := resolveSkillsInstallDir(target, dir)
			if err != nil {
				return err
			}
			written, err := writeInstallableSkills(resolved, skillreg.DefaultInstallableSkills(), force, dryRun)
			if err != nil {
				return err
			}
			if format == "json" {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"target":  normalizedSkillsTarget(target),
					"dir":     resolved,
					"dry_run": dryRun,
					"written": written,
				})
			}
			printSkillsWritten(cmd.OutOrStdout(), written, dryRun)
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "codex", "Skills target: codex")
	cmd.Flags().StringVar(&dir, "dir", "", "Install directory (default: $CODEX_HOME/skills or ~/.codex/skills)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print files without changing disk")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text|json")
	return cmd
}

func writeSetupResult(cmd *cobra.Command, format string, result setupResult) error {
	if format == "json" {
		return writeJSON(cmd.OutOrStdout(), result)
	}
	if !result.Applied {
		printSetupIntro(cmd.OutOrStdout(), result.Request)
		printAgentPlan(cmd.OutOrStdout(), result.AgentPlan)
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "dry run complete")
		return nil
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "setup complete")
	printAgentPlan(cmd.OutOrStdout(), result.AgentPlan)
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Next:")
	for i, step := range result.Next {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", i+1, step)
	}
	return nil
}

func printSetupIntro(w io.Writer, req setupRequest) {
	_, _ = fmt.Fprintln(w, "gum setup")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "This will install agent skills and MCP config, then point you at the Google OAuth and doctor checks.")
	_, _ = fmt.Fprintf(w, "Agent target: %s\nScope: %s\nFeatures: %s\nToolset: %s\n\n", req.Target, req.Scope, strings.Join(req.Features, ","), req.Toolset)
}

func setupNextSteps() []string {
	return []string{
		"Create a Google OAuth desktop client in your own Google Cloud project.",
		"Run `gum auth use-oauth-client --client-id <id> --secret-stdin`.",
		"Run `gum login --service gmail,calendar,drive`.",
		"Run `gum doctor`.",
		"Restart your MCP client.",
	}
}

func printAgentPlan(w io.Writer, plan agents.Plan) {
	for _, action := range plan.Actions {
		_, _ = fmt.Fprintf(w, "%s %s %s %s\n", action.Status, action.Target, action.Kind, filepath.ToSlash(action.Path))
	}
}

func printSkillsWritten(w io.Writer, written []string, dryRun bool) {
	verb := "wrote"
	if dryRun {
		verb = "would write"
	}
	for _, path := range written {
		_, _ = fmt.Fprintf(w, "%s %s\n", verb, filepath.ToSlash(path))
	}
}

func resolveSkillsInstallDir(target, dir string) (string, error) {
	if normalizedSkillsTarget(target) != "codex" {
		return "", fmt.Errorf("unsupported skills target: %s", target)
	}
	if dir != "" {
		return dir, nil
	}
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		return filepath.Join(codexHome, "skills"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve codex skills directory: %w", err)
	}
	return filepath.Join(home, ".codex", "skills"), nil
}

func writeInstallableSkills(base string, installables []skillreg.InstallableSkill, force, dryRun bool) ([]string, error) {
	if strings.TrimSpace(base) == "" {
		return nil, errors.New("skills output directory is required")
	}
	var written []string
	for _, item := range installables {
		for _, file := range item.Files {
			rel, err := installableRelativePath(item.Directory, file.Path)
			if err != nil {
				return nil, err
			}
			written = append(written, rel)
			if dryRun {
				continue
			}
			dest := filepath.Join(base, rel)
			if _, err := os.Lstat(dest); err == nil && !force {
				return nil, fmt.Errorf("%s already exists; pass --force to overwrite", dest)
			} else if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				return nil, err
			}
			if err := fsatomic.WriteFile(dest, []byte(file.Contents), 0o600); err != nil {
				return nil, err
			}
		}
	}
	return written, nil
}

func installableRelativePath(dir, file string) (string, error) {
	if dir == "" || filepath.Clean(dir) != dir || filepath.IsAbs(dir) || strings.ContainsAny(dir, `/\`) || strings.HasPrefix(dir, ".") {
		return "", fmt.Errorf("invalid installable skill directory: %s", dir)
	}
	clean := filepath.Clean(file)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid installable skill file path: %s", file)
	}
	return filepath.Join(dir, clean), nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizedTarget(raw string) string {
	if raw == "" {
		return agents.TargetAll
	}
	return raw
}

func normalizedScope(raw string) string {
	if raw == "" {
		return agents.ScopeUser
	}
	return raw
}

func normalizedFeatures(raw []string) []string {
	if len(raw) == 0 {
		return []string{agents.FeatureSkills, agents.FeatureMCP}
	}
	return raw
}

func normalizedToolset(raw string) string {
	if raw == "" {
		return agents.DefaultToolset
	}
	return raw
}

func normalizedSkillsTarget(raw string) string {
	if raw == "" {
		return "codex"
	}
	return raw
}
