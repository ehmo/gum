package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embed"
	"github.com/spf13/cobra"
)

// newMetaToolDispatcher is the dispatcher constructor used by the meta-tool
// commands (read/write/destructive/code). It is a package var so tests can
// inject a fake dispatcher without a live profile, mirroring the
// newCallDispatcher seam used by `gum call` (jit_auth.go).
var newMetaToolDispatcher = newDefaultDispatcherForProfile

// newCodeToolDispatcher avoids eager profile-scope keyring reads for the
// no-auth top-level gum.code op. It remains injectable for CLI tests.
var newCodeToolDispatcher = newDefaultCodeDispatcherForProfile

// parseArgsJSON unmarshals a --args=JSON flag into a map. Empty string yields
// an empty map.
func parseArgsJSON(raw string) (map[string]any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("--args must be a JSON object: %w", err)
	}
	return out, nil
}

// dispatchToWriter dispatches inv (under the named profile) and writes the
// shaped body to w with a trailing newline. Dispatch errors are rendered to
// errW (stderr) so stdout stays clean for pipelines; the returned error is the
// already-rendered sentinel so cobra/main don't double-print.
func dispatchToWriter(ctx context.Context, profile string, w, errW io.Writer, inv *dispatch.Invocation) error {
	return dispatchToWriterWithRisk(ctx, profile, w, errW, inv, "")
}

func dispatchToWriterWithRisk(ctx context.Context, profile string, w, errW io.Writer, inv *dispatch.Invocation, requestedRisk string) error {
	return dispatchToWriterWithFactory(ctx, profile, w, errW, inv, requestedRisk, newMetaToolDispatcher)
}

