package sembr

import (
	"fmt"
	"go/doc/comment"
	"strings"
)

// The specification requires that a semantic line break
// never alter the rendered output of the document.
// Rather than trusting this package's model of Go doc comment syntax
// to agree with godoc's in every case,
// each fix is applied to a copy of the comment
// and the result is re-rendered.
// A fix that would change what a reader sees is discarded
// and only the diagnostic is reported.

// texts returns the marker-stripped text of each line of a comment.
func texts(lines []line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.text
	}
	return out
}

// split returns the comment's lines with line i broken at s,
// the new line indented by brk.
func split(lines []string, i int, s site, brk string) []string {
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:i]...)
	out = append(out, lines[i][:s.from], brk+lines[i][s.to:])
	return append(out, lines[i+1:]...)
}

// join returns the comment's lines with line i+1,
// reduced to body by removing its continuation prefix,
// appended to line i.
func join(lines []string, i int, body string) []string {
	out := make([]string, 0, len(lines))
	out = append(out, lines[:i]...)
	out = append(out, lines[i]+body)
	return append(out, lines[i+2:]...)
}

// unchanged reports whether two versions of a comment render identically.
func unchanged(a, b []string) bool { return render(a) == render(b) }

// sameStructure reports whether two versions of a comment have the same block structure.
// Joining a hyphenated word intentionally changes the rendered text,
// so only the structure can be compared.
func sameStructure(a, b []string) bool { return structure(a) == structure(b) }

func render(lines []string) string {
	var p comment.Parser
	var pr comment.Printer
	return string(pr.Text(p.Parse(text(lines))))
}

func structure(lines []string) string {
	var p comment.Parser
	var b strings.Builder
	for _, blk := range p.Parse(text(lines)).Content {
		fmt.Fprintf(&b, "%T;", blk)
		if l, ok := blk.(*comment.List); ok {
			fmt.Fprintf(&b, "%d;", len(l.Items))
		}
	}
	return b.String()
}

func text(lines []string) string { return strings.Join(lines, "\n") + "\n" }
