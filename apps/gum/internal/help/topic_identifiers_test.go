package help_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// identifierPattern matches a backticked SNAKE_CASE token in a help topic:
// two or more uppercase segments joined by underscores. That shape covers the
// runtime error codes and environment variables the topics quote, and excludes
// ordinary prose, field names, and command examples.
var identifierPattern = regexp.MustCompile("`([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+)`")

// scannerFile is this test's own file name, held out of the corpus below.
const scannerFile = "topic_identifiers_test.go"

// scannedExtensions are the file types that can define an identifier: Go
// source for what the runtime emits, Markdown for what the normative contracts
// name, and the data formats the catalog and CI configuration use.
var scannedExtensions = map[string]bool{
	".go": true, ".md": true, ".json": true, ".toml": true,
	".yml": true, ".yaml": true, ".txt": true,
}

// TestHelpTopicIdentifiersExist fails when a help topic quotes an identifier
// that appears nowhere else in the repository.
//
// Help topics are served to model clients as gum://help/<topic>, so an
// invented error code teaches a caller to branch on a string the runtime never
// emits. The caller's error path then dead-codes silently. A token that exists
// only inside internal/help/topics was written from imagination, because a
// real code is either emitted by Go source or named by a docs/ contract.
//
// Passing here is not proof that the surrounding prose is accurate. It only
// bounds the cheapest and most damaging fabrication: the identifier itself.
func TestHelpTopicIdentifiersExist(t *testing.T) {
	root := repoRoot(t)
	topicDir := filepath.Join(root, "apps", "gum", "internal", "help", "topics")

	quoted := map[string][]string{}
	entries, err := os.ReadDir(topicDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(topicDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range identifierPattern.FindAllStringSubmatch(string(b), -1) {
			quoted[m[1]] = appendUnique(quoted[m[1]], e.Name())
		}
	}
	if len(quoted) == 0 {
		t.Fatal("no identifiers found; the scan or the topic directory is wrong")
	}

	corpus := repoCorpus(t, root, topicDir)
	names := make([]string, 0, len(quoted))
	for name := range quoted {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		// Word boundaries keep a short token from matching inside a longer
		// one, so a bare suffix is not satisfied by the full code that ends
		// with it.
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(corpus) {
			continue
		}
		t.Errorf("help topic %v quotes %s, which exists nowhere else in the repo", quoted[name], name)
	}
}

// repoCorpus concatenates every scannable file outside the help topics. The
// topics are excluded so that one topic cannot vouch for another's invention.
func repoCorpus(t *testing.T, root, topicDir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "dist", "vendor", "node_modules":
				return filepath.SkipDir
			}
			if path == topicDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !scannedExtensions[filepath.Ext(path)] {
			return nil
		}
		// This file names identifiers while describing the check. Letting it
		// into the corpus would make the guard vouch for its own examples.
		if d.Name() == scannerFile {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func appendUnique(list []string, v string) []string {
	for _, existing := range list {
		if existing == v {
			return list
		}
	}
	return append(list, v)
}

// repoRoot walks up from the test's directory to the directory holding the
// docs/ contracts, which is the parent of the Go module.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "docs", "spec.md")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("repo root with docs/spec.md not found")
		}
		dir = next
	}
}