func dispatchToWriterWithFactory(ctx context.Context, profile string, w, errW io.Writer, inv *dispatch.Invocation, requestedRisk string, factory func(string) dispatch.Dispatcher) error {
	disp := factory(profile)
	shaped, err := disp.Dispatch(ctx, inv)
	if err != nil {
		return printDispatchError(errW, requestedRisk, err)
	}
	// Mirror gum call: if shaping yielded no body bytes but structured content is
	// present, emit the structured content rather than a bare newline.
	if len(shaped.Body) == 0 && shaped.StructuredContent != nil {
		b, merr := json.Marshal(shaped.StructuredContent)
		if merr != nil {
			return merr
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(w)
		return nil
	}
	if _, err := w.Write(shaped.Body); err != nil {
		return err
	}
	if len(shaped.Body) == 0 || shaped.Body[len(shaped.Body)-1] != '\n' {
		_, _ = fmt.Fprintln(w)
	}
	return nil
}

// dispatchAndRender dispatches inv and renders the result in format. CLI-only
// human formats (table/csv/markdown/value) are rendered here from the structured
// result (dispatch runs json under the hood); the kernel's wire formats
// (raw/toon/json, or empty for the default) pass straight through. Shared by the
// meta-tool commands (read/write/destructive) so their --output matches gum call.
func dispatchAndRender(cmd *cobra.Command, inv *dispatch.Invocation, requestedRisk, format string) error {
	out := cmd.OutOrStdout()
	profile := resolveProfileFlag(cmd)
	if cliFormatNeedsStructured(format) {
		inv.Format = "json"
		shaped, err := newMetaToolDispatcher(profile).Dispatch(cmd.Context(), inv)
		if err != nil {
			return printDispatchError(cmd.ErrOrStderr(), requestedRisk, err)
		}
		v := shaped.StructuredContent
		if v == nil && len(shaped.Body) > 0 {
			if jerr := json.Unmarshal(shaped.Body, &v); jerr != nil {
				return fmt.Errorf("render %s output: upstream response is not JSON (use --output json or --format raw): %w", format, jerr)
			}
		}
		return renderStructured(out, format, v)
	}
	inv.Format = format
	return dispatchToWriterWithRisk(cmd.Context(), profile, out, cmd.ErrOrStderr(), inv, requestedRisk)
}

// metaToolFormat resolves the output format for a meta-tool command: an
// explicit --output (any CLI format) wins, else the legacy --format
// (toon|json|raw), else the GUM_DEFAULT_OUTPUT personal default (matching gum
// call), else empty (kernel default = TOON). Unlike gum call there is no
// TTY-aware table default, since these commands mirror the agent-facing
// gum.read/write/destructive tools.
func metaToolFormat(output, format string) (string, error) {
	if strings.TrimSpace(output) != "" {
		f := normalizeFormat(output)
		if !validCLIFormat(f) {
			return "", cliArgInvalid(fmt.Sprintf("unknown --output %q: want table|json|toon|csv|markdown|raw|value(<path>)", f))
		}
		return f, nil
	}
	if f := strings.TrimSpace(format); f != "" {
		// --format is the legacy kernel-format selector; validate locally so a
		// typo yields a clean usage error rather than a dispatch error blob.
		switch f {
		case "toon", "json", "raw":
			return f, nil
		default:
			return "", cliArgInvalid(fmt.Sprintf("unknown --format %q: want toon|json|raw", f))
		}
	}
	// No explicit selector: honour the operator's personal default, matching
	// gum call so GUM_DEFAULT_OUTPUT is not a partial preference. An invalid
	// value is ignored (falls through to the kernel default), never fatal.
	if env := strings.TrimSpace(os.Getenv("GUM_DEFAULT_OUTPUT")); env != "" {
		if f := normalizeFormat(env); validCLIFormat(f) {
			return f, nil
		}
	}
	return "", nil
}

// newReadCmd implements `gum read <op_id> [--args=JSON] [--format=...]`.
func newReadCmd() *cobra.Command {
	var (
		argsJSON string
		format   string
		output   string
	)
	cmd := &cobra.Command{
		Use:   "read <op_id>",
		Short: "Invoke a read-class catalog op",
		Example: "  # List the 5 most recent Gmail messages\n" +
			"  gum read gmail.users.messages.list --args '{\"userId\":\"me\",\"maxResults\":5}'\n" +
			"  # Pipe machine-readable JSON to jq\n" +
			"  gum read calendar.events.list --args '{\"calendarId\":\"primary\"}' | jq '.items[].summary'",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeOpIDByRisk("read"),
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed, err := parseArgsJSON(argsJSON)
			if err != nil {
				return err
			}
			fmtSel, ferr := metaToolFormat(output, format)
			if ferr != nil {
				return ferr
			}
			inv := &dispatch.Invocation{
				OpID:   args[0],
				Args:   parsed,
				Caller: dispatch.CallerCLI,
			}
			return dispatchAndRender(cmd, inv, "read", fmtSel)
		},
	}
	cmd.Flags().StringVar(&argsJSON, "args", "", "JSON object of op arguments")
	cmd.Flags().StringVar(&format, "format", "", "Output format (toon|json|raw)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Human output format: table|json|toon|csv|markdown|raw|value(<path>) (default: kernel TOON)")
	return cmd
}

// newWriteCmd implements `gum write <op_id> [--args=JSON] [--allow-write] [--format=...]`.
func newWriteCmd() *cobra.Command {
	var (
		argsJSON   string
		format     string
		output     string
		allowWrite bool
	)
	cmd := &cobra.Command{
		Use:               "write <op_id>",
		Short:             "Invoke a write-class catalog op",
		Long:              "Invoke a write-class catalog op. --allow-write is required for the policy gate to admit the dispatch.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeOpIDByRisk("write"),
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed, err := parseArgsJSON(argsJSON)
			if err != nil {
				return err
			}
			fmtSel, ferr := metaToolFormat(output, format)
			if ferr != nil {
				return ferr
			}
			inv := &dispatch.Invocation{
				OpID:       args[0],
				Args:       parsed,
				AllowWrite: allowWrite,
				Caller:     dispatch.CallerCLI,
			}
			return dispatchAndRender(cmd, inv, "write", fmtSel)
		},
	}
	cmd.Flags().StringVar(&argsJSON, "args", "", "JSON object of op arguments")
	cmd.Flags().StringVar(&format, "format", "", "Output format (toon|json|raw)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Human output format: table|json|toon|csv|markdown|raw|value(<path>) (default: kernel TOON)")
	cmd.Flags().BoolVar(&allowWrite, "allow-write", false, "Authorise this write")
	return cmd
}

