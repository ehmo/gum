package main

import "github.com/spf13/cobra"

func parentHelpOnly(cmd *cobra.Command) {
	cmd.Args = cobra.NoArgs
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	}
}
