package securityscan

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestTestdataNoProductionImport asserts that no production .go file (i.e.
// non-_test.go) imports any package whose import path contains a "/testdata/"
// segment. Required by docs/test-matrix.md row 16 and spec §4 Tier A
// guarantee: testdata helpers cannot contribute to the production Tier A
// surface or budget.
func TestTestdataNoProductionImport(t *testing.T) {
	root := moduleRoot(t)

	type leak struct {
		File   string
		Import string
	}
	var leaks []leak

	fset := token.NewFileSet()
	skipDirs := map[string]bool{
		".git":      true,
		"node_modules": true,
		"testdata":  true,
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}
		// Production files only: .go files, not _test.go, and not under any
		// testdata/ ancestor.
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.Contains(rel, string(filepath.Separator)+"testdata"+string(filepath.Separator)) ||
			strings.HasPrefix(rel, "testdata"+string(filepath.Separator)) {
			return nil
		}

		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil
		}
		for _, imp := range f.Imports {
			val := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(val, "/testdata/") || strings.HasSuffix(val, "/testdata") {
				leaks = append(leaks, leak{File: rel, Import: val})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}

	if len(leaks) > 0 {
		sort.Slice(leaks, func(i, j int) bool { return leaks[i].File < leaks[j].File })
		var msgs []string
		for _, l := range leaks {
			msgs = append(msgs, l.File+" imports "+l.Import)
		}
		t.Fatalf("production code imports testdata package (spec §4 Tier A budget violation):\n  %s",
			strings.Join(msgs, "\n  "))
	}
}
