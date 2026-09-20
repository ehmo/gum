package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestBuildSchemaCommandSkipsNilAndHidden pins the two guards that keep a
// hidden command out of `gum schema`. A hidden command is not part of the
// published surface, so emitting it would advertise an unsupported entry
// point to every schema consumer.
func TestBuildSchemaCommandSkipsNilAndHidden(t *testing.T) {
	if got := buildSchemaCommand(nil); got != nil {
		t.Errorf("buildSchemaCommand(nil) = %+v; want nil", got)
	}
	if got := buildSchemaCommand(&cobra.Command{Use: "secret", Hidden: true}); got != nil {
		t.Errorf("buildSchemaCommand(hidden) = %+v; want nil", got)
	}
}

// TestBuildSchemaCommandDropsAHiddenChild pins the child-side skip. The
// parent is visible, so the walk continues; only the hidden subcommand has
// to disappear from Subcommands.
func TestBuildSchemaCommandDropsAHiddenChild(t *testing.T) {
	parent := &cobra.Command{Use: "parent"}
	parent.AddCommand(&cobra.Command{Use: "visible"})
	parent.AddCommand(&cobra.Command{Use: "concealed", Hidden: true})

	node := buildSchemaCommand(parent)
	if node == nil {
		t.Fatal("buildSchemaCommand(parent) = nil")
	}
	for _, sub := range node.Subcommands {
		if sub.Name == "concealed" {
			t.Fatalf("hidden subcommand leaked into the schema: %+v", sub)
		}
	}
	if len(node.Subcommands) != 1 {
		t.Errorf("Subcommands = %d entries; want only the visible one", len(node.Subcommands))
	}
}

// TestUsageSuffixKeepsTheWholeLineWithoutAPath pins the empty-path arm. A
// command with no Use has no CommandPath to trim, so the usage line has to
// survive whole instead of being trimmed against "".
func TestUsageSuffixKeepsTheWholeLineWithoutAPath(t *testing.T) {
	cmd := &cobra.Command{}
	if got, want := usageSuffix(cmd), cmd.UseLine(); got != want {
		t.Errorf("usageSuffix = %q; want the untrimmed use line %q", got, want)
	}
}

// TestCollectSchemaFlagsSkipsHiddenFlags pins the per-flag skip. A hidden
// flag is unsupported surface for the same reason a hidden command is.
func TestCollectSchemaFlagsSkipsHiddenFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}
	cmd.Flags().String("shown", "", "visible")
	cmd.Flags().String("concealed", "", "hidden")
	if err := cmd.Flags().MarkHidden("concealed"); err != nil {
		t.Fatalf("mark hidden: %v", err)
	}

	var names []string
	for _, f := range collectSchemaFlags(cmd) {
		names = append(names, f.Name)
	}
	for _, n := range names {
		if n == "concealed" {
			t.Fatalf("hidden flag leaked into the schema: %v", names)
		}
	}
	var sawShown bool
	for _, n := range names {
		if n == "shown" {
			sawShown = true
		}
	}
	if !sawShown {
		t.Errorf("flags = %v; want the visible flag present", names)
	}
}
