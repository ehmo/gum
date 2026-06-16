package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ehmo/gum/internal/catalog"
)

// request_fields.go is the CLI input-convenience layer built on a catalog op's
// RequestField descriptors. It lets a human pass body fields as plain flat args
// (startDate=2026-04-28 dimensions=query rowLimit=10) instead of hand-writing
// body:='{json}': the body-located fields are coerced to their declared types
// and assembled into the reserved "body" arg the dispatch adapter already reads.
//
// This is purely additive. Ops without RequestFields (today: all of them, until
// the catalog is populated) are untouched, and the deterministic §12.0 grammar
// (key=value, key:=json, body:=json) keeps working unchanged.

// bodyArgKey mirrors adapters.BodyArgKey — the reserved Args key carrying the
// JSON request body. Duplicated here (not imported) to avoid pulling the
// adapters package into the CLI command layer; the §12.0 "body" key is stable.
const bodyArgKey = "body"

// lookupRequestFields returns the RequestField descriptors for opID from the
// embedded catalog, or nil if the op is unknown or declares none.
func lookupRequestFields(opID string) []catalog.RequestField {
	snap := loadCatalog()
	if snap == nil {
		return nil
	}
	for i := range snap.Ops {
		if snap.Ops[i].OpID == opID {
			return snap.Ops[i].RequestFields
		}
	}
	return nil
}

// catalogHasOp reports whether opID exists in the embedded catalog. Used to
// distinguish "unknown op" from "op with no request fields" for --skeleton.
func catalogHasOp(opID string) bool {
	snap := loadCatalog()
	if snap == nil {
		return false
	}
	for i := range snap.Ops {
		if snap.Ops[i].OpID == opID {
			return true
		}
	}
	return false
}

// arrayRequestFields returns the names of array-typed request fields, so the
// arg parser treats repeated keys (dimensions=query dimensions=page) as a slice.
func arrayRequestFields(fields []catalog.RequestField) []string {
	var out []string
	for _, f := range fields {
		if f.Type == "array" {
			out = append(out, f.Name)
		}
	}
	return out
}

// assembleRequestBody moves body-located request fields out of the flat args map
// and into the nested "body" map the adapter serializes, coercing each value to
// its declared type. An explicit body (from body:=json) is preserved and takes
// precedence over a flat field of the same name. args is mutated in place and
// returned. A nil/empty fields list (the common case today) is a no-op.
func assembleRequestBody(args map[string]any, fields []catalog.RequestField) map[string]any {
	if len(args) == 0 || len(fields) == 0 {
		return args
	}
	// Preserve an explicit body:=json if it parsed to an object; otherwise leave
	// any non-object explicit body untouched and skip flat-field assembly into it.
	var body map[string]any
	if existing, ok := args[bodyArgKey]; ok {
		m, isMap := existing.(map[string]any)
		if !isMap {
			return args // explicit non-object body wins; don't second-guess it
		}
		body = m
	}

	moved := false
	for _, f := range fields {
		if f.Location != catalog.RequestFieldBody {
			continue
		}
		raw, ok := args[f.Name]
		if !ok {
			continue
		}
		if body == nil {
			body = map[string]any{}
		}
		if _, exists := body[f.Name]; !exists { // explicit body field wins
			body[f.Name] = coerceFieldValue(raw, f)
		}
		delete(args, f.Name)
		moved = true
	}
	if moved {
		args[bodyArgKey] = body
	}
	return args
}

// coerceFieldValue converts a flat string arg to the field's declared type.
// Values already typed (from key:=json, or a slice from a repeated array key)
// pass through; array elements are coerced per the field's item_type.
func coerceFieldValue(v any, f catalog.RequestField) any {
	if f.Type == "array" {
		if arr, ok := v.([]any); ok {
			out := make([]any, len(arr))
			for i, e := range arr {
				out[i] = coerceScalar(e, f.ItemType)
			}
			return out
		}
		// A single occurrence of an array field (e.g. `dimensions=query`) arrives
		// as a bare scalar; wrap it so the body carries ["query"], not "query"
		// (a case-sensitive API rejects the un-wrapped scalar with a 400).
		return []any{coerceScalar(v, f.ItemType)}
	}
	return coerceScalar(v, f.Type)
}

