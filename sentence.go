package sembr

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// site is a place inside a line of prose where the specification requires a line break,
// described by offsets into the line's text.
type site struct {
	punct int // the sentence-ending punctuation mark
	from  int // start of the whitespace separating the two sentences
	to    int // start of the following sentence
}

// scanner locates sentence boundaries in prose.
type scanner struct {
	abbrev map[string]bool // words whose trailing period does not end a sentence
	strict bool            // report a boundary even when the next word is lowercase
}

// defaultAbbrev are abbreviations whose trailing period does not end a sentence.
// Abbreviations containing an interior period, such as "e.g." or "i.e.",
// need no entry here;
// they are recognised by their shape.
//
// The list is deliberately short.
// Every entry is a word that can no longer end a sentence,
// so entries that are also ordinary English words or common Go identifiers
// would silently hide real violations.
var defaultAbbrev = []string{
	"al", "aka", "approx", "cf", "chap", "dept", "eg", "esp", "etc", "fig",
	"figs", "ibid", "ie", "incl", "pp", "resp", "univ", "viz", "vol", "vols",
	"vs",
}

func newScanner(extra []string, strict bool) *scanner {
	abbrev := make(map[string]bool, len(defaultAbbrev)+len(extra))
	for _, w := range defaultAbbrev {
		abbrev[w] = true
	}
	for _, w := range extra {
		if w = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(w, "."))); w != "" {
			abbrev[w] = true
		}
	}
	return &scanner{abbrev: abbrev, strict: strict}
}

// sites returns every place in text at or after off
// where a sentence ends but the line continues.
func (sc *scanner) sites(text string, off int) []site {
	var sites []site
	for i := off; i < len(text); i++ {
		if !isTerminator(text[i]) {
			continue
		}
		end := i + 1
		for end < len(text) {
			r, n := utf8.DecodeRuneInString(text[end:])
			if !isCloser(r) {
				break
			}
			end += n
		}
		next := end + len(leadingSpace(text[end:]))
		if next == end || next == len(text) {
			// Either the mark is inside a word,
			// such as the period in a qualified identifier,
			// or the sentence ends the line.
			continue
		}
		if sc.ignores(text, i, next) {
			continue
		}
		sites = append(sites, site{punct: i, from: end, to: next})
		i = next - 1
	}
	return sites
}

// ignores reports whether the terminator at index i is not a sentence boundary,
// where next is the offset of the following word.
func (sc *scanner) ignores(text string, i, next int) bool {
	if !sc.strict && !startsSentence(text[next:]) {
		return true
	}
	if text[i] != '.' {
		return false
	}
	if i > 0 && text[i-1] == '.' {
		return true // the tail of an ellipsis
	}
	start := strings.LastIndexAny(text[:i], " \t") + 1
	word := strings.TrimLeft(text[start:i], openers)
	switch {
	case word == "":
		return true // a bare period
	case sc.abbrev[strings.ToLower(word)]:
		return true
	case isDottedAbbrev(word):
		return true
	case isInitial(word) && (lastWordCapitalised(text[:start]) || firstWordInitial(text[next:])):
		// A middle initial, as in "Stephen L. Moshier",
		// or one of a series, as in "T. S. Eliot".
		// A lone capital after a lowercase word,
		// such as the T in "works on type T",
		// is more often a type parameter ending a sentence.
		return true
	}
	return false
}

// startsSentence reports whether s opens with something that plausibly begins a sentence.
// Requiring this trades missed violations for a much lower rate of false reports,
// because Go comments are dense with abbreviations and identifiers
// that this package cannot recognise.
func startsSentence(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r) || unicode.IsDigit(r) || strings.ContainsRune(openers, r)
}

// openers and closers are the brackets and quotation marks
// that may wrap a sentence or a word.
const (
	openers = "[({\"'`«“‘"
	closers = ")]}\"'`»”’"
)

func isTerminator(b byte) bool { return b == '.' || b == '!' || b == '?' }

func isCloser(r rune) bool { return strings.ContainsRune(closers, r) }

// isDottedAbbrev reports whether s is an abbreviation written with interior periods,
// such as "e.g", "i.e" or "U.S":
// every segment between the periods is one or two letters.
// A longer or non-alphabetic segment marks a qualified identifier,
// a host name or a version,
// any of which can end a sentence.
func isDottedAbbrev(s string) bool {
	segs := strings.Split(s, ".")
	if len(segs) < 2 {
		return false
	}
	for _, seg := range segs {
		if n := utf8.RuneCountInString(seg); n < 1 || n > 2 {
			return false
		}
		for _, r := range seg {
			if !unicode.IsLetter(r) {
				return false
			}
		}
	}
	return true
}

// isInitial reports whether s is a single capital letter.
func isInitial(s string) bool {
	r, n := utf8.DecodeRuneInString(s)
	return n == len(s) && unicode.IsUpper(r)
}

// lastWordCapitalised reports whether the final word of s begins with a capital letter,
// as the name preceding a middle initial does.
func lastWordCapitalised(s string) bool {
	s = strings.TrimRight(s, " \t")
	word := strings.TrimLeft(s[strings.LastIndexAny(s, " \t")+1:], openers)
	r, _ := utf8.DecodeRuneInString(word)
	return unicode.IsUpper(r)
}

// firstWordInitial reports whether the first word of s is an initial followed by its period,
// as the second of a series of initials is.
func firstWordInitial(s string) bool {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	return strings.HasSuffix(s, ".") && isInitial(strings.TrimSuffix(s, "."))
}

// hyphenated reports whether a line ending in a and continuing on b
// splits a hyphenated word across the two lines.
//
// A hyphen ending a URL is not a split word.
// The specification allows a line break after a hyperlink,
// and joining one to the following word would corrupt it.
func hyphenated(a, b string) bool {
	if !strings.HasSuffix(a, "-") || len(a) < 2 {
		return false
	}
	if word := a[strings.LastIndexAny(a, " \t")+1:]; strings.ContainsAny(word, "/:") {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(a[:len(a)-1])
	if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
		return false
	}
	r, _ = utf8.DecodeRuneInString(strings.TrimLeft(b, " \t"))
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
