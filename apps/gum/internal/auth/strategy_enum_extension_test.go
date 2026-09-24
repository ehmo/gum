package auth

// The spec §7 "`auth_strategy` enum extension procedure" gate
// (docs/test-matrix.md, bead gum-codx).
//
// §7 says a value added to the enum MUST land six things in one PR.
// Three of them leave an artifact this test can see: the value in
// docs/catalog-abi.md's auth_strategy cross-reference (step 2), a
// strategy_<name>.go file under internal/auth (step 3), and a
// docs/test-matrix.md row naming TestAuthStrategy<Name> (step 5). The gate
// subtracts the declared baseline, which pre-existing per-strategy fixtures
// already cover, and applies (a)/(b)/(c) to whatever is left.
//
// The residual set is empty today, so the checker itself is proved against
// fixtures instead: TestResidualStrategyChecksReportEveryMissingStep feeds it
// a value no PR ever landed, and TestResidualStrategyChecksPassOnACompletePR
// feeds it a value whose three artifacts all exist.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// baselineStrategies is the auth_strategy set already covered, enumerated
// here rather than derived so the gate is self-bootstrapping: dropping a name
// re-arms (a)/(b)/(c) against it, and adding a constant to strategy.go without
// adding it here arms them immediately.
//
// Names are Strategy.String() spellings, so the catalog-ABI alias
// "service_account" (catalog.AuthStrategyServiceAccount, folded onto
// StrategyServiceAccountKey by strategyFromCatalog) is absent and
// "service_account_key" stands for both.
//
// workload_identity and impersonation used to sit here. They shipped in the
// Go enum but never in the catalog-ABI wire enum, so a variant declaring one
// passed Validate at build and install and failed only at dispatch. gum-z5yx
// removed both constants; re-adding either now arms (a)/(b)/(c) against it,
// which is what §7's extension procedure asks for.
var baselineStrategies = map[string]bool{
	"gum_oauth":           true,
	"byo_oauth":           true,
	"adc":                 true,
	"api_key":             true,
	"service_account_key": true,
	"none":                true,
	"compound":            true,
	"plugin_managed":      true,
}

func TestAuthStrategyEnumExtensionComplete(t *testing.T) {
	root := findRepoRootForTest(t)
	docs := docsDirForTest(t, root)
	authDir := filepath.Join(root, "internal", "auth")

	values := declaredStrategyValues(t, root)

	declared := make(map[string]bool, len(values))
	for _, v := range values {
		declared[v] = true
	}
	for name := range baselineStrategies {
		if !declared[name] {
			t.Errorf("baseline names %q but internal/auth/strategy.go no longer declares it; drop it from baselineStrategies", name)
		}
	}

	for _, value := range values {
		if baselineStrategies[value] {
			continue
		}
		problems, err := residualStrategyViolations(docs, authDir, value)
		if err != nil {
			t.Fatalf("checking %q: %v", value, err)
		}
		for _, p := range problems {
			t.Errorf("auth_strategy %q is outside the declared baseline, so spec §7's extension procedure applies: %s", value, p)
		}
	}
}

// declaredStrategyValues returns the canonical name of every Strategy constant
// in iota order. It parses the const block instead of probing String() upward,
// because a constant added without a String() case reports "unknown" and would
// otherwise be invisible to this gate.
func declaredStrategyValues(t *testing.T, root string) []string {
	t.Helper()

	names := strategyConstNames(t, filepath.Join(root, "internal", "auth", "strategy.go"))
	if len(names) == 0 {
		t.Fatal("internal/auth/strategy.go declares no Strategy constants")
	}

	values := make([]string, 0, len(names))
	for i, name := range names {
		got := Strategy(i).String()
		if got == "unknown" {
			t.Errorf("constant %s has no Strategy.String() case, so no surface can name it", name)
			continue
		}
		values = append(values, got)
	}

	if extra := Strategy(len(names)).String(); extra != "unknown" {
		t.Errorf("Strategy(%d).String() = %q but strategy.go declares %d constants; String() carries a case with no constant behind it", len(names), extra, len(names))
	}
	return values
}

func strategyConstNames(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST || len(gen.Specs) == 0 {
			continue
		}
		first, ok := gen.Specs[0].(*ast.ValueSpec)
		if !ok {
			continue
		}
		ident, ok := first.Type.(*ast.Ident)
		if !ok || ident.Name != "Strategy" {
			continue
		}

		var names []string
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range vs.Names {
				names = append(names, n.Name)
			}
		}
		return names
	}
	return nil
}

// residualStrategyViolations reports which of spec §7's three observable
// extension steps a strategy value is missing. It takes the two directories as
// arguments so the fixture tests can point it at a temp tree.
func residualStrategyViolations(docsDir, authDir, value string) ([]string, error) {
	var problems []string

	abiPath := filepath.Join(docsDir, "catalog-abi.md")
	abi, err := os.ReadFile(abiPath)
	if err != nil {
		return nil, err
	}
	xref := authStrategyCrossReference(string(abi))
	if xref == "" {
		return nil, fmt.Errorf("%s has no auth_strategy cross-reference bullet", abiPath)
	}
	if !strings.Contains(xref, "`"+value+"`") {
		problems = append(problems, "(a) docs/catalog-abi.md's auth_strategy cross-reference does not list it")
	}

	strategyFile := filepath.Join(authDir, "strategy_"+value+".go")
	if _, err := os.Stat(strategyFile); err != nil {
		problems = append(problems, "(b) internal/auth/strategy_"+value+".go does not exist, so the resolution path is not behind a strategy-specific file")
	}

	matrixPath := filepath.Join(docsDir, "test-matrix.md")
	matrix, err := os.ReadFile(matrixPath)
	if err != nil {
		return nil, err
	}
	want := "TestAuthStrategy" + camelCaseStrategy(value)
	if !regexp.MustCompile(`\b` + regexp.QuoteMeta(want) + `\b`).Match(matrix) {
		problems = append(problems, "(c) no docs/test-matrix.md row names "+want)
	}

	return problems, nil
}

