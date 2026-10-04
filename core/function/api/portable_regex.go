// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"unicode/utf8"
)

// A regular expression in a where expression is answered by whichever engine evaluates the term:
// PostgreSQL's `~` for a column, PostgreSQL's jsonpath like_regex for a path into a JSON attribute,
// or Go's regexp in memory and in the where-filter function. ValidatePortableRegex admits the
// subset of the syntax all three read the same way, so that a term matches the same values
// whichever engine answers it.
//
// The engines also differ on newlines: PostgreSQL's `~` lets `.` match one, while jsonpath and Go
// do not unless asked. Every engine is asked -- jsonpath with flag "s", Go with (?s) -- so `.`
// matches any character, and `^` and `$` match only at the ends of the value.

const (
	// MaxRegexLength bounds a regular expression or LIKE pattern operand.
	MaxRegexLength = 1024
	// maxRegexBound is the largest repetition count PostgreSQL accepts. Go accepts up to 1000.
	maxRegexBound = 255
	// maxRegexDepth bounds the nesting of groups.
	maxRegexDepth = 32
)

// IsRegexOperator reports whether an operator matches a regular expression.
func IsRegexOperator(operator string) bool {
	switch operator {
	case "~", "~*", "!~", "!~*":
		return true
	}
	return false
}

// IsLikeOperator reports whether an operator matches a LIKE pattern.
func IsLikeOperator(operator string) bool {
	switch operator {
	case "LIKE", "NOT LIKE", "ILIKE", "~~", "!~~":
		return true
	}
	return false
}

// ValidatePortableRegex checks that a pattern is in the subset of regular expression syntax that
// PostgreSQL and Go's regexp read the same way:
//
//   - literal characters, `.`, `^` and `$`;
//   - `*`, `+`, `?`, and bounds `{n}`, `{n,}` and `{n,m}` of at most 255, each after something to
//     repeat and not after another quantifier;
//   - alternation with `|`, and grouping with `(` and `)`;
//   - bracket expressions, negated with a leading `^`, with ranges, `]` first to include it, and
//     `-` first or last to include it.
//
// Excluded: anything beginning `(?` (flags, non-capturing groups, lookaround), lazy quantifiers,
// a pattern beginning `***` (a PostgreSQL director), `[:class:]`, `[.coll.]` and `[=equiv=]`
// (Go's classes are ASCII only, PostgreSQL's follow the locale), backslashes, and a `{`, `}` or
// `]` that is not part of a bound or bracket expression -- write `[{]` for a literal brace.
func ValidatePortableRegex(pattern string) error {
	if len(pattern) > MaxRegexLength {
		return fmt.Errorf("regular expression exceeds maximum length of %d", MaxRegexLength)
	}
	if !utf8.ValidString(pattern) {
		return fmt.Errorf("regular expression is not valid UTF-8")
	}
	checker := &regexChecker{pattern: []rune(pattern)}
	if err := checker.alternation(0); err != nil {
		return fmt.Errorf("unsupported regular expression `%s`: %v", pattern, err)
	}
	if checker.pos < len(checker.pattern) {
		return fmt.Errorf("unsupported regular expression `%s`: unmatched `)` at offset %d", pattern, checker.pos)
	}
	// The subset is meant to be a subset of what Go accepts; this keeps it one.
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("invalid regular expression `%s`: %v", pattern, err)
	}
	return nil
}

type regexChecker struct {
	pattern []rune
	pos     int
}

func (c *regexChecker) peek() (rune, bool) {
	if c.pos >= len(c.pattern) {
		return 0, false
	}
	return c.pattern[c.pos], true
}

func (c *regexChecker) alternation(depth int) error {
	for {
		if err := c.branch(depth); err != nil {
			return err
		}
		r, ok := c.peek()
		if !ok || r != '|' {
			return nil
		}
		c.pos++
	}
}

func (c *regexChecker) branch(depth int) error {
	for {
		r, ok := c.peek()
		if !ok || r == '|' || r == ')' {
			return nil
		}
		if err := c.piece(depth); err != nil {
			return err
		}
	}
}

func (c *regexChecker) piece(depth int) error {
	r, _ := c.peek()
	repeatable := true
	switch r {
	case '(':
		c.pos++
		if next, ok := c.peek(); ok && next == '?' {
			return fmt.Errorf("`(?` constructs are not supported at offset %d", c.pos-1)
		}
		if depth+1 > maxRegexDepth {
			return fmt.Errorf("groups nest deeper than %d", maxRegexDepth)
		}
		if err := c.alternation(depth + 1); err != nil {
			return err
		}
		if next, ok := c.peek(); !ok || next != ')' {
			return fmt.Errorf("missing `)`")
		}
		c.pos++
	case '[':
		if err := c.bracket(); err != nil {
			return err
		}
	case '^', '$':
		c.pos++
		repeatable = false
	case '*', '+', '?':
		return fmt.Errorf("`%c` at offset %d has nothing to repeat", r, c.pos)
	case '{':
		return fmt.Errorf("`{` at offset %d is not part of a bound; write `[{]` for a literal brace", c.pos)
	case '}', ']':
		return fmt.Errorf("`%c` at offset %d is not part of a bound or bracket expression; write `[%c]` for the character", r, c.pos, r)
	case '\\':
		return fmt.Errorf("backslashes are not supported")
	default:
		c.pos++
	}
	if !c.quantifier() {
		return nil
	}
	if !repeatable {
		return fmt.Errorf("an anchor cannot be repeated, at offset %d", c.pos-1)
	}
	return c.quantifierEnd()
}