// newDestructiveCmd implements `gum destructive <op_id> [--args=JSON] --confirmed --token=<HMAC>`.
func newDestructiveCmd() *cobra.Command {
	var (
		argsJSON  string
		format    string
		output    string
		confirmed bool
		token     string
	)
	cmd := &cobra.Command{
		Use:               "destructive <op_id>",
		Short:             "Invoke a destructive op (requires a confirmation token)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeOpIDByRisk("destructive"),
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed, err := parseArgsJSON(argsJSON)
			if err != nil {
				return err
			}
			fmtSel, ferr := metaToolFormat(output, format)
			if ferr != nil {
				return ferr
			}
			inv := &dispatch.Invocation{
				OpID:              args[0],
				Args:              parsed,
				Confirmed:         confirmed,
				ConfirmationToken: token,
				AllowDestructive:  true,
				Caller:            dispatch.CallerCLI,
			}
			return dispatchAndRender(cmd, inv, "destructive", fmtSel)
		},
	}
	cmd.Flags().StringVar(&argsJSON, "args", "", "JSON object of op arguments")
	cmd.Flags().StringVar(&format, "format", "", "Output format (toon|json|raw)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Human output format: table|json|toon|csv|markdown|raw|value(<path>) (default: kernel TOON)")
	cmd.Flags().BoolVar(&confirmed, "confirmed", false, "Set the confirmed flag")
	cmd.Flags().StringVar(&token, "token", "", "HMAC-SHA256 confirmation token")
	return cmd
}

// newSearchCmd implements `gum search <query> [--top-k=N] [--format=json|table]`
// over the embedded catalog BM25 index. Default rendering is TTY-aware (gum-me29
// acceptance b / gum-4gey.9): table when stdout is a terminal, JSON otherwise.
func newSearchCmd() *cobra.Command {
	var (
		topK   int
		format string
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "BM25 search the embedded catalog (TTY → table, pipe → JSON)",
		Example: "  # Find ops by keyword (table on a TTY, JSON when piped)\n" +
			"  gum search gmail messages\n" +
			"  gum search \"calendar events\" | jq '.results[].op_id'",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := joinArgs(args)
			snap := loadCatalog()
			out := cmd.OutOrStdout()
			fmtSel := resolveOutputFormat(format, out)

			if snap == nil || len(snap.Ops) == 0 {
				if fmtSel == "table" {
					_, _ = fmt.Fprintln(out, "no results (catalog empty)")
					return nil
				}
				return writeJSON(out, map[string]any{"results": []any{}})
			}
			idx, err := embed.Build(snap)
			if err != nil {
				return fmt.Errorf("build search index: %w", err)
			}
			results := idx.Search(query, topK)

			if fmtSel == "table" {
				renderSearchTable(out, results)
				return nil
			}
			// Normalize a nil slice (empty/no-match query) to [] so the JSON
			// envelope is always "results":[] and never "results":null —
			// consumers never have to special-case nil (gum-l0op #1).
			if results == nil {
				results = []embed.SearchResult{}
			}
			return writeJSON(out, map[string]any{"results": results})
		},
	}
	cmd.Flags().IntVar(&topK, "top-k", 10, "Maximum number of results")
	cmd.Flags().StringVar(&format, "format", "", "Output format: json|table (default: TTY=table, pipe=json)")
	return cmd
}

// renderSearchTable prints BM25 results as a compact aligned table for human
// reading. Columns: op_id, risk, auth, summary (truncated). No external deps;
// %-*s padding gives stable alignment across terminals.
func renderSearchTable(w io.Writer, results []embed.SearchResult) {
	if len(results) == 0 {
		_, _ = fmt.Fprintln(w, "no results")
		return
	}
	opWidth := len("OP_ID")
	for _, r := range results {
		if len(r.OpID) > opWidth {
			opWidth = len(r.OpID)
		}
	}
	_, _ = fmt.Fprintf(w, "%-*s  %-12s  %-12s  %s\n", opWidth, "OP_ID", "RISK", "AUTH", "SUMMARY")
	for _, r := range results {
		summary := r.Summary
		if len(summary) > 80 {
			summary = summary[:77] + "..."
		}
		_, _ = fmt.Fprintf(w, "%-*s  %-12s  %-12s  %s\n", opWidth, r.OpID, r.RiskClass, r.AuthStrategy, summary)
	}
}

