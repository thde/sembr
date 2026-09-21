// The sembr command checks Go comments for semantic line breaks.
//
// Usage:
//
//	sembr [flags] [package ...]
//
// See https://sembr.org for the specification
// and https://pkg.go.dev/golang.org/x/tools/go/analysis/singlechecker
// for the flags common to all analysis drivers.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"
	"thde.io/sembr"
)

func main() { singlechecker.Main(sembr.Analyzer) }
