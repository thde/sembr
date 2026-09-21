package sembr

import (
	"bytes"
	"go/ast"
	"go/token"
	"strings"
)

// line is a single physical line of a comment group
// with its comment marker removed,
// following the same rules as [ast.CommentGroup.Text].
//
// The text of a line is always a suffix of its physical source line,
// so the source position of text[i] is pos+token.Pos(i).
type line struct {
	pos    token.Pos // position of text[0] in the source
	indent string    // source bytes preceding text on its physical line
	cont   string    // text-level prefix an inserted line must repeat
	text   string    // line content, comment marker and trailing space removed
}

// end returns the position just past the last byte of the line's text.
func (l line) end() token.Pos { return l.pos + token.Pos(len(l.text)) }

// body returns the line's text with its continuation prefix removed.
func (l line) body() string { return strings.TrimPrefix(l.text, l.cont) }

// readLines converts a comment group into its constituent lines.
// Directive comments such as //go:generate are dropped,
// matching the behaviour of [ast.CommentGroup.Text],
// because godoc never renders them as prose.
//
// src must be the contents of the file containing the group
// and base its position of the first byte.
func readLines(g *ast.CommentGroup, src []byte, base token.Pos) []line {
	var lines []line
	for _, c := range g.List {
		off := int(c.Slash - base)
		if off < 0 || off+len(c.Text) > len(src) {
			return nil
		}
		if strings.HasPrefix(c.Text, "//") {
			l, ok := slashLine(c, src, base, off)
			if ok {
				lines = append(lines, l)
			}
			continue
		}
		ls, ok := starLines(c, src, off)
		if !ok {
			return nil
		}
		lines = append(lines, ls...)
	}
	return lines
}

// slashLine converts a //-style comment into a line.
// It reports false for directive comments,
// which carry no prose.
func slashLine(c *ast.Comment, src []byte, base token.Pos, off int) (line, bool) {
	body := c.Text[2:]
	skip := 2
	switch {
	case body == "":
		// Empty line;
		// nothing to strip.
	case body[0] == ' ':
		// Strip exactly one space,
		// as ast.CommentGroup.Text does.
		skip = 3
	case isDirective(body):
		return line{}, false
	}
	start := off + skip
	text := strings.TrimRight(c.Text[skip:], " \t")
	return line{
		pos:    base + token.Pos(start),
		indent: string(src[lineStart(src, off):start]),
		cont:   leadingSpace(text),
		text:   text,
	}, true
}

// starLines converts a /*-style comment into one line per physical source line.
// The opening and closing markers are removed
// but the interior indentation is not,
// because godoc treats it as significant.
// Each line therefore begins at the start of its physical line
// and needs no source prefix of its own.
//
// The text is taken from src rather than from c.Text.
// The scanner strips carriage returns from c.Text,
// which would put every position after the first CRLF line ending off by one.
func starLines(c *ast.Comment, src []byte, off int) ([]line, bool) {
	n := bytes.Index(src[off+2:], []byte("*/"))
	if n < 0 {
		return nil, false
	}
	segs := strings.Split(string(src[off+2:off+2+n]), "\n")

	// A break inserted on the opening line cannot repeat the /* marker,
	// so it follows the line below instead.
	first := ""
	if len(segs) > 1 {
		first = continuation(strings.TrimRight(segs[1], " \t\r"))
	}

	lines := make([]line, 0, len(segs))
	start := off + 2
	for i, seg := range segs {
		text := strings.TrimRight(seg, " \t\r")
		cont := continuation(text)
		if i == 0 {
			cont = first
		}
		lines = append(lines, line{
			pos:  c.Slash + token.Pos(start-off),
			cont: cont,
			text: text,
		})
		start += len(seg) + 1
	}
	return lines, true
}

// continuation returns the leading decoration of a comment line
// that an inserted line must repeat:
// its indentation,
// and the asterisk that block comments are conventionally drawn with.
func continuation(s string) string {
	i := len(leadingSpace(s))
	if i < len(s) && s[i] == '*' && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t') {
		i++
		i += len(leadingSpace(s[i:]))
	}
	return s[:i]
}

// lineStart returns the offset of the first byte of the source line containing off.
func lineStart(src []byte, off int) int {
	if i := bytes.LastIndexByte(src[:off], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

// standalone reports whether the group begins its own source line.
// Comments trailing code cannot be split
// without moving them relative to that code,
// so they are never rewritten.
func standalone(g *ast.CommentGroup, src []byte, base token.Pos) bool {
	off := int(g.Pos() - base)
	if off < 0 || off > len(src) {
		return false
	}
	return strings.TrimLeft(string(src[lineStart(src, off):off]), " \t") == ""
}

// adjacent reports whether b begins on the source line
// directly after the one a ends on.
// Lines of a group are not always adjacent in the source,
// because directive comments are dropped when the group is read.
func adjacent(fset *token.FileSet, a, b line) bool {
	return fset.Position(a.end()).Line+1 == fset.Position(b.pos).Line
}

// leadingSpace returns the run of spaces and tabs at the start of s.
func leadingSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}

// isDirective reports whether a //-comment body (with the slashes removed)
// is a comment directive.
// It mirrors the unexported go/ast function of the same name.
func isDirective(c string) bool {
	if strings.HasPrefix(c, "line ") || strings.HasPrefix(c, "extern ") || strings.HasPrefix(c, "export ") {
		return true
	}
	colon := strings.Index(c, ":")
	if colon <= 0 || colon+1 >= len(c) {
		return false
	}
	for i := 0; i <= colon+1; i++ {
		if i == colon {
			continue
		}
		if b := c[i]; ('a' > b || b > 'z') && ('0' > b || b > '9') {
			return false
		}
	}
	return true
}
