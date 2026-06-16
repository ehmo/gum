package main

import (
	"fmt"
	"strings"

	"github.com/ehmo/gum/internal/config"
	profilepkg "github.com/ehmo/gum/internal/profile"
	"github.com/spf13/cobra"
)

// newConfigCmd implements `gum config get|set` (spec §12.2).
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Get or set per-profile configuration values",
		Long:  "Read or write values in the active profile's config.toml. The active profile is selected via --profile (default: 'default').",
	}
	parentHelpOnly(cmd)
	cmd.AddCommand(newConfigGetCmd(), newConfigSetCmd(), newConfigListCmd(), newConfigUnsetCmd())
	return cmd
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all config keys in the active profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			profile := resolveProfileFlag(cmd)
			c, _, err := config.Load(profile)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			keys := c.Keys()
			if len(keys) == 0 {
				// Keep stdout empty for pipes; note on stderr only for an
				// interactive human so piped/scripted output stays clean
				// (gum-s985).
				if isTerminal(cmd.ErrOrStderr()) {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "No config keys set in profile %q.\n", profile)
				}
				return nil
			}
			for _, k := range keys {
				v, _ := c.Get(k)
				_, _ = fmt.Fprintf(out, "%s=%s\n", k, v)
			}
			return nil
		},
	}
}

func newConfigUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove a config key from the active profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := resolveProfileFlag(cmd)
			c, _, err := config.Load(profile)
			if err != nil {
				return err
			}
			if !c.Unset(args[0]) {
				return fmt.Errorf("config: key %q not found in profile %q", args[0], profile)
			}
			return config.Save(profile, c)
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print the value of a config key from the active profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := resolveProfileFlag(cmd)
			c, _, err := config.Load(profile)
			if err != nil {
				return err
			}
			v, ok := c.Get(args[0])
			if !ok {
				return fmt.Errorf("config: key %q not found in profile %q", args[0], profile)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), v)
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key>=<value>",
		Short: "Persist a config key=value pair to the active profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kv := args[0]
			idx := strings.IndexByte(kv, '=')
			if idx < 0 {
				return fmt.Errorf("config: expected key=value, got %q", kv)
			}
			key := strings.TrimSpace(kv[:idx])
			value := strings.TrimSpace(kv[idx+1:])
			if key == "" || value == "" {
				return fmt.Errorf("config: empty key or value in %q", kv)
			}
			profile := resolveProfileFlag(cmd)
			c, _, err := config.Load(profile)
			if err != nil {
				return err
			}
			c.Set(key, value)
			return config.Save(profile, c)
		},
	}
}

// resolveProfileFlag returns the --profile persistent flag value from the root
// command, defaulting to "default".
func resolveProfileFlag(cmd *cobra.Command) string {
	name, err := resolveProfileName(cmd)
	if err != nil {
		return profilepkg.DefaultName.String()
	}
	return name.String()
}

func resolveProfileName(cmd *cobra.Command) (profilepkg.Name, error) {
	if cmd == nil || cmd.Root() == nil {
		return profilepkg.DefaultName, nil
	}
	if f := cmd.Root().PersistentFlags().Lookup("profile"); f != nil {
		return profilepkg.Resolve(f.Value.String(), f.Changed)
	}
	return profilepkg.DefaultName, nil
}

func resolveProfileString(raw string) (profilepkg.Name, error) {
	return profilepkg.Parse(raw)
}
