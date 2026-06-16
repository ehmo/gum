// Package callargs implements the spec §12.0 CLI argument grammar for the
// `gum call` command. Positional arguments come in three forms:
//
//	key=value      — scalar string assignment
//	key:=json      — typed JSON value (numbers, booleans, arrays, objects)
//	@path.json     — load a JSON object from a file (or @- for stdin) and
//	                 merge it into the argument map before inline args apply
//
// Dotted keys address nested objects (message.subject=Hi). Literal dots are
// escaped as \., and backslashes as \\.
//
// Duplicate scalar keys fail with CLI_ARG_DUPLICATE unless ArrayFields lists
// the key as schema-typed array (spec §12.0 rule 2); duplicate array-typed
// keys append.
//
// Anything that does not match one of the three positional forms fails with
// CLI_ARG_INVALID. Host-control flags (--fields, --page-size, --page-token,
// output-format booleans) are parsed by cobra before this package runs.
package callargs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Result is what ParseArgs returns on success: the merged argument map plus
// the set of keys that came from positional inline args (used only by tests
// that want to verify file-vs-inline ordering).
type Result struct {
	Args       map[string]any
	InlineKeys map[string]struct{}
	FromFiles  map[string]struct{}
}

// Error is the CLI-arg-grammar error. Code is "CLI_ARG_DUPLICATE" or
// "CLI_ARG_INVALID"; Key, Arg, and Reason are populated where they apply.
type Error struct {
	Code   string
	Key    string
	Arg    string
	Reason string
}

func (e *Error) Error() string {
	switch e.Code {
	case "CLI_ARG_DUPLICATE":
		return fmt.Sprintf("CLI_ARG_DUPLICATE: %s", e.Key)
	case "CLI_ARG_INVALID":
		if e.Arg != "" && e.Reason != "" {
			return fmt.Sprintf("CLI_ARG_INVALID: %q: %s", e.Arg, e.Reason)
		}
		if e.Arg != "" {
			return fmt.Sprintf("CLI_ARG_INVALID: %q", e.Arg)
		}
		if e.Reason != "" {
			return fmt.Sprintf("CLI_ARG_INVALID: %s", e.Reason)
		}
		return "CLI_ARG_INVALID"
	default:
		return "CLI_ARG_ERROR: " + e.Code
	}
}

// Options controls the parser's schema-aware behavior. ArrayFields lists the
// dotted-key paths that are declared array-typed in the resolved op/variant
// schema; repeated assignments to these keys append rather than error.
//
// Stdin lets tests inject a stand-in for os.Stdin (used by @- file loads).
type Options struct {
	ArrayFields []string
	Stdin       io.Reader
}

// ParseArgs walks args left to right per spec §12.0. File loads (@path) merge
// first; inline args override on conflict per the spec's "inline args
// override file values" rule.
func ParseArgs(args []string, opts Options) (*Result, error) {
	out := &Result{
		Args:       map[string]any{},
		InlineKeys: map[string]struct{}{},
		FromFiles:  map[string]struct{}{},
	}
	arraySet := map[string]struct{}{}
	for _, f := range opts.ArrayFields {
		arraySet[f] = struct{}{}
	}

	// Spec §12.0 rule 3: file args merge first; inline args override.
	var fileArgs []string
	var inlineArgs []string
	for _, a := range args {
		if strings.HasPrefix(a, "@") {
			fileArgs = append(fileArgs, a)
		} else {
			inlineArgs = append(inlineArgs, a)
		}
	}

	for _, a := range fileArgs {
		body, err := loadFile(a, opts.Stdin)
		if err != nil {
			return nil, err
		}
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err != nil {
			return nil, &Error{Code: "CLI_ARG_INVALID", Arg: a, Reason: "not a JSON object: " + err.Error()}
		}
		for k, v := range obj {
			out.Args[k] = v
			out.FromFiles[k] = struct{}{}
			// Also record nested leaf paths (message.subject) so a later inline
			// arg can override a file-sourced NESTED value, per spec §12.0 rule 3
			// (inline overrides file) — not just top-level keys.
			recordNestedFilePaths(out, []string{k}, v)
		}
	}

	for _, a := range inlineArgs {
		if err := applyInline(a, out, arraySet); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// maxArgFileBytes caps the size of an @file / @- argument body. A JSON request
// body well over this is pathological; the cap stops `@/dev/zero` or a huge
// file from exhausting memory (especially when gum runs as a long-lived MCP
// server).
const maxArgFileBytes = 64 << 20 // 64 MiB

// loadFile resolves @- (stdin) or @path.json (file) into raw bytes, capped at
// maxArgFileBytes.
func loadFile(arg string, stdin io.Reader) ([]byte, error) {
	if arg == "@-" {
		r := stdin
		if r == nil {
			r = os.Stdin
		}
		return readCappedArgFile(r, arg)
	}
	path := strings.TrimPrefix(arg, "@")
	if path == "" {
		return nil, &Error{Code: "CLI_ARG_INVALID", Arg: arg, Reason: "empty @ file reference"}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, &Error{Code: "CLI_ARG_INVALID", Arg: arg, Reason: err.Error()}
	}
	defer func() { _ = f.Close() }()
	return readCappedArgFile(f, arg)
}

// readCappedArgFile reads r up to maxArgFileBytes, returning CLI_ARG_INVALID if
// the source exceeds the limit.
func readCappedArgFile(r io.Reader, arg string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxArgFileBytes+1))
	if err != nil {
		return nil, &Error{Code: "CLI_ARG_INVALID", Arg: arg, Reason: err.Error()}
	}
	if len(data) > maxArgFileBytes {
		return nil, &Error{Code: "CLI_ARG_INVALID", Arg: arg,
			Reason: fmt.Sprintf("file exceeds the %d-byte limit", maxArgFileBytes)}
	}
	return data, nil
}

