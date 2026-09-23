package main

// profile_shadowing.go — §9.2 shadowing warning, presentation half.
//
// The detector lives in internal/output/profile; this file is the two firing
// points the spec names: `gum profile validate` and the runtime profile loader.
// Both write to stderr and neither blocks, so a weakened override still runs.

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
	"github.com/spf13/cobra"
)

// shadowWarnFlag suppresses the warning; shadowWarnLegacyFlag is the alias
// §9.2 keeps for one release cycle before removal.
const (
	shadowWarnFlag       = "no-warn-lossy"
	shadowWarnLegacyFlag = "no-warn-recovery"
)

// shadowState holds the process-wide half of the warning: the resolved
// suppression flag, the writer, and the set of lines already printed.
//
// The runtime loader runs per invocation and the same override is resolved on
// every call, so without the seen set a long-lived `gum mcp --stdio` session
// would repeat one warning until the transport closed.
var shadowState = struct {
	mu         sync.Mutex
	suppressed bool
	out        io.Writer
	seen       map[string]bool
}{out: os.Stderr}

// registerShadowWarnFlags adds the suppression flag and its legacy alias to the
// root command. Both are persistent: the warning fires from `gum profile
// validate` and from every command that dispatches through the profile loader.
func registerShadowWarnFlags(root *cobra.Command) {
	root.PersistentFlags().Bool(shadowWarnFlag, false,
		"Suppress the "+profile.WarnOverrideDisablesLossyStage+" warning when a profile override drops a lossy-compression stage")
	root.PersistentFlags().Bool(shadowWarnLegacyFlag, false,
		"Deprecated alias of --"+shadowWarnFlag)
	_ = root.PersistentFlags().MarkDeprecated(shadowWarnLegacyFlag, "use --"+shadowWarnFlag)
}

// applyShadowWarnFlags publishes the resolved suppression state for the runtime
// loader, which has no command to read a flag from. It also clears the seen set
// so one process running several root commands, which is what the test binary
// does, does not inherit another run's printed lines.
func applyShadowWarnFlags(cmd *cobra.Command) {
	suppressed := boolFlag(cmd, shadowWarnFlag) || boolFlag(cmd, shadowWarnLegacyFlag)

	shadowState.mu.Lock()
	defer shadowState.mu.Unlock()
	shadowState.suppressed = suppressed
	shadowState.seen = nil
}

// shadowWarningsSuppressed reports whether --no-warn-lossy (or its alias) was
// set on this invocation. `gum mcp` reads it here to inject the same decision
// into the server, which logs the warning instead of writing to stderr.
func shadowWarningsSuppressed() bool {
	shadowState.mu.Lock()
	defer shadowState.mu.Unlock()
	return shadowState.suppressed
}

// boolFlag reads a persistent bool flag from cmd's root, returning false when
// the flag is absent. A test that builds a bare command gets the default.
func boolFlag(cmd *cobra.Command, name string) bool {
	if cmd == nil || cmd.Root() == nil {
		return false
	}
	v, err := cmd.Root().PersistentFlags().GetBool(name)
	if err != nil {
		return false
	}
	return v
}

// emitShadowWarnings prints each warning once to w, or drops them all when the
// operator passed the suppression flag. Passing w lets `gum profile validate`
// route the warning to the command's own stderr; the runtime loader passes nil
// and takes the process stderr.
func emitShadowWarnings(w io.Writer, warnings []profile.ShadowWarning) {
	if len(warnings) == 0 {
		return
	}

	shadowState.mu.Lock()
	defer shadowState.mu.Unlock()

	if shadowState.suppressed {
		return
	}
	if w == nil {
		w = shadowState.out
	}
	if shadowState.seen == nil {
		shadowState.seen = map[string]bool{}
	}

	for _, warning := range warnings {
		line := warning.String()
		if shadowState.seen[line] {
			continue
		}
		shadowState.seen[line] = true
		_, _ = io.WriteString(w, profile.WarnOverrideDisablesLossyStage+" "+line+"\n")
	}
}

// fileShadowWarnings is the `gum profile validate` half: it reports every
// loss-driving field the file takes away from a catalog-embedded profile.
//
// Two shapes of override reach the same place. A definition that reuses a
// catalog-embedded profile's name shadows it by resolution order, and an
// [override_bindings] entry attaches a different profile to an op. The binding
// form is reported under the op it binds, which is what the warning text names,
// so a name that a binding also mentions is not reported twice.
func fileShadowWarnings(path string, f *profile.File) []profile.ShadowWarning {
	if f == nil {
		return nil
	}

	root, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		root = ""
	}
	cat := loadCatalog()

	var out []profile.ShadowWarning
	bound := map[string]bool{}

	for _, target := range sortedBindingTargets(f.OverrideBindings) {
		name := f.OverrideBindings[target]
		bound[name] = true

		catalogProfile, ok := catalogProfileForTarget(cat, target)
		if !ok {
			continue
		}
		override := resolveBoundProfile(root, f, name)
		out = append(out, profile.DetectShadowing(target, catalogProfile, override)...)
	}

	for _, p := range f.Profiles {
		if bound[p.Name] {
			continue
		}
		catalogProfile, ok := profile.BuiltinLookup(p.Name)
		if !ok {
			continue
		}
		out = append(out, profile.DetectShadowing(p.Name, catalogProfile, p)...)
	}

	return out
}

// resolveBoundProfile finds the profile a binding names: a sibling definition in
// the same file first, then the three-level hierarchy rooted at the file's own
// directory. Returns nil when nothing resolves, which DetectShadowing treats as
// nothing to compare — ValidateOverrideBindings has already rejected that case.
func resolveBoundProfile(root string, f *profile.File, name string) *profile.Profile {
	if p := f.Lookup(name); p != nil {
		return p
	}
	p, _, err := profile.ResolveProfile(root, name, profile.BuiltinLookup)
	if err != nil {
		return nil
	}
	return p
}

// catalogProfileForTarget returns the catalog-embedded profile an override
// binding displaces. target is an op_id or a variant_id: an op_id resolves
// through its default variant, which is the variant a call without an explicit
// variant_id routes to.
func catalogProfileForTarget(c *catalog.Catalog, target string) (*profile.Profile, bool) {
	if c == nil {
		return nil, false
	}

	name := ""
	for i := range c.Ops {
		op := &c.Ops[i]
		if op.OpID == target {
			for j := range op.Variants {
				if op.Variants[j].VariantID == op.DefaultVariantID {
					name = op.Variants[j].OutputProfile
				}
			}
			break
		}
		for j := range op.Variants {
			if op.Variants[j].VariantID == target {
				name = op.Variants[j].OutputProfile
			}
		}
		if name != "" {
			break
		}
	}
	if name == "" {
		return nil, false
	}

	return profile.BuiltinLookup(name)
}

// sortedBindingTargets returns m's keys in name order so a file with several bindings
// warns in the same order on every run.
func sortedBindingTargets(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
