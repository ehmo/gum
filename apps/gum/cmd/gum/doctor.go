package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/config"
	"github.com/spf13/cobra"
)

// newDoctorCmd implements `gum doctor` (gum-me29 acceptance g): a single
// command that aggregates auth status, audit availability, cache health, plugin verify, and
// config validate, then exits non-zero if any subsystem is broken so it is
// scriptable in CI pre-flight checks.
//
// Each check is intentionally non-fatal individually — the command always
// runs every check and reports them in one JSON envelope so an operator sees
// every problem at once instead of having to fix-then-rerun in a loop.
func newDoctorCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose gum subsystems (auth, audit, cache, plugins, config)",
		Long: `Runs a single pre-flight that reports the health of each gum subsystem:

  - auth:    checks BYO OAuth, API key, service account, and ADC readiness
  - audit:   reports audit.broken and confirms the per-profile data dir is writable
  - cache:   confirms the per-profile cache dir exists and is writable
  - plugin:  lists installed plugins (a fast read-only walk of the plugin root)
  - config:  loads and validates the active profile's config.toml

Exits non-zero if any subsystem is broken. Use --format=json for machine output.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, err := resolveProfileName(cmd)
			if err != nil {
				return err
			}
			profile := name.String()
			rep := runDoctorChecks(cmd, profile)

			if format == "json" {
				// Write the JSON envelope to stdout regardless, then mirror the
				// text path's exit contract: a failing subsystem must exit non-zero
				// so `gum doctor --format=json` is usable as a CI pre-flight gate.
				if err := writeJSON(cmd.OutOrStdout(), rep); err != nil {
					return err
				}
				if !rep.OK {
					return errDoctorUnhealthy
				}
				return nil
			}
			renderDoctorText(cmd.OutOrStdout(), rep)
			if !rep.OK {
				return errDoctorUnhealthy
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text|json")
	return cmd
}

// errDoctorUnhealthy is returned by `gum doctor` when any subsystem check
// fails so cobra surfaces a non-zero exit status without a usage banner.
var errDoctorUnhealthy = errors.New("doctor: one or more subsystems unhealthy")

var doctorKeyringTimeout = 2 * time.Second

var doctorKeyringFactory = func() auth.KeyringBackend {
	return auth.NewOSKeyring()
}

// doctorReport is the aggregate envelope `gum doctor --format=json` writes.
type doctorReport struct {
	OK      bool                `json:"ok"`
	Profile string              `json:"profile"`
	Checks  []doctorCheckResult `json:"checks"`
}

type doctorCheckResult struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Summary string `json:"summary"`
	Hint    string `json:"hint,omitempty"`
}

// runDoctorChecks invokes each subsystem probe and assembles the report.
// Probes are cheap and read-only; nothing here issues network requests.
func runDoctorChecks(cmd *cobra.Command, profile string) doctorReport {
	rep := doctorReport{Profile: profile}
	rep.Checks = append(rep.Checks,
		doctorAuth(cmd),
		doctorAudit(profile),
		doctorCache(profile),
		doctorPlugin(),
		doctorConfig(profile),
	)
	rep.OK = true
	for _, c := range rep.Checks {
		if !c.OK {
			rep.OK = false
			break
		}
	}
	return rep
}

// doctorAudit reports whether the per-profile audit sink is usable. The audit
// writer leaves audit.broken when filesystem errors occur during append or
// rotation; this check makes that fail-closed signal visible to CLI and CI
// users instead of only MCP cache_stats consumers.
func doctorAudit(profile string) doctorCheckResult {
	name, err := resolveProfileString(profile)
	if err != nil {
		return doctorCheckResult{
			Name:    "audit",
			OK:      false,
			Summary: "invalid profile",
			Hint:    err.Error(),
		}
	}
	dir, err := name.DataDir()
	if err != nil {
		return doctorCheckResult{
			Name:    "audit",
			OK:      false,
			Summary: "cannot resolve audit dir",
			Hint:    err.Error(),
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return doctorCheckResult{
			Name:    "audit",
			OK:      false,
			Summary: "cannot create audit dir",
			Hint:    err.Error(),
		}
	}
	sentinel := filepath.Join(dir, "audit.broken")
	if _, err := os.Stat(sentinel); err == nil {
		return doctorCheckResult{
			Name:    "audit",
			OK:      false,
			Summary: "audit sink previously failed",
			Hint:    readAuditBrokenHint(sentinel),
		}
	} else if !os.IsNotExist(err) {
		return doctorCheckResult{
			Name:    "audit",
			OK:      false,
			Summary: "cannot inspect audit.broken",
			Hint:    err.Error(),
		}
	}
	probe := filepath.Join(dir, ".audit-doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return doctorCheckResult{
			Name:    "audit",
			OK:      false,
			Summary: "audit dir not writable",
			Hint:    err.Error(),
		}
	}
	_ = os.Remove(probe)
	return doctorCheckResult{
		Name:    "audit",
		OK:      true,
		Summary: "audit sink writable: " + dir,
	}
}

func readAuditBrokenHint(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return err.Error()
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return err.Error()
	}
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		return path
	}
	return msg
}

// doctorAuth reports local credential readiness without making a network call.
// BYO OAuth is the public v1 default, but ADC/API-key/service-account setups are
// also valid for variants that declare those auth strategies.
func doctorAuth(cmd *cobra.Command) doctorCheckResult {
	profile := resolveProfileFlag(cmd)
	keyringResult, keyringDone := doctorAuthFromKeyring(cmd.Context(), doctorKeyringFactory(), profile)
	if keyringDone {
		return keyringResult
	}

	if strings.TrimSpace(os.Getenv(auth.EnvAPIKeyVar)) != "" {
		return doctorCheckResult{
			Name:    "auth",
			OK:      true,
			Summary: auth.EnvAPIKeyVar + " is set",
		}
	}
	if strings.TrimSpace(os.Getenv(auth.EnvServiceAccountKeyVar)) != "" {
		return doctorCheckResult{
			Name:    "auth",
			OK:      true,
			Summary: auth.EnvServiceAccountKeyVar + " is set",
		}
	}

	st := collectADCStatus(cmd.Context(), []string{"gmail.readonly"})
	if st.Source != "" {
		return doctorCheckResult{
			Name:    "auth",
			OK:      true,
			Summary: "ADC source: " + st.Source,
		}
	}

	hint := "run `gum auth use-oauth-client --client-id <id> --secret-stdin`, then `gum login --service gmail,calendar`"
	if keyringResult.Hint != "" {
		hint = keyringResult.Hint + "; " + hint
	}
	return doctorCheckResult{
		Name:    "auth",
		OK:      false,
		Summary: "no local credential source detected",
		Hint:    hint,
	}
}

func doctorAuthFromKeyring(ctx context.Context, kb auth.KeyringBackend, profile string) (doctorCheckResult, bool) {
	if kb == nil {
		return doctorCheckResult{Name: "auth"}, false
	}
	type result struct {
		check doctorCheckResult
		done  bool
	}
	ctx, cancel := context.WithTimeout(ctx, doctorKeyringTimeout)
	defer cancel()
	ch := make(chan result, 1)
	go func() {
		if _, ok, err := auth.LoadByoClient(kb, profile); err == nil && ok {
			if scopes := auth.GrantedScopes(kb, profile); len(scopes) > 0 {
				ch <- result{
					done: true,
					check: doctorCheckResult{
						Name:    "auth",
						OK:      true,
						Summary: fmt.Sprintf("BYO OAuth ready for profile %q (%d granted scope(s))", profile, len(scopes)),
					},
				}
				return
			}
			ch <- result{
				done: true,
				check: doctorCheckResult{
					Name:    "auth",
					OK:      false,
					Summary: fmt.Sprintf("BYO OAuth client registered for profile %q but no grant is stored", profile),
					Hint:    "run `gum login --service gmail,calendar` or `gum login --scope <scope-url>`",
				},
			}
			return
		}
		if key, err := auth.LookupAPIKey(kb, profile); err == nil && strings.TrimSpace(key) != "" {
			ch <- result{
				done: true,
				check: doctorCheckResult{
					Name:    "auth",
					OK:      true,
					Summary: fmt.Sprintf("API key present in keychain for profile %q", profile),
				},
			}
			return
		}
		ch <- result{check: doctorCheckResult{Name: "auth"}}
	}()

	select {
	case res := <-ch:
		return res.check, res.done
	case <-ctx.Done():
		return doctorCheckResult{
			Name:    "auth",
			OK:      false,
			Summary: "OS keychain lookup timed out",
			Hint:    "OS keychain lookup timed out; set GUM_API_KEY or GUM_SERVICE_ACCOUNT_KEY for non-interactive environments",
		}, false
	}
}

// doctorCache verifies the per-profile cache directory is reachable and
// writable. We do not open the bbolt files — that would race against a
// long-running gum process holding the lock — only confirm we could.
func doctorCache(profile string) doctorCheckResult {
	name, err := resolveProfileString(profile)
	if err != nil {
		return doctorCheckResult{
			Name:    "cache",
			OK:      false,
			Summary: "invalid profile",
			Hint:    err.Error(),
		}
	}
	dir, err := name.CacheDir()
	if err != nil {
		return doctorCheckResult{
			Name:    "cache",
			OK:      false,
			Summary: "cannot resolve cache dir",
			Hint:    err.Error(),
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return doctorCheckResult{
			Name:    "cache",
			OK:      false,
			Summary: "cannot create cache dir",
			Hint:    err.Error(),
		}
	}
	probe := filepath.Join(dir, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return doctorCheckResult{
			Name:    "cache",
			OK:      false,
			Summary: "cache dir not writable",
			Hint:    err.Error(),
		}
	}
	_ = os.Remove(probe)
	return doctorCheckResult{
		Name:    "cache",
		OK:      true,
		Summary: "writable: " + dir,
	}
}

// doctorPlugin asks the plugin host for the installed list. The list path
// is fully read-only; a broken plugin root surfaces as a non-OK check
// instead of crashing the whole doctor run.
func doctorPlugin() doctorCheckResult {
	if _, err := DispatchPluginCommand([]string{"list"}, defaultPluginsHost()); err != nil {
		return doctorCheckResult{
			Name:    "plugin",
			OK:      false,
			Summary: "plugin host failed",
			Hint:    err.Error(),
		}
	}
	return doctorCheckResult{
		Name:    "plugin",
		OK:      true,
		Summary: "plugin host reachable",
	}
}

// doctorConfig loads the active profile's config.toml. Missing config is OK
// (defaults apply); a parse error fails the check with the parser's message
// so an operator can fix the offending line.
func doctorConfig(profile string) doctorCheckResult {
	// config.Load returns (*Config, []Warning, error) — the second value is the
	// warnings, NOT the path. Get the path separately so the summary shows the
	// file location instead of the Go representation of the warnings slice.
	c, warnings, err := config.Load(profile)
	if err != nil {
		return doctorCheckResult{
			Name:    "config",
			OK:      false,
			Summary: "config load failed",
			Hint:    err.Error(),
		}
	}
	path, _ := config.Path(profile)
	res := doctorCheckResult{
		Name:    "config",
		OK:      true,
		Summary: fmt.Sprintf("loaded %d keys from %s", len(c.Keys()), path),
	}
	if len(warnings) > 0 {
		res.Hint = fmt.Sprintf("%d config warning(s); run `gum config` to review", len(warnings))
	}
	return res
}

// renderDoctorText prints the human-readable summary for `gum doctor`
// without --format=json. One line per check; a trailing OK/FAIL banner.
func renderDoctorText(w fileWriter, rep doctorReport) {
	for _, c := range rep.Checks {
		mark := "ok"
		if !c.OK {
			mark = "FAIL"
		}
		_, _ = fmt.Fprintf(w, "[%s] %s: %s\n", mark, c.Name, c.Summary)
		if c.Hint != "" {
			_, _ = fmt.Fprintf(w, "       hint: %s\n", c.Hint)
		}
	}
	if rep.OK {
		_, _ = fmt.Fprintln(w, "doctor: all checks passed")
	} else {
		_, _ = fmt.Fprintln(w, "doctor: one or more checks FAILED")
	}
}

// fileWriter is the subset of io.Writer used by renderDoctorText so tests can
// inject a bytes.Buffer without importing the full io package surface here.
type fileWriter interface {
	Write(p []byte) (int, error)
}
