// Package sembr provides an analysis pass
// that checks Go comments for semantic line breaks
// as described by https://sembr.org.
//
// Only the specification's absolute requirements are enforced.
// A line break MUST follow a sentence,
// and MUST NOT fall inside a hyphenated word.
// The rules governing clause boundaries are recommendations
// that cannot be decided without understanding the prose,
// and are left to the author.
package sembr

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
)

const doc = `check comments for semantic line breaks

A semantic line break ends a line at a boundary in the prose rather than at a
fixed column, so that edits to one sentence leave the surrounding lines
untouched. This analyzer reports the two cases the specification at
https://sembr.org states as requirements: a sentence that is followed by more
text on the same line, and a hyphenated word split across two lines.

Comments trailing code on the same line are not reported, because they cannot
be broken without moving them relative to the code. Code blocks, headings and
comment directives are not prose and are skipped.`

var (
	strict bool
	abbrev string
)

// Analyzer checks comments for semantic line breaks.
var Analyzer = &analysis.Analyzer{
	Name: "sembr",
	Doc:  doc,
	URL:  "https://github.com/thde/sembr",
	Run:  run,
}

func init() {
	Analyzer.Flags.BoolVar(&strict, "strict", false,
		"report a sentence boundary even when the following word is not capitalised")
	Analyzer.Flags.StringVar(&abbrev, "abbrev", "",
		"comma-separated abbreviations whose trailing period does not end a sentence")
}

func run(pass *analysis.Pass) (any, error) {
	sc := newScanner(strings.Split(abbrev, ","), strict)
	for _, f := range pass.Files {
		checkFile(pass, sc, f)
	}
	return nil, nil
}

// checkFile reports every violation in the comments of one file.
// Generated files are skipped:
// their comments are not written by hand.
func checkFile(pass *analysis.Pass, sc *scanner, f *ast.File) {
	if ast.IsGenerated(f) {
		return
	}
	tf := pass.Fset.File(f.FileStart)
	if tf == nil {
		return
	}
	src, err := readFile(pass, tf.Name())
	if err != nil || len(src) != tf.Size() {
		// The contents differ from what was parsed,
		// so no offset into them can be trusted.
		return
	}
	eol := "\n"
	if bytes.Contains(src, []byte("\r\n")) {
		eol = "\r\n"
	}
	output := exampleOutputs(f)
	base := token.Pos(tf.Base())
	for _, g := range f.Comments {
		if !output[g] && standalone(g, src, base) {
			checkGroup(pass, sc, readLines(g, src, base), eol)
		}
	}
}

func readFile(pass *analysis.Pass, name string) ([]byte, error) {
	read := os.ReadFile
	if pass.ReadFile != nil {
		read = pass.ReadFile
	}
	src, err := read(name)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	return src, nil
}

// outputPrefix matches the comment that introduces
// the expected output of a testable example,
// as recognised by go/doc.
var outputPrefix = regexp.MustCompile(`(?i)^\s*(unordered )?output:`)

// exampleOutputs returns the comment groups
// that hold the expected output of a testable example,
// identified as go/doc does:
// the last comment group in the body of an Example function,
// when it begins with an Output: marker.
// Rewriting one would change what the example asserts.
func exampleOutputs(f *ast.File) map[*ast.CommentGroup]bool {
	var out map[*ast.CommentGroup]bool
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil || len(fn.Type.Params.List) != 0 || !isTest(fn.Name.Name, "Example") {
			continue
		}
		var last *ast.CommentGroup
		for _, g := range f.Comments {
			if fn.Body.Pos() < g.Pos() && g.End() <= fn.Body.End() {
				last = g
			}
		}
		if last != nil && outputPrefix.MatchString(last.Text()) {
			if out == nil {
				out = make(map[*ast.CommentGroup]bool)
			}
			out[last] = true
		}
	}
	return out
}

// isTest reports whether name looks like a test, benchmark or example function name
// with the given prefix.
// It mirrors the unexported go/doc function of the same name.
func isTest(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if len(name) == len(prefix) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(r)
}

// checkGroup reports every violation in a single comment group.
// eol is the line ending an inserted line should use.
func checkGroup(pass *analysis.Pass, sc *scanner, lines []line, eol string) {
	if len(lines) == 0 {
		return
	}
	orig := texts(lines)
	for _, run := range proseRuns(lines) {
		for i, l := range run {
			for _, s := range sc.sites(l.text, l.off) {
				reportSentence(pass, orig, l, s, eol)
			}
			if i+1 < len(run) && splitsWord(pass.Fset, l.line, run[i+1].line) {
				reportHyphen(pass, orig, l, run[i+1])
			}
		}
	}
}

// splitsWord reports whether a hyphenated word is split
// between a and the line b that follows it.
// The two must be adjacent in the source:
// joining them across a dropped directive comment would delete it.
func splitsWord(fset *token.FileSet, a, b line) bool {
	return hyphenated(a.text, b.body()) && adjacent(fset, a, b)
}

func reportSentence(pass *analysis.Pass, orig []string, l proseLine, s site, eol string) {
	d := analysis.Diagnostic{
		Pos:      l.pos + token.Pos(s.punct),
		End:      l.pos + token.Pos(s.punct+1),
		Category: "sentence",
		Message:  "missing line break after sentence",
	}
	if mod := split(orig, l.idx, s, l.brk); unchanged(orig, mod) {
		d.SuggestedFixes = []analysis.SuggestedFix{{
			Message: "insert line break",
			TextEdits: []analysis.TextEdit{{
				Pos:     l.pos + token.Pos(s.from),
				End:     l.pos + token.Pos(s.to),
				NewText: []byte(eol + l.indent + l.brk),
			}},
		}}
	}
	pass.Report(d)
}

func reportHyphen(pass *analysis.Pass, orig []string, a, b proseLine) {
	d := analysis.Diagnostic{
		Pos:      a.end() - 1,
		End:      a.end(),
		Category: "hyphen",
		Message:  "line break inside hyphenated word",
	}
	body := b.body()
	if mod := join(orig, a.idx, body); sameStructure(orig, mod) {
		d.SuggestedFixes = []analysis.SuggestedFix{{
			Message: "join hyphenated word",
			TextEdits: []analysis.TextEdit{{
				Pos: a.end(),
				End: b.end() - token.Pos(len(body)),
			}},
		}}
	}
	pass.Report(d)
}
