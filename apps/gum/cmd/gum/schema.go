package main

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandSchema struct {
	SchemaVersion int            `json:"schema_version"`
	Command       *schemaCommand `json:"command"`
}

type schemaCommand struct {
	Name        string          `json:"name"`
	Path        string          `json:"path"`
	Usage       string          `json:"usage,omitempty"`
	Help        string          `json:"help,omitempty"`
	Aliases     []string        `json:"aliases,omitempty"`
	Arguments   []schemaArg     `json:"arguments,omitempty"`
	Flags       []schemaFlag    `json:"flags,omitempty"`
	Subcommands []schemaCommand `json:"subcommands,omitempty"`
}

type schemaArg struct {
	Name string `json:"name"`
	Help string `json:"help,omitempty"`
}

type schemaFlag struct {
	Name       string   `json:"name"`
	Short      string   `json:"short,omitempty"`
	Type       string   `json:"type"`
	Default    string   `json:"default,omitempty"`
	HasDefault bool     `json:"has_default"`
	Help       string   `json:"help,omitempty"`
	Aliases    []string `json:"aliases,omitempty"`
}

func newSchemaCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the runtime command schema",
		Long:  "Print a JSON description of the active gum command tree, including command paths, aliases, arguments, and flags.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The flag is accepted for parity with other CLIs that expose schema
			// output. JSON is the only supported format today.
			_ = jsonOut
			payload := commandSchema{
				SchemaVersion: 1,
				Command:       buildSchemaCommand(cmd.Root()),
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			return enc.Encode(payload)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit JSON output")
	return cmd
}

func buildSchemaCommand(cmd *cobra.Command) *schemaCommand {
	if cmd == nil || cmd.Hidden {
		return nil
	}
	node := &schemaCommand{
		Name:      cmd.Name(),
		Path:      cmd.CommandPath(),
		Usage:     usageSuffix(cmd),
		Help:      commandHelp(cmd),
		Aliases:   append([]string(nil), cmd.Aliases...),
		Arguments: parseUseArgs(cmd.Use),
	}
	node.Flags = collectSchemaFlags(cmd)
	for _, child := range cmd.Commands() {
		if child.Hidden {
			continue
		}
		childNode := buildSchemaCommand(child)
		if childNode != nil {
			node.Subcommands = append(node.Subcommands, *childNode)
		}
	}
	return node
}

func commandHelp(cmd *cobra.Command) string {
	if cmd.Long != "" {
		return strings.TrimSpace(cmd.Long)
	}
	return strings.TrimSpace(cmd.Short)
}

func usageSuffix(cmd *cobra.Command) string {
	use := strings.TrimSpace(cmd.UseLine())
	path := strings.TrimSpace(cmd.CommandPath())
	if path == "" {
		return use
	}
	return strings.TrimSpace(strings.TrimPrefix(use, path))
}

// parseUseArgs extracts the positional argument names from a cobra Use line
// such as `call <op-id> [args...]`. Field 0 is the command name, so the scan
// starts at 1; a command with an empty Use has no fields at all and must not
// slice past the end.
func parseUseArgs(use string) []schemaArg {
	fields := strings.Fields(use)
	args := []schemaArg{}
	if len(fields) < 2 {
		return args
	}
	for _, field := range fields[1:] {
		if !strings.ContainsAny(field, "<[") {
			continue
		}
		name := strings.Trim(field, "[]<>")
		// TrimSuffix, not TrimRight: the variadic marker is the literal "...",
		// and a cutset would also eat a trailing dot that belongs to the name.
		name = strings.TrimSuffix(name, "...")
		if name == "" || strings.HasPrefix(name, "-") {
			continue
		}
		args = append(args, schemaArg{Name: name})
	}
	return args
}

func collectSchemaFlags(cmd *cobra.Command) []schemaFlag {
	seen := map[string]bool{}
	flags := []schemaFlag{}
	addSet := func(set *pflag.FlagSet) {
		if set == nil {
			return
		}
		set.VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden || seen[flag.Name] {
				return
			}
			seen[flag.Name] = true
			item := schemaFlag{
				Name:       flag.Name,
				Short:      flag.Shorthand,
				Type:       flag.Value.Type(),
				Default:    flag.DefValue,
				HasDefault: flag.DefValue != "",
				Help:       flag.Usage,
			}
			flags = append(flags, item)
		})
	}
	addSet(cmd.NonInheritedFlags())
	addSet(cmd.InheritedFlags())
	return flags
}
