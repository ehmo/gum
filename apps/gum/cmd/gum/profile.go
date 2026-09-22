package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
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

// newProfileValidateCmd runs all three profile validators over a DSL file
// (docs/expression-profile-dsl.md:13). Exits 0 on success.
//
// Two of them read the file alone: profile.Parse is the structural validator
// and profile.ValidateSemantics is the cross-field one. The third,
// ValidateStripNullsSafety, reads the profile's keep_fields against the bound
// variant's null_elision_safe_fields, which no standalone file carries. That is
// what --variant supplies. Without it the strip_nulls check is skipped and the
// command says so, because an unbound run would have to treat every
// strip_nulls profile as unsafe.
func newProfileValidateCmd() *cobra.Command {
	var variantID string
	cmd := &cobra.Command{
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
			f, err := profile.ParseFile(string(src))
			if err != nil {
				return fmt.Errorf("%s: invalid profile: %w", args[0], err)
			}
			var v *catalog.Variant
			if variantID != "" {
				v = findCatalogVariant(loadCatalog(), variantID)
				if v == nil {
					return fmt.Errorf("VARIANT_NOT_FOUND: %q is not in the embedded catalog", variantID)
				}
			}

			// A bound run goes through profile.ValidateForVariant, the same
			// entry point cmd/gen-catalog uses, so the two build-time paths
			// cannot disagree about which checks a bound profile gets.
			for _, p := range f.Profiles {
				var verr error
				if v != nil {
					verr = profile.ValidateForVariant(p, v.NullElisionSafeFields)
				} else {
					verr = profile.ValidateSemantics(p)
				}
				if verr != nil {
					return fmt.Errorf("%s: invalid profile: %w", args[0], verr)
				}
			}

			out := cmd.OutOrStdout()
			if err := validateFileBindings(out, args[0], f); err != nil {
				return err
			}

			if v == nil {
				for _, p := range f.Profiles {
					if p.StripNulls {
						_, _ = fmt.Fprintln(out, "note: strip_nulls safety not checked; re-run with --variant <variant_id> to check keep_fields against that variant's null_elision_safe_fields")
					}
				}
			}

			_, _ = fmt.Fprintln(out, "ok")
			return nil
		},
	}
	cmd.Flags().StringVar(&variantID, "variant", "", "Catalog variant to bind for the strip_nulls safety check")
	return cmd
}

// findCatalogVariant returns the variant with the given id, or nil when the
// catalog is unavailable or carries no such variant.
func findCatalogVariant(c *catalog.Catalog, variantID string) *catalog.Variant {
	if c == nil {
		return nil
	}
	for i := range c.Ops {
		op := &c.Ops[i]
		for j := range op.Variants {
			if op.Variants[j].VariantID == variantID {
				return &op.Variants[j]
			}
		}
	}
	return nil
}

// validateFileBindings runs the OVERRIDE_BINDING_INVALID check over a file's
// [override_bindings] table (spec §9.2).
//
// A binding value is satisfied by a sibling definition in the same file, by the
// three-level hierarchy rooted at the file's own directory, or by a builtin. The
// key is checked against the embedded catalog; when the binary ships no catalog
// there is nothing to check the key against, so the check is skipped and the
// command says so rather than passing a dangling key silently.
func validateFileBindings(out io.Writer, path string, f *profile.File) error {
	if len(f.OverrideBindings) == 0 {
		return nil
	}
	targets := catalogTargetIDs(loadCatalog())
	var targetKnown func(string) bool
	if len(targets) > 0 {
		targetKnown = func(id string) bool {
			_, ok := targets[id]
			return ok
		}
	}
	root, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		root = ""
	}
	profileKnown := func(name string) bool {
		if f.Lookup(name) != nil {
			return true
		}
		_, _, rerr := profile.ResolveProfile(root, name, profile.BuiltinLookup)
		return rerr == nil
	}
	if err := profile.ValidateOverrideBindings(f, targetKnown, profileKnown); err != nil {
		return fmt.Errorf("%s: invalid profile: %w", path, err)
	}
	if targetKnown == nil {
		_, _ = fmt.Fprintln(out, "note: override_bindings keys not checked; this binary embeds no catalog to check op_id and variant_id against")
	}
	return nil
}

// catalogTargetIDs collects every id an [override_bindings] key may name: an
// op_id or any of that op's variant_ids.
func catalogTargetIDs(c *catalog.Catalog) map[string]struct{} {
	if c == nil {
		return nil
	}
	ids := make(map[string]struct{}, len(c.Ops)*2)
	for i := range c.Ops {
		op := &c.Ops[i]
		ids[op.OpID] = struct{}{}
		for j := range op.Variants {
			ids[op.Variants[j].VariantID] = struct{}{}
		}
	}
	return ids
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
		inputPath   string
		goldenPath  string
		userFormat  string
		profileName string
		opID        string
		variantID   string
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
			f, err := profile.ParseFile(string(profileSrc))
			if err != nil {
				return fmt.Errorf("%s: invalid profile: %w", args[0], err)
			}
			for _, def := range f.Profiles {
				if err := profile.ValidateSemantics(def); err != nil {
					return fmt.Errorf("%s: invalid profile: %w", args[0], err)
				}
			}
			p, err := selectProfile(f, profileName, args[0])
			if err != nil {
				return err
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
			out, err := profile.Apply(p, profile.ApplyInput{
				Body:       body,
				UserFormat: userFormat,
				Op:         opID,
				Variant:    variantID,
			})
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
	// Named --name, not --profile: the root command already owns a persistent
	// --profile that selects the config profile, and shadowing it here would
	// silently change what `gum profile test ... --profile x` means.
	cmd.Flags().StringVar(&profileName, "name", "", "Definition to test when the file declares several [output_profiles.\"<name>\"] tables")
	cmd.Flags().StringVar(&inputPath, "input", "", "Path to input JSON (single-fixture mode)")
	cmd.Flags().StringVar(&goldenPath, "golden", "", "Path to golden output; if set, compare byte-for-byte")
	cmd.Flags().StringVar(&userFormat, "format", "", "Output format: in fixture-runner mode 'json' (default); in single-fixture mode overrides the profile format (toon|csv|json|markdown|raw)")
	// A TOON body is a §9.0 document whose header names the op and variant that
	// produced it. The command has no dispatch behind it, so a golden that
	// carries real ids needs them supplied here.
	cmd.Flags().StringVar(&opID, "op", "", "op_id written to the TOON op: header (single-fixture mode)")
	cmd.Flags().StringVar(&variantID, "variant", "", "variant_id written to the TOON variant: header (single-fixture mode)")
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

// selectProfile picks the definition `gum profile test` runs. A file with one
// definition needs no --profile; a file with several has no defensible default,
// so it demands the flag rather than testing an arbitrary one.
func selectProfile(f *profile.File, name, path string) (*profile.Profile, error) {
	if name != "" {
		p := f.Lookup(name)
		if p == nil {
			return nil, fmt.Errorf("PROFILE_NOT_FOUND: %s defines no profile %q (defined: %s)", path, name, strings.Join(profileNames(f), ", "))
		}
		return p, nil
	}
	if len(f.Profiles) == 1 {
		return f.Profiles[0], nil
	}
	return nil, fmt.Errorf("PROFILE_AMBIGUOUS: %s defines %d profiles (%s); pass --name <profile>", path, len(f.Profiles), strings.Join(profileNames(f), ", "))
}

// profileNames lists a file's definitions for error text.
func profileNames(f *profile.File) []string {
	out := make([]string, 0, len(f.Profiles))
	for _, p := range f.Profiles {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}
