package sembr

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// result is the outcome of analysing a single source file.
type result struct {
	diags []string // "line:col: message", in source order
	fixed string   // src with every suggested fix applied
}

// analyze runs the analyzer over src without a driver.
// The analyzer needs only the syntax tree and the file contents,
// so a pass can be assembled directly.
func analyze(t *testing.T, src string, opts ...func(*scanner)) result {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "a.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing source: %v", err)
	}

	sc := newScanner(nil, false)
	for _, opt := range opts {
		opt(sc)
	}

	var got result
	var edits []analysis.TextEdit
	pass := &analysis.Pass{
		Analyzer: Analyzer,
		Fset:     fset,
		Files:    []*ast.File{f},
		ReadFile: func(string) ([]byte, error) { return []byte(src), nil },
		Report: func(d analysis.Diagnostic) {
			p := fset.Position(d.Pos)
			got.diags = append(got.diags, fmt.Sprintf("%d:%d: %s", p.Line, p.Column, d.Message))
			for _, fix := range d.SuggestedFixes {
				edits = append(edits, fix.TextEdits...)
			}
		},
	}

	for _, file := range pass.Files {
		checkFile(pass, sc, file)
	}

	got.fixed = apply(t, src, fset.File(f.FileStart).Base(), edits)
	return got
}

// apply rewrites src with the given edits, which must not overlap.
func apply(t *testing.T, src string, base int, edits []analysis.TextEdit) string {
	t.Helper()
	slices.SortFunc(edits, func(a, b analysis.TextEdit) int { return int(a.Pos - b.Pos) })

	var b strings.Builder
	prev := 0
	for _, e := range edits {
		start, end := int(e.Pos)-base, int(e.End)-base
		if start < prev {
			t.Fatalf("overlapping edits at offset %d", start)
		}
		b.WriteString(src[prev:start])
		b.Write(e.NewText)
		prev = end
	}
	b.WriteString(src[prev:])
	return b.String()
}

func strictMode(sc *scanner) { sc.strict = true }

func extraAbbrev(words ...string) func(*scanner) {
	return func(sc *scanner) {
		for _, w := range words {
			sc.abbrev[w] = true
		}
	}
}

// wrap puts a comment in front of a declaration so it forms a valid file.
func wrap(comment string) string {
	return "package a\n\n" + comment + "\nfunc F() {}\n"
}

func TestSentences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		src   string
		want  []string
		fixed string
	}{{
		name:  "two sentences",
		src:   wrap("// F does a thing. It does another."),
		want:  []string{"3:18: missing line break after sentence"},
		fixed: wrap("// F does a thing.\n// It does another."),
	}, {
		name: "three sentences",
		src:  wrap("// One. Two. Three."),
		want: []string{
			"3:7: missing line break after sentence",
			"3:12: missing line break after sentence",
		},
		fixed: wrap("// One.\n// Two.\n// Three."),
	}, {
		name: "sentence per line is clean",
		src:  wrap("// F does a thing.\n// It does another."),
	}, {
		name: "exclamation and question marks",
		src:  wrap("// Careful! This panics. Why? Because it does."),
		want: []string{
			"3:11: missing line break after sentence",
			"3:24: missing line break after sentence",
			"3:29: missing line break after sentence",
		},
		fixed: wrap("// Careful!\n// This panics.\n// Why?\n// Because it does."),
	}, {
		name:  "indented comment keeps its indentation",
		src:   "package a\n\nfunc F() {\n\t// One. Two.\n}\n",
		want:  []string{"4:8: missing line break after sentence"},
		fixed: "package a\n\nfunc F() {\n\t// One.\n\t// Two.\n}\n",
	}, {
		name:  "no space after the comment marker",
		src:   wrap("//One. Two."),
		want:  []string{"3:6: missing line break after sentence"},
		fixed: wrap("//One.\n//Two."),
	}, {
		name:  "qualified identifier ends a sentence",
		src:   wrap("// F returns io.EOF. Otherwise it returns nil."),
		want:  []string{"3:20: missing line break after sentence"},
		fixed: wrap("// F returns io.EOF.\n// Otherwise it returns nil."),
	}, {
		name:  "version ends a sentence",
		src:   wrap("// F was added in v1.2.3. Use it freely."),
		want:  []string{"3:25: missing line break after sentence"},
		fixed: wrap("// F was added in v1.2.3.\n// Use it freely."),
	}, {
		name:  "host name ends a sentence",
		src:   wrap("// See golang.org/x/tools. It has more."),
		want:  []string{"3:26: missing line break after sentence"},
		fixed: wrap("// See golang.org/x/tools.\n// It has more."),
	}, {
		name:  "number ends a sentence",
		src:   wrap("// F returns 0. Otherwise it returns the count."),
		want:  []string{"3:15: missing line break after sentence"},
		fixed: wrap("// F returns 0.\n// Otherwise it returns the count."),
	}, {
		name:  "type parameter ends a sentence",
		src:   wrap("// F works on type T. It is generic."),
		want:  []string{"3:21: missing line break after sentence"},
		fixed: wrap("// F works on type T.\n// It is generic."),
	}, {
		name:  "CRLF line comment",
		src:   "package a\r\n\r\n// One. Two.\r\nfunc F() {}\r\n",
		want:  []string{"3:7: missing line break after sentence"},
		fixed: "package a\r\n\r\n// One.\r\n// Two.\r\nfunc F() {}\r\n",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := analyze(t, tt.src)
			checkResult(t, got, tt.want, tt.fixed, tt.src)
		})
	}
}

