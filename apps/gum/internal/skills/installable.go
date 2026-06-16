package skills

import "fmt"

const (
	CoreInstallableDirectory = "gum"
	HaspInstallableDirectory = "gum-hasp"
	MCPInstallableDirectory  = "gum-mcp"
)

type InstallableFile struct {
	Path     string
	Contents string
}

type InstallableSkill struct {
	Directory string
	Files     []InstallableFile
}

func DefaultInstallableSkills() []InstallableSkill {
	installables, _ := InstallableSkills(DefaultRegistry())
	return installables
}

func InstallableSkills(registry Registry) ([]InstallableSkill, error) {
	core, err := registry.Resolve("core", LatestVersion)
	if err != nil {
		return nil, err
	}
	mcp, err := registry.Resolve("mcp", LatestVersion)
	if err != nil {
		return nil, err
	}
	hasp, err := registry.Resolve("hasp", LatestVersion)
	if err != nil {
		return nil, err
	}
	return []InstallableSkill{
		{
			Directory: CoreInstallableDirectory,
			Files: []InstallableFile{
				{
					Path: "SKILL.md",
					Contents: skillMarkdown(
						"gum",
						"Use when an agent needs Google API discovery, gum CLI calls, OAuth setup, or catalog-backed workflows.",
						core.Body,
					),
				},
				{Path: "agents/openai.yaml", Contents: coreOpenAIYAML},
			},
		},
		{
			Directory: HaspInstallableDirectory,
			Files: []InstallableFile{
				{
					Path: "SKILL.md",
					Contents: skillMarkdown(
						"gum-hasp",
						"Use when a gum workflow needs local secrets protected by HASP.",
						hasp.Body,
					),
				},
				{Path: "agents/openai.yaml", Contents: haspOpenAIYAML},
			},
		},
		{
			Directory: MCPInstallableDirectory,
			Files: []InstallableFile{
				{
					Path: "SKILL.md",
					Contents: skillMarkdown(
						"gum-mcp",
						"Use when an agent needs to wire or drive gum MCP tools for guarded Google API work.",
						mcp.Body,
					),
				},
				{Path: "agents/openai.yaml", Contents: mcpOpenAIYAML},
			},
		},
	}, nil
}

func skillMarkdown(name, description, body string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s", name, description, body)
}

const coreOpenAIYAML = `interface:
  display_name: "gum"
  short_description: "Use gum for Google API discovery, OAuth setup, catalog-backed calls, and guarded write flows."
  default_prompt: "Use gum to inspect the Google API catalog, choose the narrow operation and scope, then run the CLI or MCP call with explicit risk gates."
`

const haspOpenAIYAML = `interface:
  display_name: "gum + HASP"
  short_description: "Use HASP when gum work also needs local repo, test, or deploy secrets."
  default_prompt: "Use gum for Google API calls and HASP for non-Google secrets. Keep secret values out of prompts and request short-lived HASP grants for the command."
`

const mcpOpenAIYAML = `interface:
  display_name: "gum MCP"
  short_description: "Wire agents to gum MCP for guarded Google API calls through stdio."
  default_prompt: "Use gum MCP tools for this Google API task. Start with search and describe, keep scopes narrow, and respect write and destructive confirmations."
`