// applyInline parses one inline positional arg and merges it into the result.
func applyInline(a string, out *Result, arraySet map[string]struct{}) error {
	if a == "" {
		return &Error{Code: "CLI_ARG_INVALID", Arg: a, Reason: "empty positional argument"}
	}
	if strings.HasPrefix(a, "-") {
		return &Error{Code: "CLI_ARG_INVALID", Arg: a, Reason: "unrecognized flag after operation id"}
	}
	key, val, kind, err := splitKVPair(a)
	if err != nil {
		return err
	}
	parts, perr := splitDottedKey(key)
	if perr != nil {
		return perr
	}

	var assigned any
	switch kind {
	case "string":
		assigned = val
	case "json":
		var jv any
		dec := json.NewDecoder(strings.NewReader(val))
		dec.UseNumber()
		if err := dec.Decode(&jv); err != nil {
			return &Error{Code: "CLI_ARG_INVALID", Arg: a, Reason: "key:=json value is not JSON: " + err.Error()}
		}
		// Reject trailing non-whitespace after the JSON value to catch
		// silent truncation like `x:=10garbage`.
		trailing := strings.TrimSpace(val[dec.InputOffset():])
		if trailing != "" {
			return &Error{Code: "CLI_ARG_INVALID", Arg: a, Reason: "trailing data after JSON: " + trailing}
		}
		assigned = jv
	}

	// Dotted assignment into nested map.
	if len(parts) > 1 {
		return assignNested(out, parts, assigned, a, arraySet)
	}
	return assignTop(out, parts[0], assigned, a, arraySet)
}

func assignTop(out *Result, key string, value any, raw string, arraySet map[string]struct{}) error {
	_, inline := out.InlineKeys[key]
	out.InlineKeys[key] = struct{}{}
	_, fromFile := out.FromFiles[key]
	existing, exists := out.Args[key]
	if !exists {
		out.Args[key] = value
		return nil
	}
	// File-sourced value is always overridden by the first inline.
	if fromFile && !inline {
		out.Args[key] = value
		// Remove from FromFiles so a second inline does not also override.
		delete(out.FromFiles, key)
		return nil
	}
	// Schema-typed array: append.
	if _, ok := arraySet[key]; ok {
		out.Args[key] = appendArray(existing, value)
		return nil
	}
	return &Error{Code: "CLI_ARG_DUPLICATE", Key: key, Arg: raw}
}

