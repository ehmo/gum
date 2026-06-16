// Package testmatrix parses docs/test-matrix.md into named groups of
// Go test function names and runs each group as a release-gate proof.
//
// Spec source of truth: docs/test-matrix.md (groups A-L, normative).
// Bead: gum-b22o.1.
package testmatrix

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Group is one named matrix section (e.g. "A: Tier A budget and schema")
// containing the unique set of Go test function names listed in that
// section's "Proof artifact" column.
type Group struct {
	Letter      string   // "A", "B", ...
	Description string   // section description text after the colon
	Tests       []string // sorted, deduplicated Go test function names
}

var (
	// groupHeaderRe matches the HTML comment that separates matrix sections,
	// e.g. `<!-- Group A: Tier A budget and schema -->`. Capture 1 = letter,
	// capture 2 = description.
	groupHeaderRe = regexp.MustCompile(`^\s*<!--\s*Group\s+([A-Z]):\s*(.+?)\s*-->\s*$`)

	// proofTestRe matches a backticked Go test identifier (`TestFoo`,
	// `FuzzFoo`, or `TestFoo<Name>` template placeholders) inside a markdown
	// table cell. Template placeholders containing angle brackets are
	// filtered out by isRunnableTestName.
	proofTestRe = regexp.MustCompile("`((?:Test|Fuzz)[A-Za-z0-9_<>]+)`")
)

// ParseFile reads path, parses it as docs/test-matrix.md, and returns the
// matrix groups in source order.
func ParseFile(path string) ([]Group, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("testmatrix: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return Parse(f)
}

// Parse reads the matrix from r and returns groups in source order.
//
// Rows appearing before the first group header are dropped; tests appearing
// outside any group are not assigned to a group. Tests with template
// placeholders (`TestBackendKind<Name>`) are skipped because they describe
// the contract for future PRs, not actual functions to invoke.
func Parse(r io.Reader) ([]Group, error) {
	scanner := bufio.NewScanner(r)
	// Matrix rows can be very long (single-row paragraphs); raise the line buffer.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var groups []Group
	current := -1
	seen := map[string]map[string]bool{} // group-letter -> test-name -> present

	for scanner.Scan() {
		line := scanner.Text()
		if m := groupHeaderRe.FindStringSubmatch(line); m != nil {
			letter, desc := m[1], m[2]
			groups = append(groups, Group{Letter: letter, Description: desc})
			current = len(groups) - 1
			seen[letter] = map[string]bool{}
			continue
		}
		if current < 0 {
			continue
		}
		for _, sub := range proofTestRe.FindAllStringSubmatch(line, -1) {
			name := sub[1]
			if !isRunnableTestName(name) {
				continue
			}
			letter := groups[current].Letter
			if !seen[letter][name] {
				seen[letter][name] = true
				groups[current].Tests = append(groups[current].Tests, name)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("testmatrix: scan: %w", err)
	}
	for i := range groups {
		sort.Strings(groups[i].Tests)
	}
	return groups, nil
}

// isRunnableTestName reports whether name is a runnable Go test identifier
// rather than a template placeholder. Templates like `TestBackendKind<Name>`
// describe a contract for future PRs, not a function to invoke, and are
// excluded from the runnable set.
func isRunnableTestName(name string) bool {
	if strings.ContainsAny(name, "<>") {
		return false
	}
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz")
}