// authStrategyCrossReference returns the one markdown bullet catalog-abi.md
// devotes to auth_strategy. Scoping the (a) check to that bullet keeps a
// passing mention elsewhere in the document from satisfying it.
func authStrategyCrossReference(body string) string {
	const marker = "- `auth_strategy`"

	start := strings.Index(body, marker)
	if start < 0 {
		return ""
	}
	rest := body[start+len(marker):]
	if end := strings.Index(rest, "\n- "); end >= 0 {
		return body[start : start+len(marker)+end]
	}
	return body[start:]
}

func camelCaseStrategy(value string) string {
	parts := strings.Split(value, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// docsDirForTest resolves docs/ from the module root. The module lives at
// apps/gum, the documents it is checked against live at the repo root.
func docsDirForTest(t *testing.T, moduleRoot string) string {
	t.Helper()

	dir := filepath.Clean(filepath.Join(moduleRoot, "..", "..", "docs"))
	if _, err := os.Stat(filepath.Join(dir, "catalog-abi.md")); err != nil {
		t.Fatalf("docs directory not found at %s: %v", dir, err)
	}
	return dir
}

// TestResidualStrategyChecksReportEveryMissingStep proves the checker against
// a value no PR ever landed. The residual set is empty while the baseline
// covers the whole enum, so without this fixture
// TestAuthStrategyEnumExtensionComplete would pass whether or not its checks
// work.
func TestResidualStrategyChecksReportEveryMissingStep(t *testing.T) {
	root := findRepoRootForTest(t)
	docs := docsDirForTest(t, root)

	problems, err := residualStrategyViolations(docs, filepath.Join(root, "internal", "auth"), "workforce_pool")
	if err != nil {
		t.Fatalf("residualStrategyViolations: %v", err)
	}

	want := []string{
		"docs/catalog-abi.md",
		"strategy_workforce_pool.go",
		"TestAuthStrategyWorkforcePool",
	}
	if len(problems) != len(want) {
		t.Fatalf("got %d problems %q, want one per step: %q", len(problems), problems, want)
	}
	for i, substr := range want {
		if !strings.Contains(problems[i], substr) {
			t.Errorf("problem %d = %q, want it to name %q", i, problems[i], substr)
		}
	}
}

// TestResidualStrategyChecksPassOnACompletePR is the positive control: a
// fixture tree carrying all three artifacts reports nothing, so the checker is
// not simply always-fail.
func TestResidualStrategyChecksPassOnACompletePR(t *testing.T) {
	const value = "workforce_pool"

	docs := t.TempDir()
	authDir := t.TempDir()

	writeFixture(t, filepath.Join(docs, "catalog-abi.md"),
		"- `output_profile` names a profile.\n"+
			"- `auth_strategy` uses the closed enum (`adc`, `"+value+"`).\n"+
			"- `capabilities[]` is closed.\n")
	writeFixture(t, filepath.Join(docs, "test-matrix.md"),
		"| A successful call, an unconfigured failure, and the stdio rejection | `TestAuthStrategyWorkforcePool` | v0.2 CI |\n")
	writeFixture(t, filepath.Join(authDir, "strategy_"+value+".go"), "package auth\n")

	problems, err := residualStrategyViolations(docs, authDir, value)
	if err != nil {
		t.Fatalf("residualStrategyViolations: %v", err)
	}
	if len(problems) != 0 {
		t.Errorf("complete extension reported %q; want none", problems)
	}
}

// TestAuthStrategyCrossReferenceIsScopedToItsBullet pins the (a) check to the
// one bullet catalog-abi.md gives auth_strategy. A value mentioned anywhere
// else in the document must not satisfy step 2.
func TestAuthStrategyCrossReferenceIsScopedToItsBullet(t *testing.T) {
	docs := t.TempDir()
	authDir := t.TempDir()

	writeFixture(t, filepath.Join(docs, "catalog-abi.md"),
		"- `auth_strategy` uses the closed enum (`adc`).\n"+
			"- `capabilities[]` is closed; see `workforce_pool` elsewhere.\n")
	writeFixture(t, filepath.Join(docs, "test-matrix.md"), "| row | `TestAuthStrategyWorkforcePool` | v0.2 CI |\n")
	writeFixture(t, filepath.Join(authDir, "strategy_workforce_pool.go"), "package auth\n")

	problems, err := residualStrategyViolations(docs, authDir, "workforce_pool")
	if err != nil {
		t.Fatalf("residualStrategyViolations: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "(a)") {
		t.Errorf("got %q; want only the (a) cross-reference problem", problems)
	}
}

func writeFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
