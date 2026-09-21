package sembr

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// TestCorpus applies the analyzer to the Go standard library,
// which is large and hand-written
// but makes no attempt to follow the specification,
// so it exercises the checks against realistic prose.
//
// For every file it asserts the properties a rewrite must have:
// the result still parses,
// godoc renders it the same,
// and a second run finds nothing further to fix.
func TestCorpus(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping corpus test in short mode")
	}
	root := filepath.Join(goroot(t), "src")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("no standard library source at %s", root)
	}

	var files, fixed int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil //nolint:nilerr // unreadable directories are not this test's concern
		case d.IsDir() && (d.Name() == "testdata" || d.Name() == "vendor"):
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(path, ".go"):
			return nil
		}
		src, err := os.ReadFile(path) //nolint:gosec // the path comes from walking GOROOT
		if err != nil {
			return nil //nolint:nilerr // an unreadable file is not this test's concern
		}
		files++
		fixed += checkRewrite(t, path, string(src))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("no source files examined")
	}
	t.Logf("examined %d files, applied %d fixes", files, fixed)
}

// goroot returns the root of the Go installation
// that the go command on PATH belongs to.
func goroot(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output()
	if err != nil {
		t.Skipf("go env GOROOT: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// checkRewrite analyses src, applies the suggested fixes
// and verifies that the result is equivalent.
// It returns the number of fixes applied.
//
// A line break inserted between sentences
// must leave the rendered comment byte for byte the same.
// Joining a hyphenated word deliberately removes one space,
// so for those the comparison ignores whitespace.
func checkRewrite(t *testing.T, path, src string) int {
	t.Helper()
	before, edits := analyse(t, path, src)
	if len(edits.sentence)+len(edits.hyphen) == 0 {
		return 0
	}

	if len(edits.sentence) > 0 {
		after, _ := analyse(t, path, rewrite(t, path, src, edits.sentence))
		compare(t, path, "inserting line breaks", before, after, identity)
	}

	all := append(append([]analysis.TextEdit(nil), edits.sentence...), edits.hyphen...)
	fixed := rewrite(t, path, src, all)
	after, again := analyse(t, path, fixed)
	compare(t, path, "fixing", before, after, unspaced)
	if n := len(again.sentence) + len(again.hyphen); n != 0 {
		t.Errorf("%s: %d violations remain after fixing", path, n)
	}
	checkFormat(t, path, src, fixed)
	return len(all)
}

// checkFormat verifies that a file gofmt left alone before the rewrite
// is still left alone after it,
// so that the analyzer and the formatter do not disagree
// about how a comment should look.
func checkFormat(t *testing.T, path, src, fixed string) {
	t.Helper()
	before, err := format.Source([]byte(src))
	if err != nil || string(before) != src {
		return // not formatted to begin with
	}
	after, err := format.Source([]byte(fixed))
	if err != nil {
		t.Errorf("%s: fixed source does not format: %v", path, err)
		return
	}
	if string(after) != fixed {
		t.Errorf("%s: gofmt would reformat the fixed source", path)
	}
}

func compare(t *testing.T, path, what string, before, after []string, norm func(string) string) {
	t.Helper()
	if len(before) != len(after) {
		t.Errorf("%s: %d comment groups before %s, %d after", path, len(before), what, len(after))
		return
	}
	for i := range before {
		if norm(before[i]) != norm(after[i]) {
			t.Errorf("%s: %s changed a comment:\n before %q\n  after %q",
				path, what, before[i], after[i])
			return
		}
	}
}

func identity(s string) string { return s }

// unspaced removes all whitespace,
// so that two renderings differing only in where the text is broken compare equal.
func unspaced(s string) string { return strings.Join(strings.Fields(s), "") }

// edits are the fixes the analyzer suggests, kept apart by what they do.
type edits struct {
	sentence []analysis.TextEdit
	hyphen   []analysis.TextEdit
}

// analyse renders every comment group of src as godoc would
// and collects the fixes the analyzer suggests.
func analyse(t *testing.T, path, src string) ([]string, edits) {
	t.Helper()
	fset, f := parse(t, path, src)

	rendered := make([]string, 0, len(f.Comments))
	for _, g := range f.Comments {
		rendered = append(rendered, render(strings.Split(g.Text(), "\n")))
	}

	var got edits
	pass := &analysis.Pass{
		Analyzer: Analyzer,
		Fset:     fset,
		Files:    []*ast.File{f},
		ReadFile: func(string) ([]byte, error) { return []byte(src), nil },
		Report: func(d analysis.Diagnostic) {
			for _, fix := range d.SuggestedFixes {
				if d.Category == "hyphen" {
					got.hyphen = append(got.hyphen, fix.TextEdits...)
				} else {
					got.sentence = append(got.sentence, fix.TextEdits...)
				}
			}
		},
	}
	checkFile(pass, newScanner(nil, false), f)
	return rendered, got
}

func rewrite(t *testing.T, path, src string, edits []analysis.TextEdit) string {
	t.Helper()
	fset, f := parse(t, path, src)
	return apply(t, src, fset.File(f.FileStart).Base(), edits)
}

func parse(t *testing.T, path, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("%s: does not parse: %v", path, err)
	}
	return fset, f
}