// newDescribeCmd implements `gum describe <op_id>` against the embedded catalog.
// Output includes a synthesised example_args block (gum-4gey.10) so callers
// see a runnable shape they can paste into `gum read --args=...`.
func newDescribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe <op_id>",
		Short: "Return the catalog entry for an op_id (with example_args)",
		Example: "  # Inspect an op's schema, scopes, and example_args\n" +
			"  gum describe gmail.users.messages.list",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeOpIDByRisk(""),
		RunE: func(cmd *cobra.Command, args []string) error {
			snap := loadCatalog()
			if snap == nil {
				return fmt.Errorf("OP_NOT_FOUND: %s (catalog not loaded)", args[0])
			}
			for i := range snap.Ops {
				op := &snap.Ops[i]
				if op.OpID == args[0] {
					return writeJSON(cmd.OutOrStdout(), map[string]any{
						"op":           op,
						"example_args": synthesizeExampleArgs(op),
					})
				}
			}
			return fmt.Errorf("OP_NOT_FOUND: %s", args[0])
		},
	}
}

// synthesizeExampleArgs builds a best-effort args map from the op's metadata
// so `gum describe` shows a runnable shape (gum-4gey.10). Three signal
// sources, in priority order:
//
//  1. params_required (first alternative group only — the catalog spec
//     records alternatives, but for a "here's a starting point" example
//     we pick the first).
//  2. URL-template placeholders in the default variant's binding.http.path
//     (e.g. "/gmail/v1/users/{userId}/messages" → "userId").
//  3. params_optional first group, prefixed with a comment marker so
//     scripted callers know they can drop them.
//
// Values are placeholders, not concrete defaults: a single string parameter
// named "user_id" becomes "<user_id>", a name that smells like a page size
// becomes the integer 10. This is intentionally conservative — anything
// fancier would need full JSON Schema introspection (out of scope, see
// gum-wcwn).
func synthesizeExampleArgs(op *catalog.Op) map[string]any {
	out := map[string]any{}
	var required []string
	if len(op.ParamsRequired) > 0 {
		required = op.ParamsRequired[0]
	}
	for _, name := range required {
		out[name] = exampleValueFor(name)
	}
	// Path-template fallback: pulls placeholders the binding declared even
	// when the curator did not surface them in params_required.
	v := defaultVariant(op)
	if v != nil && v.Binding != nil && v.Binding.HTTP != nil {
		for _, name := range pathTemplateParams(v.Binding.HTTP.Path) {
			if _, already := out[name]; already {
				continue
			}
			out[name] = exampleValueFor(name)
		}
	}
	return out
}

// pathTemplateParams returns the {placeholder} names in p in order. Empty
// when p has no template segments. Used by synthesizeExampleArgs.
func pathTemplateParams(p string) []string {
	var names []string
	for i := 0; i < len(p); i++ {
		if p[i] != '{' {
			continue
		}
		j := i + 1
		for j < len(p) && p[j] != '}' {
			j++
		}
		if j >= len(p) {
			break
		}
		names = append(names, p[i+1:j])
		i = j
	}
	return names
}

// exampleValueFor maps a parameter name to a plausible placeholder. Heuristic
// only — strings that look like page sizes get 10, anything else gets a
// "<name>" sigil so the caller knows to substitute. Kept conservative
// rather than clever; full type-aware synthesis lives behind gum-wcwn.
func exampleValueFor(name string) any {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "page_size"), lower == "pagesize", strings.HasSuffix(lower, "_count"):
		return 10
	case lower == "user_id" || lower == "userid":
		return "me"
	case strings.HasSuffix(lower, "_enabled"), strings.HasPrefix(lower, "include_"):
		return false
	}
	return "<" + name + ">"
}