func TestNotSentenceBoundaries(t *testing.T) {
	t.Parallel()
	clean := []struct{ name, src string }{
		{"qualified identifier", wrap("// F returns an io.Reader for the data.")},
		{"e.g.", wrap("// F accepts a reader, e.g. bytes.Reader, and copies it.")},
		{"i.e. before a capital", wrap("// F copies the data, i.e. Everything in the buffer.")},
		{"etc.", wrap("// F handles files, sockets, etc. Nothing else matters.")},
		{"vs.", wrap("// F compares Foo vs. Bar and reports the winner.")},
		{"e.g. in brackets", wrap("// F accepts a reader (e.g. Reader) and copies it.")},
		{"etc. in brackets", wrap("// F handles files (sockets, etc.) Nothing else matters.")},
		{"U.S.", wrap("// F formats dates the U.S. Way by default.")},
		{"ellipsis", wrap("// F waits... Then it returns.")},
		{"decimal", wrap("// F sleeps for 0.5 seconds before returning.")},
		{"middle initial", wrap("// Ported from code by Stephen L. Moshier in 1984.")},
		{"series of initials", wrap("// Named after T. S. Eliot for no reason.")},
		{"lowercase continuation", wrap("// F does a thing. it also does another.")},
		{"sentence ends the line", wrap("// F does a thing.")},
		{"trailing whitespace", wrap("// F does a thing.   ")},
	}
	for _, tt := range clean {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := analyze(t, tt.src); len(got.diags) != 0 {
				t.Errorf("got %v, want no diagnostics", got.diags)
			}
		})
	}
}

func TestStrict(t *testing.T) {
	t.Parallel()
	src := wrap("// F does a thing. it also does another.")
	if got := analyze(t, src); len(got.diags) != 0 {
		t.Errorf("default mode: got %v, want no diagnostics", got.diags)
	}
	got := analyze(t, src, strictMode)
	checkResult(t, got,
		[]string{"3:18: missing line break after sentence"},
		wrap("// F does a thing.\n// it also does another."), src)
}

func TestAbbrev(t *testing.T) {
	t.Parallel()
	src := wrap("// F reads at most 10 ms. Then it gives up.")
	if got := analyze(t, src); len(got.diags) != 1 {
		t.Errorf("default mode: got %v, want one diagnostic", got.diags)
	}
	if got := analyze(t, src, extraAbbrev("ms")); len(got.diags) != 0 {
		t.Errorf("with -abbrev=ms: got %v, want no diagnostics", got.diags)
	}
}