// coerceScalar parses a string into the given JSON scalar type; non-strings and
// unparseable strings pass through unchanged.
func coerceScalar(v any, typ string) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	switch typ {
	case "integer":
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	case "number":
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return n
		}
	case "boolean":
		if b, err := strconv.ParseBool(s); err == nil {
			return b
		}
	}
	return s
}

// renderSkeleton prints a fillable, copy-pasteable template of an op's request
// fields (grouped by location, with type, required, enum, and default hints),
// so a human can discover what to pass without reading the API docs. The lines
// use the §12.0 key=value grammar, so the body of the skeleton can be edited
// and pasted straight into a `gum call` invocation.
func renderSkeleton(w io.Writer, opID string, fields []catalog.RequestField) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — fill in and pass to `gum call %s --risk=<read|write|destructive> <args>`\n", opID, opID)
	groups := []struct {
		loc   catalog.RequestFieldLocation
		label string
	}{
		{catalog.RequestFieldPath, "path parameters"},
		{catalog.RequestFieldQuery, "query parameters"},
		{catalog.RequestFieldBody, "body fields"},
		{catalog.RequestFieldArg, "arguments"},
	}
	wrote := false
	for _, g := range groups {
		var inGroup []catalog.RequestField
		for _, f := range fields {
			if f.Location == g.loc {
				inGroup = append(inGroup, f)
			}
		}
		if len(inGroup) == 0 {
			continue
		}
		wrote = true
		fmt.Fprintf(&b, "# %s:\n", g.label)
		for _, f := range inGroup {
			fmt.Fprintf(&b, "%s=<%s>", f.Name, fieldTypeHint(f))
			var notes []string
			if f.Required {
				notes = append(notes, "required")
			}
			if f.Type == "array" {
				notes = append(notes, "repeatable")
			}
			if f.Format != "" {
				notes = append(notes, f.Format)
			}
			if len(f.Enum) > 0 {
				notes = append(notes, "choices: "+strings.Join(f.Enum, "|"))
			}
			if f.Default != "" {
				notes = append(notes, "default: "+f.Default)
			}
			if len(notes) > 0 {
				fmt.Fprintf(&b, "   # %s", strings.Join(notes, "; "))
			}
			b.WriteByte('\n')
		}
	}
	if !wrote {
		b.WriteString("# (this operation takes no request parameters)\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// fieldTypeHint returns a placeholder type word for the skeleton.
func fieldTypeHint(f catalog.RequestField) string {
	if f.Type == "array" {
		if f.ItemType != "" {
			return f.ItemType
		}
		return "value"
	}
	if f.Type == "" {
		return "value"
	}
	return f.Type
}

// validateEnumArgs rejects flat args whose value is not a member of the field's
// enum (case-insensitive, so query/QUERY both pass). Runs on the flat args
// before body assembly; array values are checked element-wise. A matched value
// is normalized in place to the enum's canonical case — Google APIs are
// case-sensitive (Gmail wants "minimal", Sheets wants "ROWS"), so accepting
// "MINIMAL" but forwarding it verbatim would earn a 400 from the server.
func validateEnumArgs(args map[string]any, fields []catalog.RequestField) error {
	for _, f := range fields {
		if len(f.Enum) == 0 {
			continue
		}
		raw, ok := args[f.Name]
		if !ok {
			continue
		}
		switch t := raw.(type) {
		case string:
			canon, ok := canonicalEnum(f.Enum, t)
			if !ok {
				return cliArgInvalid(fmt.Sprintf("%s=%q is not a valid choice: %s", f.Name, t, strings.Join(f.Enum, "|")))
			}
			args[f.Name] = canon
		case []any:
			for i, e := range t {
				s, isStr := e.(string)
				if !isStr {
					s = fmt.Sprintf("%v", e)
				}
				canon, ok := canonicalEnum(f.Enum, s)
				if !ok {
					return cliArgInvalid(fmt.Sprintf("%s=%q is not a valid choice: %s", f.Name, s, strings.Join(f.Enum, "|")))
				}
				t[i] = canon
			}
		default:
			// A typed scalar (int/float/bool from key:=json) still needs enum
			// validation. flattenToStrings returns nil for a bare scalar, so fall
			// back to its string form rather than skipping validation entirely —
			// otherwise e.g. `mode:=5` against enum [a,b,c] would slip through to
			// the API instead of getting the local "not a valid choice" error.
			vals := flattenToStrings(raw)
			if vals == nil && raw != nil {
				vals = []string{fmt.Sprintf("%v", raw)}
			}
			for _, v := range vals {
				if !enumContains(f.Enum, v) {
					return cliArgInvalid(fmt.Sprintf("%s=%q is not a valid choice: %s", f.Name, v, strings.Join(f.Enum, "|")))
				}
			}
		}
	}
	// Also validate enum fields that arrived pre-nested inside an explicit
	// body:=json object (args[bodyArgKey]). Those never appear at the top level,
	// so the loop above silently skips them. Normalize canonical case in place so
	// the already-assembled body map is ready for dispatch without a second pass.
	if bodyRaw, ok := args[bodyArgKey]; ok {
		body, isMap := bodyRaw.(map[string]any)
		if !isMap {
			return nil // non-object explicit body: nothing to validate here
		}
		for _, f := range fields {
			if len(f.Enum) == 0 {
				continue
			}
			raw, ok := body[f.Name]
			if !ok {
				continue
			}
			switch t := raw.(type) {
			case string:
				canon, ok := canonicalEnum(f.Enum, t)
				if !ok {
					return cliArgInvalid(fmt.Sprintf("%s=%q is not a valid choice: %s", f.Name, t, strings.Join(f.Enum, "|")))
				}
				body[f.Name] = canon
			case []any:
				for i, e := range t {
					s, isStr := e.(string)
					if !isStr {
						s = fmt.Sprintf("%v", e)
					}
					canon, ok := canonicalEnum(f.Enum, s)
					if !ok {
						return cliArgInvalid(fmt.Sprintf("%s=%q is not a valid choice: %s", f.Name, s, strings.Join(f.Enum, "|")))
					}
					t[i] = canon
				}
			default:
				for _, v := range flattenToStrings(raw) {
					if !enumContains(f.Enum, v) {
						return cliArgInvalid(fmt.Sprintf("%s=%q is not a valid choice: %s", f.Name, v, strings.Join(f.Enum, "|")))
					}
				}
			}
		}
	}
	return nil
}

func enumContains(enum []string, v string) bool {
	_, ok := canonicalEnum(enum, v)
	return ok
}

// pageSizeParam returns the query parameter the op uses for page size. Google
// REST APIs are split: most use "maxResults" (Gmail, Calendar, Tasks), the newer
// ones use "pageSize" (Drive). The --page-size host-control flag is mapped to
// whichever the op actually declares, defaulting to "pageSize".
func pageSizeParam(fields []catalog.RequestField) string {
	for _, f := range fields {
		if f.Name == "pageSize" {
			return "pageSize"
		}
	}
	for _, f := range fields {
		if f.Name == "maxResults" {
			return "maxResults"
		}
	}
	return "pageSize"
}

// canonicalEnum returns the enum entry that matches v case-insensitively, so a
// caller can both validate and normalize to the API's expected casing.
func canonicalEnum(enum []string, v string) (string, bool) {
	for _, e := range enum {
		if strings.EqualFold(e, v) {
			return e, true
		}
	}
	return "", false
}

func flattenToStrings(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			} else {
				// Non-string element (e.g. a number from key:=json) — render it
				// so the enum check still sees and can reject it, rather than
				// silently dropping it.
				out = append(out, fmt.Sprintf("%v", e))
			}
		}
		return out
	}
	return nil
}