// newCodeCmd implements `gum code <script> [--allow-write] [--allow-destructive] [--timeout-sec=N]`.
// The script may be inline or @path/to/file.risor.
func newCodeCmd() *cobra.Command {
	var (
		allowWrite       bool
		allowDestructive bool
		timeoutSec       int
		language         string
		confirmed        bool
		token            string
		format           string
		output           string
	)
	cmd := &cobra.Command{
		Use:   "code <script-or-@file>",
		Short: "Run a Risor v2 script in the sandbox",
		Long: `Run a Risor v2 script in the gum sandbox.

The sandbox is deliberately small: there is NO filesystem, os/exec, or raw
network access, and Risor v2 has NO for/while/loop keyword — iterate with the
stdlib instead (range(), list().each(), .map()).

Catalog access is provided through these injected builtins:

  gum_call(op_id, args)              Invoke a catalog op; returns its result.
                                     Destructive ops require a prior
                                     gum_confirm_destructive + --allow-destructive.
  gum_search(query)                  BM25-search the catalog; returns a list.
  gum_confirm_destructive(op_id, key) Arm a one-shot destructive confirmation.
  gum_parallel(calls)                Fan out a list of {op_id, args} concurrently.
  gum_print(value)                   Emit a value to the response body.
  gum_http_get(url)                  HTTPS GET to an allowlisted host.

Scripts may be passed inline or as @path/to/file.risor.`,
		Example: `  # Inline: search the catalog and print the first hit.
  gum code 'let hits = gum_search("send email"); gum_print(hits)'

  # Iterate with .each (Risor has no for loop) over parallel results.
  gum code 'gum_print(gum_parallel([{"op_id": "gmail.users.labels.list", "args": {"userId": "me"}}]))'

  # Read a write-class op behind the policy gate.
  gum code --allow-write @./script.risor`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source, err := readScriptArg(args[0])
			if err != nil {
				return err
			}
			fmtSel, ferr := metaToolFormat(output, format)
			if ferr != nil {
				return ferr
			}
			invArgs := map[string]any{
				"language": language,
				"source":   source,
			}
			if timeoutSec > 0 {
				invArgs["timeout_sec"] = timeoutSec
			}
			inv := &dispatch.Invocation{
				OpID:              "gum.code",
				Args:              invArgs,
				AllowWrite:        allowWrite,
				AllowDestructive:  allowDestructive,
				Confirmed:         confirmed,
				ConfirmationToken: token,
				Caller:            dispatch.CallerCLI,
				Format:            fmtSel,
			}
			return dispatchToWriterWithFactory(cmd.Context(), resolveProfileFlag(cmd), cmd.OutOrStdout(), cmd.ErrOrStderr(), inv, "", newCodeToolDispatcher)
		},
	}
	cmd.Flags().BoolVar(&allowWrite, "allow-write", false, "Authorise sandbox writes")
	cmd.Flags().BoolVar(&allowDestructive, "allow-destructive", false, "Authorise destructive sandbox ops")
	cmd.Flags().BoolVar(&confirmed, "confirmed", false, "Set the signed-confirmation flag for elevated sandbox ops")
	cmd.Flags().StringVar(&token, "token", "", "Confirmation token returned by a prior elevated gum code attempt")
	cmd.Flags().IntVar(&timeoutSec, "timeout-sec", 0, "Per-invocation timeout in seconds (0=default)")
	cmd.Flags().StringVar(&language, "language", "risor", "Sandbox language (only risor in v0.1.0)")
	cmd.Flags().StringVar(&format, "format", "", "Output format (toon|json|raw)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: json|toon|raw (raw script output remains raw)")
	return cmd
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

// readScriptArg returns the script body. If raw starts with '@', it is treated
// as a path to a file containing the script.
func readScriptArg(raw string) (string, error) {
	if len(raw) > 0 && raw[0] == '@' {
		b, err := os.ReadFile(raw[1:])
		if err != nil {
			return "", fmt.Errorf("read script file: %w", err)
		}
		return string(b), nil
	}
	return raw, nil
}