// quantifier consumes a quantifier, if one follows, and reports whether it did. A `{` that does
// not begin a bound is left for the caller, which rejects it.
func (c *regexChecker) quantifier() bool {
	r, ok := c.peek()
	if !ok {
		return false
	}
	switch r {
	case '*', '+', '?':
		c.pos++
		return true
	case '{':
		return c.bound()
	}
	return false
}

// quantifierEnd rejects a second quantifier after the one just read: `a**` is an error in Go,
// and `a*?` is laziness, which cannot change whether a pattern matches.
func (c *regexChecker) quantifierEnd() error {
	r, ok := c.peek()
	if !ok {
		return nil
	}
	switch r {
	case '*', '+', '?', '{':
		return fmt.Errorf("`%c` at offset %d follows another quantifier", r, c.pos)
	}
	return nil
}

func (c *regexChecker) digits() (int, bool) {
	start := c.pos
	for c.pos < len(c.pattern) && c.pattern[c.pos] >= '0' && c.pattern[c.pos] <= '9' && c.pos-start < 4 {
		c.pos++
	}
	if c.pos == start {
		return 0, false
	}
	n, err := strconv.Atoi(string(c.pattern[start:c.pos]))
	return n, err == nil
}

// bound consumes `{n}`, `{n,}` or `{n,m}`. On anything else it consumes nothing.
func (c *regexChecker) bound() bool {
	start := c.pos
	c.pos++ // {
	low, ok := c.digits()
	if !ok || low > maxRegexBound {
		c.pos = start
		return false
	}
	high := low
	if r, _ := c.peek(); r == ',' {
		c.pos++
		high = maxRegexBound
		if n, ok := c.digits(); ok {
			high = n
		}
	}
	if r, _ := c.peek(); r != '}' || high > maxRegexBound || high < low {
		c.pos = start
		return false
	}
	c.pos++
	return true
}

func (c *regexChecker) bracket() error {
	open := c.pos
	c.pos++ // [
	if r, ok := c.peek(); ok && r == '^' {
		c.pos++
	}
	first := true
	for {
		r, ok := c.peek()
		if !ok {
			return fmt.Errorf("missing `]` for the bracket expression at offset %d", open)
		}
		switch {
		case r == ']' && !first:
			c.pos++
			return nil
		case r == '\\':
			return fmt.Errorf("backslashes are not supported")
		case r == '[':
			if next := c.pos + 1; next < len(c.pattern) {
				switch c.pattern[next] {
				case ':', '.', '=':
					return fmt.Errorf("`[%c` classes are not supported, at offset %d", c.pattern[next], c.pos)
				}
			}
		case r == '-' && !first:
			if next := c.pos + 1; next >= len(c.pattern) || c.pattern[next] != ']' {
				return fmt.Errorf("`-` at offset %d must end a range, or come first or last", c.pos)
			}
		}
		c.pos++
		first = false
		// A range: the character just read, `-`, and a character other than the closing `]`.
		if next := c.pos + 1; r != '-' && c.pos < len(c.pattern) && c.pattern[c.pos] == '-' && next < len(c.pattern) && c.pattern[next] != ']' {
			high := c.pattern[next]
			if high == '\\' || high == '[' {
				return fmt.Errorf("unsupported range end `%c` at offset %d", high, next)
			}
			if high < r {
				return fmt.Errorf("range `%c-%c` is out of order", r, high)
			}
			c.pos = next + 1
		}
	}
}

// patternCache holds compiled patterns, so that evaluating a term against many values compiles its
// pattern once. It is cleared rather than evicted from when full: patterns come from expressions,
// of which a process sees few distinct ones.
var patternCache = struct {
	sync.Mutex
	patterns map[string]*regexp.Regexp
}{patterns: map[string]*regexp.Regexp{}}

const maxCachedPatterns = 1024

// CompilePatternCached compiles a regular expression with the flags every engine evaluating a
// where expression in Go uses -- `.` matching a newline, and case folded when asked -- from the
// same cache, so a term evaluated against many values compiles its pattern once.
func CompilePatternCached(pattern string, caseInsensitive bool) (*regexp.Regexp, error) {
	return compileCached(regexFlags(caseInsensitive) + pattern)
}

func compileCached(expr string) (*regexp.Regexp, error) {
	patternCache.Lock()
	defer patternCache.Unlock()
	if re, ok := patternCache.patterns[expr]; ok {
		return re, nil
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}
	if len(patternCache.patterns) >= maxCachedPatterns {
		patternCache.patterns = map[string]*regexp.Regexp{}
	}
	patternCache.patterns[expr] = re
	return re, nil
}

// ValidatePatternOperand checks the operand of a regular expression or LIKE operator: a regular
// expression must be in the portable subset, and either must be no longer than MaxRegexLength.
// Any other operator is left alone.
func ValidatePatternOperand(operator, literal string, dataType DataType) error {
	if dataType != DataTypeString || (!IsRegexOperator(operator) && !IsLikeOperator(operator)) {
		return nil
	}
	pattern := literal
	if len(pattern) >= 2 && pattern[0] == '\'' && pattern[len(pattern)-1] == '\'' {
		pattern = pattern[1 : len(pattern)-1]
	}
	if IsRegexOperator(operator) {
		return ValidatePortableRegex(pattern)
	}
	if len(pattern) > MaxRegexLength {
		return fmt.Errorf("LIKE pattern exceeds maximum length of %d", MaxRegexLength)
	}
	return nil
}
