package dispatch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// errorsGoFile is the single source of truth for the stable runtime error
// codes. Both tests in this file read it from disk rather than from a Go
// literal, so a new ErrCode* constant cannot pass without also updating the
// spec list and the header count.
const errorsGoFile = "errors.go"

// specStableCodeMarker opens the spec paragraph that enumerates the codes.
const specStableCodeMarker = "**Stable runtime error codes.**"

// specStableCodeTerminator ends the enumeration. Prose after it describes
// individual envelopes and mentions codes again, so the scan must stop here
// or it would also pick up codes named only in the descriptions.
const specStableCodeTerminator = "are the spec's stable runtime structured error codes"

var (
	backtickedCode  = regexp.MustCompile("`([A-Z][A-Z_]{2,})`")
	headerCountLine = regexp.MustCompile(`// All (\d+) stable runtime error codes`)
)

// errorCodeConstants parses errors.go and returns every string value declared
// with type ErrorCode, keyed by the constant's Go name.
func errorCodeConstants(t *testing.T) map[string]string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, errorsGoFile, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", errorsGoFile, err)
	}

	codes := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != "ErrorCode" {
				continue
			}

			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s value %s: %v", name.Name, lit.Value, err)
				}
				codes[name.Name] = value
			}
		}
	}

	if len(codes) == 0 {
		t.Fatalf("no ErrorCode constants found in %s", errorsGoFile)
	}
	return codes
}

// findSpec walks up from the package directory looking for docs/spec.md.
// It returns "" when the file is absent, which is the normal state of the
// public export: scripts/public-release-manifest.json lists docs/spec.md
// under forbidden_public_paths, so the export carries no copy of it.
func findSpec(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		candidate := filepath.Join(dir, "docs", "spec.md")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// TestErrorCodesMatchSpecList fails when errors.go and the spec's enumerated
// stable-code list disagree in either direction. The list drifted twice before
// this gate existed: POLICY_DENIED and RESPONSE_TOO_LARGE both shipped and were
// reachable while the spec enumerated 30 of the 32 codes (bead gum-irx2).
func TestErrorCodesMatchSpecList(t *testing.T) {
	specPath := findSpec(t)
	if specPath == "" {
		t.Skip("docs/spec.md not present; it is private-only, see scripts/public-release-manifest.json")
	}

	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}

	var paragraph string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, specStableCodeMarker) {
			paragraph = line
			break
		}
	}
	if paragraph == "" {
		t.Fatalf("%s: no line starts with %q", specPath, specStableCodeMarker)
	}

	idx := strings.Index(paragraph, specStableCodeTerminator)
	if idx < 0 {
		t.Fatalf("%s: stable-code paragraph does not contain %q", specPath, specStableCodeTerminator)
	}

	inSpec := map[string]bool{}
	for _, match := range backtickedCode.FindAllStringSubmatch(paragraph[:idx], -1) {
		if inSpec[match[1]] {
			t.Errorf("%s lists %s twice in the stable-code enumeration", specPath, match[1])
		}
		inSpec[match[1]] = true
	}

	constants := errorCodeConstants(t)
	inCode := map[string]bool{}
	for _, value := range constants {
		inCode[value] = true
	}

	var missingFromSpec, missingFromCode []string
	for code := range inCode {
		if !inSpec[code] {
			missingFromSpec = append(missingFromSpec, code)
		}
	}
	for code := range inSpec {
		if !inCode[code] {
			missingFromCode = append(missingFromCode, code)
		}
	}
	sort.Strings(missingFromSpec)
	sort.Strings(missingFromCode)

	if len(missingFromSpec) > 0 {
		t.Errorf("codes in %s but not in the spec enumeration: %v\nadd them to the %q paragraph with an envelope description",
			errorsGoFile, missingFromSpec, specStableCodeMarker)
	}
	if len(missingFromCode) > 0 {
		t.Errorf("codes in the spec enumeration but not in %s: %v\ndefine the constant or drop it from the spec",
			errorsGoFile, missingFromCode)
	}
}

// TestErrorCodeHeaderCountIsCurrent pins the count in the const block's header
// comment. It said 28 while 32 constants were declared. This test needs no spec
// file, so it also guards the public export.
func TestErrorCodeHeaderCountIsCurrent(t *testing.T) {
	raw, err := os.ReadFile(errorsGoFile)
	if err != nil {
		t.Fatalf("read %s: %v", errorsGoFile, err)
	}

	match := headerCountLine.FindSubmatch(raw)
	if match == nil {
		t.Fatalf("%s: no %q header comment", errorsGoFile, "// All N stable runtime error codes")
	}

	declared, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatalf("parse header count %q: %v", match[1], err)
	}

	constants := errorCodeConstants(t)
	if declared != len(constants) {
		t.Errorf("header comment says %d stable runtime error codes; %s declares %d",
			declared, errorsGoFile, len(constants))
	}

	values := map[string][]string{}
	for name, value := range constants {
		values[value] = append(values[value], name)
	}
	for value, names := range values {
		if len(names) > 1 {
			sort.Strings(names)
			t.Errorf("error code %q is declared by %d constants: %v", value, len(names), names)
		}
	}
}
