package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain wires the gum binary as a testscript "gum" command. Each
// testdata/script/*.txtar file can then invoke `gum ...` directly and assert
// stdin/stdout/stderr/exit-code without spawning a real subprocess.
//
// Spec Appendix A pins rogpeppe/go-internal as the contract-test framework;
// gum-5dh is the bead.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"gum": func() {
			if err := newRootCmd().Execute(); err != nil {
				// Mirror the real binary's main(): SilenceErrors=true on root,
				// so we explicitly write the final fatal line to stderr.
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
		},
	})
}

// TestCLIScripts walks testdata/script/*.txtar and executes each as a
// sandboxed CLI contract test.
//
// Sandboxing: HOME and XDG_CONFIG_HOME are redirected to the script's $WORK
// directory so no script can touch the developer's real ~/.config/gum or
// keyring. GOOGLE_APPLICATION_CREDENTIALS is cleared so ADC resolution stays
// deterministic (any script that needs ADC sets its own fake JSON via the
// testscript `cp` command and re-exports the env var).
func TestCLIScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(env *testscript.Env) error {
			env.Setenv("HOME", env.WorkDir)
			env.Setenv("XDG_CONFIG_HOME", env.WorkDir)
			env.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
			env.Setenv("GUM_PROFILE", "default")
			return nil
		},
	})
}
