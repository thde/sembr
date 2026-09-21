# sembr

A Go analysis pass that checks comments for [semantic line breaks][spec].

```
go run github.com/thde/sembr/cmd/sembr@latest ./...
go run github.com/thde/sembr/cmd/sembr@latest -fix ./...
```

The analyzer is an ordinary `*analysis.Analyzer`,
so it also runs under `go vet -vettool`, `singlechecker`
and the golangci-lint module plugin system.

## What is checked

The specification states most of its rules with SHOULD or MAY.
Only the two absolute rules are enforced:

| Rule                                                 | Reported as                         |
| ---------------------------------------------------- | ----------------------------------- |
| A line break MUST occur after a sentence             | `missing line break after sentence` |
| A line break MUST NOT occur within a hyphenated word | `line break inside hyphenated word` |

Whether a comma separates independent clauses,
or whether a break would help a reader,
cannot be decided without understanding the prose,
so those rules are left to the author.
A break is never itself a violation;
only a missing one is reported.

## Sentence detection

Go comments are full of periods that do not end a sentence.
A period is ignored when

- it is part of an ellipsis;
- the word before it is a known abbreviation such as `etc.`,
  or one written with interior periods such as `e.g.` or `U.S.`
  (every segment one or two letters,
  so `io.EOF.` and `v1.2.3.` still end a sentence).
- the word before it is an initial in a name,
  as in `Stephen L. Moshier` or `T. S. Eliot`.
- the word after it does not begin with a capital,
  a digit, or an opening bracket or quote.

The last rule hides a violation whenever a sentence begins
with a lowercase identifier, which is common in Go.
The trade is deliberate:
a linter that cries wolf gets turned off.
Pass `-strict` to drop that rule,
and `-abbrev` to add abbreviations of your own:

```
sembr -strict -abbrev=ms,req,resp ./...
```

## Fixes

Every diagnostic carries a suggested fix, applied with `-fix`.

## Testing

`go test -short` runs the unit tests.
Without `-short` it also runs the analyzer over the Go standard library and asserts, for every file it changes,
that the result parses, renders the same.

[spec]: https://sembr.org
