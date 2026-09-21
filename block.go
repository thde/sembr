package sembr

import (
	"strings"
	"unicode/utf8"
)

// proseLine is a comment line that godoc renders as flowing text.
type proseLine struct {
	line
	idx int    // index of the line within its comment group
	off int    // offset in text where prose starts, past any list marker
	brk string // text-level indentation for a line inserted inside this line
}

// proseRuns groups the lines of a comment into runs of adjacent prose.
//
// Each run is rendered by godoc as a single flowing block of text,
// so a line break may be inserted anywhere inside it
// and a break at the end of one of its lines joins with the next.
// Code blocks, headings and blank lines are omitted;
// they are either not prose or not safe to reflow.
//
// The classification mirrors go/doc/comment,
// but deliberately omits its recovery heuristics for malformed comments.
// Anything those heuristics would reinterpret is caught later by [unchanged],
// which rejects a fix that would change the rendered output.
func proseRuns(lines []line) [][]proseLine {
	u := unindent(lines)

	var runs [][]proseLine
	for i := 0; i < len(lines); {
		if u[i] == "" {
			i++
			continue
		}
		start := i
		if indented(u[i]) {
			for i < len(lines) && (u[i] == "" || indented(u[i])) {
				i++
			}
			end := i
			for end > start && u[end-1] == "" {
				end--
			}
			if isList(u[start]) {
				runs = append(runs, listRuns(start, lines[start:end], u[start:end])...)
			}
			continue
		}
		for i < len(lines) && u[i] != "" && !indented(u[i]) {
			i++
		}
		if i-start == 1 && isHeading(u[start]) {
			continue
		}
		runs = append(runs, paragraphRun(start, lines[start:i]))
	}
	return runs
}

// paragraphRun converts the lines of a paragraph into a prose run.
func paragraphRun(base int, lines []line) []proseLine {
	run := make([]proseLine, len(lines))
	for i, l := range lines {
		run[i] = proseLine{line: l, idx: base + i, brk: l.cont}
	}
	return run
}

// listRuns splits a list into one prose run per item.
// Items are separated rather than joined into a single run
// because a line break at the end of one item does not flow into the next.
func listRuns(base int, lines []line, u []string) [][]proseLine {
	var runs [][]proseLine
	var cur []proseLine
	flush := func() {
		if len(cur) > 0 {
			runs = append(runs, cur)
			cur = nil
		}
	}
	for i, l := range lines {
		switch {
		case u[i] == "":
			flush()
		case isList(u[i]):
			flush()
			// A list item continues on lines aligned under its text.
			// A block comment drawn with asterisks is the exception:
			// its decoration has to be repeated,
			// which usually means no fix is possible,
			// since repeating the asterisk would start a new item.
			off := markerEnd(l.text)
			brk := blank(l.text[:off])
			if strings.TrimLeft(l.cont, " \t") != "" {
				brk = l.cont
			}
			cur = append(cur, proseLine{line: l, idx: base + i, off: off, brk: brk})
		default:
			cur = append(cur, proseLine{line: l, idx: base + i, brk: l.cont})
		}
	}
	flush()
	return runs
}

// markerEnd returns the offset in a list item line where the item's content begins,
// that is, just past the bullet or number and the space following it.
func markerEnd(s string) int {
	i := len(leadingSpace(s))
	if r, n := utf8.DecodeRuneInString(s[i:]); r == '•' || r == '*' || r == '+' || r == '-' {
		i += n
	} else {
		for i < len(s) && '0' <= s[i] && s[i] <= '9' {
			i++
		}
		i++ // the '.' or ')' following the number
	}
	return i + len(leadingSpace(s[i:]))
}

// blank replaces every byte of s except tabs with a space,
// so that the result occupies the same columns as s.
func blank(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return '\t'
		}
		return ' '
	}, s)
}

// unindent returns the text of each line with the comment's common indentation removed,
// mirroring the unexported go/doc/comment function of the same name.
// Whitespace-only lines become empty.
func unindent(lines []line) []string {
	prefix, seen := "", false
	for _, l := range lines {
		if strings.TrimSpace(l.text) == "" {
			continue
		}
		if !seen {
			prefix, seen = leadingSpace(l.text), true
			continue
		}
		prefix = commonPrefix(prefix, leadingSpace(l.text))
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		s := strings.TrimPrefix(l.text, prefix)
		if strings.TrimSpace(s) == "" {
			s = ""
		}
		out[i] = s
	}
	return out
}

func commonPrefix(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return a[:i]
}

func indented(s string) bool {
	return s != "" && (s[0] == ' ' || s[0] == '\t')
}

func isHeading(s string) bool {
	return len(s) >= 2 && s[0] == '#' && (s[1] == ' ' || s[1] == '\t') && strings.TrimSpace(s) != "#"
}

// isList reports whether s begins with a bullet or numbered list marker,
// mirroring the listMarker function in go/doc/comment.
func isList(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	var rest string
	if r, n := utf8.DecodeRuneInString(s); r == '•' || r == '*' || r == '+' || r == '-' {
		rest = s[n:]
	} else if '0' <= s[0] && s[0] <= '9' {
		n := 1
		for n < len(s) && '0' <= s[n] && s[n] <= '9' {
			n++
		}
		if n >= len(s) || (s[n] != '.' && s[n] != ')') {
			return false
		}
		rest = s[n+1:]
	} else {
		return false
	}
	return indented(rest) && strings.TrimSpace(rest) != ""
}