// validateFieldTypes rejects a flat string arg that cannot parse as its declared
// scalar type, with a local, field-specific error instead of a confusing
// upstream 400. Values already typed via key:=json (non-strings) are trusted.
func validateFieldTypes(args map[string]any, fields []catalog.RequestField) error {
	for _, f := range fields {
		raw, ok := args[f.Name]
		if !ok {
			continue
		}
		// Array fields: validate each element against the item type, whether the
		// value arrived as a single scalar, a repeated-key []any, or a JSON array
		// (key:=json). Without this, a bad element (count:=["one"]) slips past the
		// non-string guard below and surfaces only as an upstream 400.
		if f.Type == "array" {
			for _, e := range flattenToStrings(raw) {
				if err := checkScalarType(f.Name, e, f.ItemType); err != nil {
					return err
				}
			}
			continue
		}
		s, isStr := raw.(string)
		if !isStr {
			continue
		}
		if err := checkScalarType(f.Name, s, f.Type); err != nil {
			return err
		}
	}
	return nil
}

// checkScalarType returns a local, field-specific error when s cannot parse as
// the declared scalar type. Unknown/empty types (e.g. plain strings) pass.
func checkScalarType(name, s, typ string) error {
	switch typ {
	case "integer":
		if _, err := strconv.ParseInt(s, 10, 64); err != nil {
			return cliArgInvalid(fmt.Sprintf("%s=%q: expected an integer", name, s))
		}
	case "number":
		if _, err := strconv.ParseFloat(s, 64); err != nil {
			return cliArgInvalid(fmt.Sprintf("%s=%q: expected a number", name, s))
		}
	case "boolean":
		if _, err := strconv.ParseBool(s); err != nil {
			return cliArgInvalid(fmt.Sprintf("%s=%q: expected true or false", name, s))
		}
	}
	return nil
}

