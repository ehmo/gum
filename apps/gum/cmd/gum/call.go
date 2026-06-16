package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/cli/callargs"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/spf13/cobra"
)

// newCallCmd implements `gum call <op_id> --risk=... [flags] [args...]`
// per spec §12.0. The risk gate, variant-id pin, and positional arg grammar
// all run before any upstream request.
func newCallCmd() *cobra.Command {
	var (
		riskRaw     string
		variantID   string
		fields      string
		pageSize    int
		pageToken   string
		formatJSON  bool
		formatTOON  bool
		formatCSV   bool
		formatMD    bool
		output      string
		skeleton    bool
		yes         bool
		confirmed   bool
		token       string
		raw         bool
		noFieldMask bool
	)
	cmd := &cobra.Command{
		Use:   "call <op_id> --risk=<read|write|destructive> [args...]",
		Short: "Invoke a catalog operation through the dispatch kernel",
		Long: "gum call is the deterministic CLI entry point for catalog operations.\n" +
			"Positional args follow the §12.0 grammar: key=value, key:=json, @file. The risk\n" +
			"flag MUST match the resolved variant's risk_class or the call returns\n" +
			"RISK_TOOL_MISMATCH before any upstream request.",
		Example: "  # Read with positional args\n" +
			"  gum call gmail.users.messages.list --risk=read userId=me maxResults:=5\n" +
			"  # Print a fill-in request skeleton (no upstream call, no --risk needed)\n" +
			"  gum call gmail.users.messages.list --skeleton",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MinimumNArgs(1)(cmd, args); err != nil {
				return err
			}
			// --skeleton is introspection (no upstream call), so it doesn't need
			// --risk. Clear the required annotation before cobra validates it
			// (ValidateArgs runs before ValidateRequiredFlags).
			if skeleton {
				if f := cmd.Flags().Lookup("risk"); f != nil {
					delete(f.Annotations, cobra.BashCompOneRequiredFlag)
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			opID := args[0]
			positional := args[1:]

			// --skeleton is introspection: print the op's fillable request
			// template and exit, without requiring --risk or any upstream call.
			if skeleton {
				if !catalogHasOp(opID) {
					return cliArgInvalid(fmt.Sprintf("unknown op_id %q (run `gum search <keyword>` to find operations)", opID))
				}
				return renderSkeleton(cmd.OutOrStdout(), opID, lookupRequestFields(opID))
			}

			// §12.0 risk path: required, closed enum.
			risk, ok := normalizeRisk(riskRaw)
			if !ok {
				return cliArgInvalid(fmt.Sprintf("--risk=%s is required: choose read|write|destructive", riskRaw))
			}

			// Resolve output format: an explicit --output/-o or a format
			// boolean wins; otherwise default to a human table on a terminal
			// and machine JSON when piped (so scripts/agents keep stable JSON).
			format, ferr := resolveCallFormat(cmd.OutOrStdout(), output, formatJSON, formatTOON, formatCSV, formatMD)
			if ferr != nil {
				return ferr
			}
			// table/csv/markdown are rendered in the CLI from the structured
			// result; the dispatch kernel only needs the parsed JSON for them.
			cliRender := cliFormatNeedsStructured(format)
			dispatchFormat := format
			if cliRender {
				dispatchFormat = "json"
			}

			// Schema-aware input convenience (additive): when the op declares
			// RequestFields, repeated array keys parse to slices and body-located
			// fields are assembled into the "body" arg — so a human can pass flat
			// fields instead of body:=json. Ops without RequestFields are
			// unaffected and the §12.0 grammar is unchanged.
			reqFields := lookupRequestFields(opID)
			parsed, err := callargs.ParseArgs(positional, callargs.Options{
				Stdin:       cmd.InOrStdin(),
				ArrayFields: arrayRequestFields(reqFields),
			})
			if err != nil {
				return err
			}
			// Merge any typed --kebab flags (registered for this op before parse)
			// into the args under their canonical field names, so they're seen by
			// the wizard (no re-prompt) and validated like positional fields.
			applyKebabFlags(cmd, parsed.Args, reqFields)
			// Interactive wizard: on a TTY, prompt for any required field not
			// supplied (scripts/agents/pipes are non-TTY and skip this, falling
			// through to the normal missing-arg error instead of blocking).
			if isReaderTerminal(cmd.InOrStdin()) {
				if werr := promptMissingFields(cmd.InOrStdin(), cmd.ErrOrStderr(), parsed.Args, reqFields); werr != nil {
					return werr
				}
			}
			// Reject bad enum values (case-insensitive) before dispatch.
			if verr := validateEnumArgs(parsed.Args, reqFields); verr != nil {
				return verr
			}
			if verr := validateFieldTypes(parsed.Args, reqFields); verr != nil {
				return verr
			}
			parsed.Args = assembleRequestBody(parsed.Args, reqFields)

			// Build the dispatch invocation.
			inv := &dispatch.Invocation{
				OpID:               opID,
				Args:               parsed.Args,
				Format:             dispatchFormat,
				RequestedVariantID: variantID,
				Caller:             dispatch.CallerCLI,
			}
			switch risk {
			case "write":
				inv.AllowWrite = true
			case "destructive":
				inv.AllowDestructive = true
				if yes {
					return cliArgInvalid("destructive calls require signed confirmation: run once without --confirmed to receive confirmation_token, then retry with --confirmed --token <confirmation_token>")
				}
				inv.Confirmed = confirmed
				inv.ConfirmationToken = token
			}

			// Host-control pagination / field-mask flags map to the canonical
			// Google query parameters. The adapter forwards these verbatim to the
			// REST API, which expects fields / pageToken / {pageSize|maxResults} —
			// an earlier __-prefixed form was silently ignored upstream, so the
			// flags had no effect. NOTE: `fields` is the universal Google partial-
			// response param, so the --fields flag and a positional fields= target
			// the SAME canonical arg; when both are given the flag wins (assigned
			// last). Intentional — not a silent positional drop.
			if !noFieldMask && fields != "" {
				if parsed.Args == nil {
					parsed.Args = map[string]any{}
				}
				parsed.Args["fields"] = fields
			}
			if pageToken != "" {
				if parsed.Args == nil {
					parsed.Args = map[string]any{}
				}
				parsed.Args["pageToken"] = pageToken
			}
			if pageSize > 0 {
				if parsed.Args == nil {
					parsed.Args = map[string]any{}
				}
				parsed.Args[pageSizeParam(reqFields)] = pageSize
			}
			_ = raw // reserved for §12.0 --raw passthrough (no shaping)

			// Risk-gate the resolved variant before the executor runs.
			profileName := resolveProfileFlag(cmd)
			disp := newCallDispatcher(profileName)
			res, derr := disp.Dispatch(cmd.Context(), inv)
			// Just-in-time auth: an interactive operator hitting an
			// unauthorized byo_oauth op is prompted to authorize in the
			// browser, then the call is retried once (gum-ossy). Agents and
			// pipes fall through to the structured AUTH_REQUIRED error.
			if derr != nil && maybeJITLogin(cmd, opID, variantID, derr) {
				// Rebuild after login: dispatchers may cache the profile policy
				// and granted-scope allowlist they read during construction.
				disp = newCallDispatcher(profileName)
				res, derr = disp.Dispatch(cmd.Context(), inv)
			}
			if derr != nil {
				return printDispatchError(cmd.ErrOrStderr(), risk, derr)
			}

			out := cmd.OutOrStdout()
			if cliRender {
				v := res.StructuredContent
				if v == nil && len(res.Body) > 0 {
					if jerr := json.Unmarshal(res.Body, &v); jerr != nil {
						return fmt.Errorf("gum call: cannot render %s output — upstream response is not JSON (use --output json or --raw): %w", format, jerr)
					}
				}
				return renderStructured(out, format, v)
			}
			if len(res.Body) > 0 {
				_, _ = out.Write(res.Body)
				if !strings.HasSuffix(string(res.Body), "\n") {
					_, _ = fmt.Fprintln(out)
				}
				return nil
			}
			if res.StructuredContent != nil {
				b, _ := json.Marshal(res.StructuredContent)
				_, _ = out.Write(b)
				_, _ = fmt.Fprintln(out)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&riskRaw, "risk", "", "Risk path: read|write|destructive (required)")
	_ = cmd.MarkFlagRequired("risk")
	cmd.Flags().StringVar(&variantID, "variant-id", "", "Pin variant_id (default: op's default_variant_id)")
	cmd.Flags().StringVar(&fields, "fields", "", "Field mask sent to the upstream API (host control)")
	cmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size for paginated reads (host control)")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "Page token for paginated reads (host control)")
	cmd.Flags().BoolVar(&formatJSON, "json", false, "Render output as JSON (default when piped)")
	cmd.Flags().BoolVar(&formatTOON, "toon", false, "Render output as TOON")
	cmd.Flags().BoolVar(&formatCSV, "csv", false, "Render output as CSV")
	cmd.Flags().BoolVar(&formatMD, "markdown", false, "Render output as Markdown")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: table|json|toon|csv|markdown|raw|value(<path>) (default: table on a terminal, json when piped)")
	cmd.Flags().BoolVar(&skeleton, "skeleton", false, "Print a fillable template of the op's request fields and exit (no upstream call)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Deprecated: destructive variants require --confirmed --token")
	cmd.Flags().BoolVar(&confirmed, "confirmed", false, "Set the signed-confirmation flag for destructive variants")
	cmd.Flags().StringVar(&token, "token", "", "HMAC-SHA256 confirmation token returned by a prior destructive attempt")
	cmd.Flags().BoolVar(&raw, "raw", false, "Return the raw upstream JSON without shaping")
	cmd.Flags().BoolVar(&noFieldMask, "no-field-mask", false, "Disable upstream field_mask injection")

	_ = cmd.RegisterFlagCompletionFunc("fields", completeFieldsForOp)
	_ = cmd.RegisterFlagCompletionFunc("variant-id", completeVariantIDForOp)
	_ = cmd.RegisterFlagCompletionFunc("risk", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"read", "write", "destructive"}, cobra.ShellCompDirectiveNoFileComp
	})
	// `call` is the canonical invocation path; complete the op_id positional
	// across all risk classes (the risk path is supplied via --risk) so it has
	// parity with read/write/destructive/describe (review gum-j4ss).
	cmd.ValidArgsFunction = completeOpIDByRisk("")
	return cmd
}

// completeFieldsForOp proposes field-mask candidates for `gum call --fields=`
// pulled from the resolved op's default variant default_fields (gum-wcwn item 11).
// The completion is intentionally a single concatenated candidate per existing
// prefix-friendly grammar; shell users append a comma to continue listing fields.
func completeFieldsForOp(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	snap := loadCatalog()
	if snap == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	opID := args[0]
	for i := range snap.Ops {
		op := &snap.Ops[i]
		if op.OpID != opID {
			continue
		}
		v := defaultVariant(op)
		if v == nil || v.DefaultFields == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		parts := strings.Split(v.DefaultFields, ",")
		var out []string
		// Suggest each individual field (so users can pick one piece) plus the
		// full default mask (so they can accept the whole thing in one tab).
		seen := map[string]bool{}
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			if toComplete == "" || strings.HasPrefix(p, toComplete) {
				out = append(out, p)
			}
		}
		if toComplete == "" || strings.HasPrefix(v.DefaultFields, toComplete) {
			out = append(out, v.DefaultFields)
		}
		return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// completeVariantIDForOp proposes variant_ids known to the op (gum-wcwn item 11).
func completeVariantIDForOp(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	snap := loadCatalog()
	if snap == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	opID := args[0]
	for i := range snap.Ops {
		op := &snap.Ops[i]
		if op.OpID != opID {
			continue
		}
		var out []string
		for _, v := range op.Variants {
			if toComplete == "" || strings.HasPrefix(v.VariantID, toComplete) {
				out = append(out, v.VariantID)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// normalizeRisk parses the closed enum read|write|destructive. The empty
// string is rejected because the flag is required.
func normalizeRisk(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "read", "write", "destructive":
		return strings.ToLower(strings.TrimSpace(s)), true
	}
	return "", false
}

// selectFormat resolves the mutually-exclusive boolean output flags. Setting
// more than one fails with CLI_ARG_DUPLICATE per spec §12.0 line 2422.
func selectFormat(j, t, c, m bool) (string, error) {
	picked := []string{}
	if j {
		picked = append(picked, "json")
	}
	if t {
		picked = append(picked, "toon")
	}
	if c {
		picked = append(picked, "csv")
	}
	if m {
		picked = append(picked, "markdown")
	}
	if len(picked) > 1 {
		return "", &callargs.Error{Code: "CLI_ARG_DUPLICATE", Key: "format",
			Reason: "use exactly one of --json|--toon|--csv|--markdown"}
	}
	if len(picked) == 0 {
		return "json", nil // §12.0 default
	}
	return picked[0], nil
}

// resolveCallFormat picks the effective output format for `gum call`. An
// explicit --output/-o value or one of the §12.0 format booleans wins (setting
// more than one is CLI_ARG_DUPLICATE). With none, it honors the
// GUM_DEFAULT_OUTPUT env var if it names a valid format, then falls back to a
// TTY-aware default — a human table on a terminal, machine JSON when piped — so
// interactive use is readable while scripts and agents keep stable JSON.
func resolveCallFormat(out io.Writer, output string, j, t, c, m bool) (string, error) {
	var picked []string
	if strings.TrimSpace(output) != "" {
		picked = append(picked, normalizeFormat(output))
	}
	if j {
		picked = append(picked, "json")
	}
	if t {
		picked = append(picked, "toon")
	}
	if c {
		picked = append(picked, "csv")
	}
	if m {
		picked = append(picked, "markdown")
	}
	if len(picked) > 1 {
		return "", &callargs.Error{Code: "CLI_ARG_DUPLICATE", Key: "format",
			Reason: "use exactly one output format (--output or one of --json|--toon|--csv|--markdown)"}
	}
	if len(picked) == 1 {
		f := picked[0]
		if !validCLIFormat(f) {
			return "", cliArgInvalid(fmt.Sprintf("unknown --output %q: want table|json|toon|csv|markdown|raw|value(<path>)", f))
		}
		return f, nil
	}
	// No explicit selector: personal default via env, else TTY-aware default.
	if env := strings.TrimSpace(os.Getenv("GUM_DEFAULT_OUTPUT")); env != "" {
		if f := normalizeFormat(env); validCLIFormat(f) {
			return f, nil
		}
	}
	return resolveOutputFormat("", out), nil
}

// normalizeFormat lowercases a format selector but preserves the verbatim path
// of a value(<path>) selector — JSON field names are case-sensitive.
func normalizeFormat(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "value(") {
		return s
	}
	return strings.ToLower(s)
}

// cliArgInvalid wraps a CLI_ARG_INVALID error with a stable code so callers
// can pattern-match on it.
func cliArgInvalid(reason string) error {
	return &callargs.Error{Code: "CLI_ARG_INVALID", Reason: reason}
}

// requiresConfirmation builds a CLI_ARG_INVALID error that describes the
// signed destructive retry shape. The dispatcher normally issues the token as
// REQUIRES_CONFIRMATION; this helper is kept for pre-dispatch validation paths.
func requiresConfirmation(opID string) error {
	return &callargs.Error{Code: "CLI_ARG_INVALID",
		Reason: fmt.Sprintf("op %s is destructive: run once without --confirmed to receive confirmation_token, then retry with --confirmed --token <confirmation_token>", opID)}
}

// printDispatchError formats the dispatcher's structured error for the CLI.
// RISK_TOOL_MISMATCH is rendered with the expected --risk flag so the LLM /
// user can correct and retry. Output goes through renderStructuredEnvelope
// so every error_code carries a "how_to_fix" remediation block and the raw
// §1421 envelope is preserved under "machine_envelope" (gum-fkme).
func printDispatchError(w io.Writer, requestedRisk string, err error) error {
	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		return err
	}
	extras := map[string]any{}
	if se.ErrCode == dispatch.ErrCodeRiskToolMismatch {
		extras["requested_risk"] = requestedRisk
		extras["required_risk_flag"] = "--risk=" + asString(se.Detail["variant_risk_class"])
	}
	if rerr := renderStructuredEnvelope(w, se, extras); rerr != nil {
		return rerr
	}
	// The full envelope is now on the error stream; wrap so main.go skips the
	// duplicate terse "Error:" line while still exiting non-zero. The wrapper
	// unwraps to se, so upstream errors.As still resolves the StructuredError.
	return errRendered{se}
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case catalog.RiskClass:
		return string(t)
	}
	return ""
}
