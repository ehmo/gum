package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ehmo/gum/internal/output/profile"
	"github.com/spf13/cobra"
)

// newProfileCmd implements `gum profile validate|test`.
func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Validate or test an expression profile",
	}
	parentHelpOnly(cmd)
	cmd.AddCommand(newProfileValidateCmd(), newProfileTestCmd())
	return cmd
}

// newProfileValidateCmd parses an expression-profile file and reports any
// syntax or semantic errors. Exits 0 on success.
func newProfileValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate an expression-profile DSL file",
		Long: "Parse an expression-profile DSL file and report any errors. " +
			"Use this in CI to catch malformed catalog profiles before release.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read profile: %w", err)
			}
			if _, err := profile.Parse(string(src)); err != nil {
				return fmt.Errorf("%s: invalid profile: %w", args[0], err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ok")
			return nil
		},
	}
}

// newProfileTestCmd has two modes (spec §12.1):
//
//  1. Single-fixture mode (legacy): `gum profile test <profile> --input <file>
//     [--golden <golden>] [--format toon|json|raw]` applies the profile to a
//     single JSON input and prints (or compares against a golden) the shaped
//     output. `--format` here selects the *expression pipeline* output format.
//
//  2. Fixture-runner mode: `gum profile test <profile> --format=json` (no
//     --input) discovers `[[tests]]` entries in the profile file, runs each
//     through the expression pipeline, and emits a JSON envelope
//     `{passed, fixtures: ProfileFixtureResult[], token_budget}`. Exits non-zero
//     when any fixture fails or any token ceiling is violated.
//
// The two modes are disambiguated by presence of --input: with --input, the
// command is in single-fixture mode; without --input, it is in fixture-runner
// mode. `--format=json` (or other ProgramOutputFormat values) is interpreted
// per spec §12 (automation-safe JSON root) only in fixture-runner mode.
func newProfileTestCmd() *cobra.Command {
	var (
		inputPath  string
		goldenPath string
		userFormat string
	)
	cmd := &cobra.Command{
		Use:   "test <profile-path>",
		Short: "Run [[tests]] fixtures or apply a profile to a single --input file",
		Long: "When --input is set, applies the profile to that file (optionally " +
			"comparing against --golden). When --input is omitted, runs every " +
			"[[tests]] fixture in the profile file through the expression pipeline " +
			"and prints a ProfileFixtureResult[] JSON envelope (--format=json).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileSrc, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read profile: %w", err)
			}
			p, err := profile.Parse(string(profileSrc))
			if err != nil {
				return fmt.Errorf("%s: invalid profile: %w", args[0], err)
			}

			// Fixture-runner mode: triggered when --input is absent.
			if inputPath == "" {
				if len(p.Tests) == 0 {
					return fmt.Errorf("PROFILE_NO_FIXTURES: profile %q declares no [[tests]] entries; pass --input <path> to test a single fixture or add a [[tests]] block to the profile (docs/expression-profile-dsl.md Test Format)", args[0])
				}
				baseDir := filepath.Dir(args[0])
				res, err := profile.RunFixtures(p, baseDir)
				if err != nil {
					return fmt.Errorf("%s: run fixtures: %w", args[0], err)
				}
				switch userFormat {
				case "", "json":
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					if err := enc.Encode(res); err != nil {
						return fmt.Errorf("encode json: %w", err)
					}
				default:
					return fmt.Errorf("--format=%q is not supported in fixture-runner mode (use 'json')", userFormat)
				}
				if !res.Passed {
					return fmt.Errorf("PROFILE_FIXTURE_FAILED: %d/%d fixtures failed",
						countFailed(res.Fixtures), len(res.Fixtures))
				}
				return nil
			}

			// Legacy single-fixture mode.
			body, err := os.ReadFile(inputPath)
			if err != nil {
				return fmt.Errorf("read input: %w", err)
			}
			out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: userFormat})
			if err != nil {
				return fmt.Errorf("%s: apply profile: %w", args[0], err)
			}
			if goldenPath != "" {
				golden, err := os.ReadFile(goldenPath)
				if err != nil {
					return fmt.Errorf("read golden: %w", err)
				}
				if !bytes.Equal(out.Body, golden) {
					return fmt.Errorf("PROFILE_GOLDEN_MISMATCH:\n--- want\n%s\n--- got\n%s", golden, out.Body)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ok")
				return nil
			}
			if _, err := cmd.OutOrStdout().Write(out.Body); err != nil {
				return err
			}
			if len(out.Body) == 0 || out.Body[len(out.Body)-1] != '\n' {
				_, _ = fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&inputPath, "input", "", "Path to input JSON (single-fixture mode)")
	cmd.Flags().StringVar(&goldenPath, "golden", "", "Path to golden output; if set, compare byte-for-byte")
	cmd.Flags().StringVar(&userFormat, "format", "", "Output format: in fixture-runner mode 'json' (default); in single-fixture mode overrides profile default_format (toon|json|raw)")
	return cmd
}

// countFailed returns the number of fixtures in xs whose Passed is false.
func countFailed(xs []profile.ProfileFixtureResult) int {
	n := 0
	for _, x := range xs {
		if !x.Passed {
			n++
		}
	}
	return n
}