func assignNested(out *Result, parts []string, value any, raw string, arraySet map[string]struct{}) error {
	// Walk into nested maps, creating as needed.
	cur := out.Args
	for i, p := range parts[:len(parts)-1] {
		nv, ok := cur[p]
		if !ok {
			next := map[string]any{}
			cur[p] = next
			cur = next
			continue
		}
		nm, ok := nv.(map[string]any)
		if !ok {
			return &Error{Code: "CLI_ARG_INVALID", Arg: raw,
				Reason: fmt.Sprintf("path %s collides with non-object value at %s",
					joinDotted(parts), joinDotted(parts[:i+1]))}
		}
		cur = nm
	}
	last := parts[len(parts)-1]
	dotted := joinDotted(parts)
	if existing, ok := cur[last]; ok {
		_, fromFile := out.FromFiles[dotted]
		_, inline := out.InlineKeys[dotted]
		// A file-sourced nested value is overridden by the first inline arg
		// (mirrors assignTop). Remove it from FromFiles so a SECOND inline for
		// the same path still conflicts.
		if fromFile && !inline {
			cur[last] = value
			out.InlineKeys[dotted] = struct{}{}
			delete(out.FromFiles, dotted)
			return nil
		}
		if _, isArray := arraySet[dotted]; isArray {
			cur[last] = appendArray(existing, value)
			return nil
		}
		return &Error{Code: "CLI_ARG_DUPLICATE", Key: dotted, Arg: raw}
	}
	cur[last] = value
	out.InlineKeys[dotted] = struct{}{}
	return nil
}

// recordNestedFilePaths walks a file-sourced value and records every nested
// leaf path (joinDotted form) in out.FromFiles, so assignNested can recognize a
// file-sourced nested key as overridable by an inline arg.
func recordNestedFilePaths(out *Result, prefix []string, v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for k, vv := range m {
		path := append(append([]string{}, prefix...), k)
		out.FromFiles[joinDotted(path)] = struct{}{}
		recordNestedFilePaths(out, path, vv)
	}
}

func appendArray(existing, value any) any {
	if arr, ok := existing.([]any); ok {
		return append(arr, value)
	}
	return []any{existing, value}
}

// splitKVPair finds the := or = separator. Returns (key, value, kind)
// where kind is "string" for key=value and "json" for key:=json. The split
// honors backslash escapes inside the key.
func splitKVPair(arg string) (string, string, string, error) {
	for i := 0; i < len(arg); i++ {
		// Backslash escape skips the next character.
		if arg[i] == '\\' && i+1 < len(arg) {
			i++
			continue
		}
		if arg[i] == ':' && i+1 < len(arg) && arg[i+1] == '=' {
			return arg[:i], arg[i+2:], "json", nil
		}
		if arg[i] == '=' {
			return arg[:i], arg[i+1:], "string", nil
		}
	}
	return "", "", "", &Error{Code: "CLI_ARG_INVALID", Arg: arg,
		Reason: "expected key=value, key:=json, or @file"}
}

// splitDottedKey splits a dotted key into path components, honoring \. as a
// literal dot and \\ as a literal backslash.
func splitDottedKey(key string) ([]string, error) {
	if key == "" {
		return nil, &Error{Code: "CLI_ARG_INVALID", Reason: "empty key"}
	}
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c == '\\' {
			if i+1 >= len(key) {
				return nil, &Error{Code: "CLI_ARG_INVALID", Key: key, Reason: "dangling backslash in key"}
			}
			next := key[i+1]
			if next != '.' && next != '\\' {
				return nil, &Error{Code: "CLI_ARG_INVALID", Key: key,
					Reason: fmt.Sprintf("unrecognized escape \\%c (use \\. for literal dot or \\\\ for backslash)", next)}
			}
			cur.WriteByte(next)
			i++
			continue
		}
		if c == '.' {
			if cur.Len() == 0 {
				return nil, &Error{Code: "CLI_ARG_INVALID", Key: key, Reason: "empty segment in dotted key"}
			}
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	if cur.Len() == 0 {
		return nil, &Error{Code: "CLI_ARG_INVALID", Key: key, Reason: "trailing dot in dotted key"}
	}
	parts = append(parts, cur.String())
	return parts, nil
}

// joinDotted is the inverse mirror of splitDottedKey for error reporting. It
// re-encodes literal dots in segments as \. so the path round-trips.
func joinDotted(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		p = strings.ReplaceAll(p, "\\", "\\\\")
		p = strings.ReplaceAll(p, ".", "\\.")
		out[i] = p
	}
	return strings.Join(out, ".")
}

// IsDuplicate is a convenience for callers that want to check the error code.
func IsDuplicate(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == "CLI_ARG_DUPLICATE"
}

// IsInvalid is a convenience for callers that want to check the error code.
func IsInvalid(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == "CLI_ARG_INVALID"
}