// TestNewScanner checks that the -abbrev flag value is normalised
// the way the words in a comment are looked up:
// lowercase, without a trailing period.
func TestNewScanner(t *testing.T) {
	t.Parallel()
	sc := newScanner([]string{" Ms.", "REQ", "", "resp"}, false)
	for _, w := range []string{"ms", "req", "resp", "etc"} {
		if !sc.abbrev[w] {
			t.Errorf("abbrev[%q] = false, want true", w)
		}
	}
	for _, w := range []string{"", "Ms.", "REQ"} {
		if sc.abbrev[w] {
			t.Errorf("abbrev[%q] = true, want false", w)
		}
	}
	if sc.strict {
		t.Error("strict = true, want false")
	}
}

func TestSkipped(t *testing.T) {
	t.Parallel()
	clean := []struct{ name, src string }{
		{"trailing comment", "package a\n\nvar x = 1 // One. Two.\n"},
		{"code block", wrap("// F runs code.\n//\n//\tone. two.\n//")},
		{"heading", wrap("// # One. Two.")},
		{"directive", "package a\n\n//go:generate echo one. two.\nfunc F() {}\n"},
		{"generated file", "// Code generated by hand. DO NOT EDIT.\n\npackage a\n\n// One. Two.\nfunc F() {}\n"},
		{"example output", "package a\n\nfunc ExampleF() {\n\t// Output:\n\t// go-\n\t// go-gopher. Yes.\n}\n"},
		{"unordered example output", "package a\n\nfunc ExampleF() {\n\t// Unordered output:\n\t// One. Two.\n}\n"},
		{"hyphen across a directive", wrap("// F handles well-\n//go:generate echo hi\n// formed input.")},
	}
	for _, tt := range clean {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := analyze(t, tt.src); len(got.diags) != 0 {
				t.Errorf("got %v, want no diagnostics", got.diags)
			}
		})
	}
}

// TestExampleOutput checks that only the comment go/doc treats as expected output is skipped:
// the last one in an Example function's body.
func TestExampleOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		src   string
		want  []string
		fixed string
	}{{
		name:  "prose before the output",
		src:   "package a\n\nfunc ExampleF() {\n\t// One. Two.\n\tF()\n\t// Output:\n\t// go-\n\t// gopher. Yes.\n}\n",
		want:  []string{"4:8: missing line break after sentence"},
		fixed: "package a\n\nfunc ExampleF() {\n\t// One.\n\t// Two.\n\tF()\n\t// Output:\n\t// go-\n\t// gopher. Yes.\n}\n",
	}, {
		name:  "output marker outside an example",
		src:   "package a\n\nfunc F() {\n\t// Output: One. Two.\n}\n",
		want:  []string{"4:16: missing line break after sentence"},
		fixed: "package a\n\nfunc F() {\n\t// Output: One.\n\t// Two.\n}\n",
	}, {
		name:  "output marker not first in the comment",
		src:   "package a\n\nfunc ExampleF() {\n\t// One. Two.\n\t// Output: x\n}\n",
		want:  []string{"4:8: missing line break after sentence"},
		fixed: "package a\n\nfunc ExampleF() {\n\t// One.\n\t// Two.\n\t// Output: x\n}\n",
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkResult(t, analyze(t, tt.src), tt.want, tt.fixed, tt.src)
		})
	}
}

func TestLists(t *testing.T) {
	t.Parallel()
	src := wrap("// F has options:\n//\n//   - fast. it is quick.\n//   - safe. It is careful.\n//     And it continues. Here.\n//")
	got := analyze(t, src)
	want := []string{
		"6:12: missing line break after sentence",
		"7:24: missing line break after sentence",
	}
	fixed := wrap("// F has options:\n//\n//   - fast. it is quick.\n//   - safe.\n//     It is careful.\n//     And it continues.\n//     Here.\n//")
	checkResult(t, got, want, fixed, src)
}

func TestHyphen(t *testing.T) {
	t.Parallel()
	src := wrap("// F handles well-\n// formed input only.")
	got := analyze(t, src)
	checkResult(t, got,
		[]string{"3:18: line break inside hyphenated word"},
		wrap("// F handles well-formed input only."), src)
}

func TestHyphenNotSplit(t *testing.T) {
	t.Parallel()
	clean := []struct{ name, src string }{
		{"complete hyphenated word", wrap("// F handles well-formed input.\n// It is careful.")},
		{"dash at end of prose", wrap("// F handles input -\n// and more.")},
		{"flag name", wrap("// Pass -v to F.\n// It is verbose.")},
		{"url ending in a hyphen", wrap("// See https://example.com/what-is-a-uname-\n// Therefore F is careful.")},
	}
	for _, tt := range clean {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := analyze(t, tt.src); len(got.diags) != 0 {
				t.Errorf("got %v, want no diagnostics", got.diags)
			}
		})
	}
}