// promptMissingFields is the interactive wizard: for each required field not
// already supplied, it prints a prompt to errOut (enum choices shown when
// present) and reads a value from in. Array fields accept a comma-separated
// list. An empty answer for a required field is an error. The caller gates this
// on stdin being a TTY so scripts/agents never block on a prompt.
func promptMissingFields(in io.Reader, errOut io.Writer, args map[string]any, fields []catalog.RequestField) error {
	reader := bufio.NewReader(in)
	for _, f := range fields {
		if !f.Required {
			continue
		}
		if fieldAlreadySupplied(args, f.Name) {
			continue
		}
		prompt := f.Name
		if f.Description != "" {
			prompt += " — " + f.Description
		}
		if len(f.Enum) > 0 {
			prompt += " [" + strings.Join(f.Enum, "|") + "]"
		}
		_, _ = fmt.Fprintf(errOut, "%s: ", prompt)
		line, rerr := reader.ReadString('\n')
		val := strings.TrimSpace(line)
		if val == "" {
			if rerr != nil && !errors.Is(rerr, io.EOF) {
				// A real I/O error (broken pipe, EIO) — surface it rather than
				// masking it behind a misleading "is required".
				return fmt.Errorf("reading %s: %w", f.Name, rerr)
			}
			if errors.Is(rerr, io.EOF) {
				return cliArgInvalid(fmt.Sprintf("%s is required (stdin closed before it was provided)", f.Name))
			}
			return cliArgInvalid(fmt.Sprintf("%s is required", f.Name))
		}
		if f.Type == "array" {
			parts := splitCSVToAny(val)
			if len(parts) == 0 {
				// e.g. the answer was just "," — non-empty line, empty list.
				return cliArgInvalid(fmt.Sprintf("%s is required (empty list not accepted)", f.Name))
			}
			args[f.Name] = parts
		} else {
			args[f.Name] = val
		}
	}
	return nil
}

// fieldAlreadySupplied reports whether name was given as a flat arg or already
// lives inside an explicit body:=json object — so the wizard doesn't re-prompt
// for a field the operator already provided.
func fieldAlreadySupplied(args map[string]any, name string) bool {
	if _, ok := args[name]; ok {
		return true
	}
	if body, ok := args[bodyArgKey].(map[string]any); ok {
		if _, ok := body[name]; ok {
			return true
		}
	}
	return false
}

