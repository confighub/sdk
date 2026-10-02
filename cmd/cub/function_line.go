// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"unicode"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A function invocation is written the same way wherever cub takes one: the function's name and
// its arguments, as on the "cub function do" command line. A line of the file "cub function exec"
// reads is one, and so is the value of a flag that holds a function, such as --function.

// splitFunctionLine splits a function line into words. Whitespace separates them. An argument
// that holds whitespace is quoted with ' or ": a quote opens at the start of a word, or straight
// after the = of a --name=value argument, and closes at the same character followed by
// whitespace or the end of the line. A quote anywhere else is part of the word, so
// r.kind=="Deployment" is what it looks like, and so is a quote inside a quoted argument. The one
// that would close the argument early, because whitespace follows it, is written with a
// backslash before it.
func splitFunctionLine(line string) ([]string, error) {
	var words []string
	i := 0
	for {
		for i < len(line) && unicode.IsSpace(rune(line[i])) {
			i++
		}
		if i >= len(line) {
			return words, nil
		}
		var word strings.Builder
		named := strings.HasPrefix(line[i:], "--")
		// A quote may open here, and once more after the first = of a named argument.
		mayOpen := true
		for i < len(line) && !unicode.IsSpace(rune(line[i])) {
			c := line[i]
			if mayOpen && (c == '\'' || c == '"') {
				end := closingQuote(line, i)
				if end < 0 {
					return nil, errors.Newf("the quote opened by %c at position %d of %q is never closed", c, i+1, line)
				}
				quote := string(c)
				word.WriteString(strings.ReplaceAll(line[i+1:end], `\`+quote, quote))
				i = end + 1
				break
			}
			mayOpen = false
			word.WriteByte(c)
			i++
			if named && c == '=' {
				named, mayOpen = false, true
			}
		}
		words = append(words, word.String())
	}
}

// closingQuote is the position of the quote that closes the one at open: the next one that has
// no backslash before it and is followed by whitespace or ends the line.
func closingQuote(line string, open int) int {
	for i := open + 1; i < len(line); i++ {
		if line[i] == line[open] && line[i-1] != '\\' && (i+1 == len(line) || unicode.IsSpace(rune(line[i+1]))) {
			return i
		}
	}
	return -1
}

// parseFunctionLine reads one function invocation from a line. A blank line is none.
func parseFunctionLine(line string) (*goclientnew.FunctionInvocation, error) {
	words, err := splitFunctionLine(line)
	if err != nil || len(words) == 0 {
		return nil, err
	}
	return initializeFunctionInvocation(words[0], words[1:]), nil
}