func TestBlockComment(t *testing.T) {
	t.Parallel()
	src := "package a\n\n/*\nF does a thing. It does another.\n*/\nfunc F() {}\n"
	got := analyze(t, src)
	checkResult(t, got,
		[]string{"4:15: missing line break after sentence"},
		"package a\n\n/*\nF does a thing.\nIt does another.\n*/\nfunc F() {}\n", src)
}

// TestCRLFBlockComment checks that positions inside a block comment stay correct
// when the file uses CRLF line endings,
// which the scanner strips from the comment's text.
func TestCRLFBlockComment(t *testing.T) {
	t.Parallel()
	src := "package a\r\n\r\n/*\r\nOne. Two.\r\nThree. Four.\r\n*/\r\nfunc F() {}\r\n"
	got := analyze(t, src)
	checkResult(t, got,
		[]string{
			"4:4: missing line break after sentence",
			"5:6: missing line break after sentence",
		},
		"package a\r\n\r\n/*\r\nOne.\r\nTwo.\r\nThree.\r\nFour.\r\n*/\r\nfunc F() {}\r\n", src)
}

// TestStarBlockHyphen checks that a hyphenated word
// split across the lines of an asterisk-decorated block comment
// is joined without the decoration of the second line.
func TestStarBlockHyphen(t *testing.T) {
	t.Parallel()
	src := "package a\n\n/*\n * F handles well-\n * formed input.\n */\nfunc F() {}\n"
	got := analyze(t, src)
	checkResult(t, got, []string{"4:18: line break inside hyphenated word"},
		"package a\n\n/*\n * F handles well-formed input.\n */\nfunc F() {}\n", src)
}

// TestStarBlockComment checks that a block comment drawn with asterisks
// is reported but not rewritten.
// gofmt leaves such comments alone,
// and a line inserted without an asterisk would make it start reformatting them.
func TestStarBlockComment(t *testing.T) {
	t.Parallel()
	src := "package a\n\n/*\n * F does a thing. It does another.\n */\nfunc F() {}\n"
	got := analyze(t, src)
	checkResult(t, got, []string{"4:18: missing line break after sentence"}, src, src)
}

// TestPlainBlockComment checks that a block comment without decoration is rewritten,
// since its continuation style is unambiguous.
func TestPlainBlockComment(t *testing.T) {
	t.Parallel()
	src := "package a\n\n/*\nIndented list:\n\n  - one. Two.\n*/\nfunc F() {}\n"
	got := analyze(t, src)
	checkResult(t, got, []string{"6:8: missing line break after sentence"},
		"package a\n\n/*\nIndented list:\n\n  - one.\n    Two.\n*/\nfunc F() {}\n", src)
}

// TestFixesConverge checks that applying the suggested fixes leaves no further violations,
// so that a single -fix run is enough.
func TestFixesConverge(t *testing.T) {
	t.Parallel()
	srcs := []string{
		wrap("// One. Two. Three."),
		wrap("// F has options:\n//\n//   - fast. it is quick.\n//   - safe. It is careful.\n//"),
		wrap("// F handles well-\n// formed input. It does."),
		"package a\n\nfunc F() {\n\t// One. Two. Three.\n}\n",
	}
	for i, src := range srcs {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			fixed := analyze(t, src).fixed
			if got := analyze(t, fixed); len(got.diags) != 0 {
				t.Errorf("after fixing, got %v\nfixed source:\n%s", got.diags, fixed)
			}
		})
	}
}

func checkResult(t *testing.T, got result, wantDiags []string, wantFixed, src string) {
	t.Helper()
	if !slices.Equal(got.diags, wantDiags) {
		t.Errorf("diagnostics:\n got %v\nwant %v", got.diags, wantDiags)
	}
	if wantFixed == "" {
		wantFixed = src
	}
	if got.fixed != wantFixed {
		t.Errorf("fixed source:\n got %q\nwant %q", got.fixed, wantFixed)
	}
}