// splitCSVToAny splits a comma-separated answer into a trimmed []any.
func splitCSVToAny(s string) []any {
	parts := strings.Split(s, ",")
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// kebabCase converts a camelCase API field name to a kebab-case flag name,
// acronym-aware so runs of capitals don't become per-letter hyphens
// (siteUrl -> site-url, labelIds -> label-ids, URLPath -> url-path, ID -> id,
// htmlContent -> html-content). A hyphen is inserted before a capital only at a
// real word boundary: after a lowercase/digit, or before the last capital of a
// run that starts a new lowercase word.
func kebabCase(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		upper := c >= 'A' && c <= 'Z'
		if upper && i > 0 {
			prevLowerOrDigit := (r[i-1] >= 'a' && r[i-1] <= 'z') || (r[i-1] >= '0' && r[i-1] <= '9')
			nextLower := i+1 < len(r) && r[i+1] >= 'a' && r[i+1] <= 'z'
			if prevLowerOrDigit || nextLower {
				b.WriteByte('-')
			}
		}
		if upper {
			b.WriteRune(c - 'A' + 'a')
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// requestFieldFlagHelp builds the --help text for a derived flag.
func requestFieldFlagHelp(f catalog.RequestField) string {
	parts := []string{string(f.Location)}
	if f.Required {
		parts = append(parts, "required")
	}
	if f.Type != "" && f.Type != "string" {
		parts = append(parts, f.Type)
	}
	if len(f.Enum) > 0 {
		parts = append(parts, "choices: "+strings.Join(f.Enum, "|"))
	}
	meta := "(" + strings.Join(parts, "; ") + ")"
	if f.Description != "" {
		return f.Description + " " + meta
	}
	return meta
}

// applyKebabFlags reads the dynamically-registered --kebab flags off cmd and
// merges any that were set into args under the field's canonical (camelCase)
// name, so they route exactly like the equivalent key=value positional. For an
// array field the flag values are appended to any positional values already
// accumulated for the same field (both forms mean "add to the list"); for a
// scalar field the explicit flag wins over a positional of the same field.
func applyKebabFlags(cmd *cobra.Command, args map[string]any, fields []catalog.RequestField) {
	for _, f := range fields {
		name := kebabCase(f.Name)
		fl := cmd.Flags().Lookup(name)
		if fl == nil || !fl.Changed {
			continue
		}
		if f.Type == "array" {
			vals, _ := cmd.Flags().GetStringArray(name)
			// Preserve positional values rather than clobbering them — merge so a
			// user mixing `dimensions=query` and `--dimensions=page` gets both.
			arr, _ := args[f.Name].([]any)
			if s, ok := args[f.Name].(string); ok { // single positional, pre-coercion
				arr = []any{s}
			}
			for _, v := range vals {
				arr = append(arr, v)
			}
			args[f.Name] = arr
		} else {
			v, _ := cmd.Flags().GetString(name)
			args[f.Name] = v
		}
	}
}

// registerDynamicCallFlags inspects rawArgs for a `gum call <op_id>` invocation
// and registers a typed --kebab flag on the call command for each of that op's
// RequestFields, BEFORE cobra parses. This is the two-pass that gives the
// schema-derived flags real cobra behavior (--help, completion, typo errors).
// A no-op for any other command or an unknown op.
func registerDynamicCallFlags(root *cobra.Command, rawArgs []string) {
	callCmd, _, err := root.Find([]string{"call"})
	if err != nil || callCmd == nil || callCmd.Name() != "call" {
		return
	}
	// The op_id is the first positional after "call". Two guards keep a flag's
	// space-form value from being mistaken for it: (1) skip the value token that
	// follows a known value-taking flag (--variant-id gmail.v1.rest..., --profile
	// my.profile, --risk read); (2) require a dot, which every catalog op_id has
	// (gmail.users.messages.get, flights.search, gum.code). A miss is graceful —
	// no dynamic flags register and the user re-orders.
	valueFlags := map[string]bool{
		"--risk": true, "--variant-id": true, "--fields": true, "--page-size": true,
		"--page-token": true, "--output": true, "-o": true, "--token": true,
		"--profile": true, "--log-level": true, "--log-format": true,
	}
	opID := ""
	seenCall := false
	skipNext := false
	for _, a := range rawArgs {
		if !seenCall {
			if a == "call" {
				seenCall = true
			}
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && valueFlags[a] {
				skipNext = true // this flag's value is the next token, not the op_id
			}
			continue
		}
		if !strings.Contains(a, ".") {
			continue // op_ids always contain a dot
		}
		opID = a
		break
	}
	if opID == "" {
		return
	}
	for _, f := range lookupRequestFields(opID) {
		name := kebabCase(f.Name)
		if callCmd.Flags().Lookup(name) != nil {
			// Never shadow a real call flag (--fields/--page-token/--page-size are
			// host controls; --raw is a bool). A field whose kebab name collides
			// isn't exposed as a flag, but the positional key=value form still
			// works (e.g. pageToken=…) and --skeleton still lists it.
			continue
		}
		help := requestFieldFlagHelp(f)
		if f.Type == "array" {
			callCmd.Flags().StringArray(name, nil, help)
		} else {
			callCmd.Flags().String(name, "", help)
		}
		if len(f.Enum) > 0 {
			enum := append([]string(nil), f.Enum...)
			_ = callCmd.RegisterFlagCompletionFunc(name, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
				return enum, cobra.ShellCompDirectiveNoFileComp
			})
		}
	}
}
