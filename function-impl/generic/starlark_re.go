// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"
	"regexp"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// starlarkReModule is the Starlark "re" module providing regular expression support
// backed by Go's regexp package (RE2 syntax).
//
// Functions:
//
//	re.search(pattern, string) → match or None
//	re.match(pattern, string) → match or None (anchored at start)
//	re.sub(pattern, repl, string, count=0) → string
//	re.findall(pattern, string) → list of strings (or list of tuples if groups)
//
// Match objects have:
//
//	m.group(n=0) → string
//	m.groups() → tuple of strings
//	m.start(n=0) → int
//	m.end(n=0) → int
//	m.span(n=0) → (start, end)
var starlarkReModule = &starlarkstruct.Module{
	Name: "re",
	Members: starlark.StringDict{
		"search":  starlark.NewBuiltin("re.search", reSearch),
		"match":   starlark.NewBuiltin("re.match", reMatch),
		"sub":     starlark.NewBuiltin("re.sub", reSub),
		"findall": starlark.NewBuiltin("re.findall", reFindall),
	},
}

// reSearch implements re.search(pattern, string) → match or None.
func reSearch(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, str string
	if err := starlark.UnpackPositionalArgs("re.search", args, kwargs, 2, &pattern, &str); err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("re.search: %w", err)
	}
	loc := re.FindStringSubmatchIndex(str)
	if loc == nil {
		return starlark.None, nil
	}
	return newStarlarkMatch(str, loc), nil
}

// reMatch implements re.match(pattern, string) → match or None.
// Anchored at the beginning of the string (like Python's re.match).
func reMatch(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, str string
	if err := starlark.UnpackPositionalArgs("re.match", args, kwargs, 2, &pattern, &str); err != nil {
		return nil, err
	}
	// Anchor at start if not already
	anchored := pattern
	if len(anchored) == 0 || anchored[0] != '^' {
		anchored = "^(?:" + pattern + ")"
	}
	re, err := regexp.Compile(anchored)
	if err != nil {
		return nil, fmt.Errorf("re.match: %w", err)
	}
	loc := re.FindStringSubmatchIndex(str)
	if loc == nil {
		return starlark.None, nil
	}
	return newStarlarkMatch(str, loc), nil
}

// reSub implements re.sub(pattern, repl, string, count=0) → string.
// repl is a literal replacement string (no backreference expansion).
// count=0 means replace all occurrences.
func reSub(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, repl, str string
	count := 0
	if err := starlark.UnpackArgs("re.sub", args, kwargs, "pattern", &pattern, "repl", &repl, "string", &str, "count?", &count); err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("re.sub: %w", err)
	}
	if count == 0 {
		return starlark.String(re.ReplaceAllLiteralString(str, repl)), nil
	}
	// Replace up to count occurrences
	n := 0
	result := re.ReplaceAllStringFunc(str, func(match string) string {
		if n < count {
			n++
			return repl
		}
		return match
	})
	return starlark.String(result), nil
}

// reFindall implements re.findall(pattern, string) → list.
// If the pattern has no groups, returns a list of strings.
// If the pattern has one group, returns a list of strings (the group match).
// If the pattern has multiple groups, returns a list of tuples.
func reFindall(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, str string
	if err := starlark.UnpackPositionalArgs("re.findall", args, kwargs, 2, &pattern, &str); err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("re.findall: %w", err)
	}
	numGroups := re.NumSubexp()
	matches := re.FindAllStringSubmatch(str, -1)
	elems := make([]starlark.Value, 0, len(matches))
	for _, m := range matches {
		switch {
		case numGroups == 0:
			elems = append(elems, starlark.String(m[0]))
		case numGroups == 1:
			elems = append(elems, starlark.String(m[1]))
		default:
			tuple := make(starlark.Tuple, numGroups)
			for i := 1; i <= numGroups; i++ {
				tuple[i-1] = starlark.String(m[i])
			}
			elems = append(elems, tuple)
		}
	}
	return starlark.NewList(elems), nil
}

// starlarkMatch is a Starlark value representing a regex match result.
type starlarkMatch struct {
	str string // the original string
	loc []int  // submatch indices from FindStringSubmatchIndex
}

func newStarlarkMatch(str string, loc []int) *starlarkMatch {
	return &starlarkMatch{str: str, loc: loc}
}

func (m *starlarkMatch) String() string {
	return fmt.Sprintf("<re.match group(0)=%q>", m.group(0))
}
func (m *starlarkMatch) Type() string          { return "re.match" }
func (m *starlarkMatch) Freeze()               {} // immutable
func (m *starlarkMatch) Truth() starlark.Bool   { return true }
func (m *starlarkMatch) Hash() (uint32, error)  { return 0, fmt.Errorf("unhashable type: re.match") }

func (m *starlarkMatch) numGroups() int { return len(m.loc)/2 - 1 }

func (m *starlarkMatch) group(n int) string {
	start := m.loc[2*n]
	end := m.loc[2*n+1]
	if start < 0 {
		return ""
	}
	return m.str[start:end]
}

// Attr implements starlark.HasAttrs.
func (m *starlarkMatch) Attr(name string) (starlark.Value, error) {
	switch name {
	case "group":
		return starlark.NewBuiltin("match.group", m.groupMethod), nil
	case "groups":
		return starlark.NewBuiltin("match.groups", m.groupsMethod), nil
	case "start":
		return starlark.NewBuiltin("match.start", m.startMethod), nil
	case "end":
		return starlark.NewBuiltin("match.end", m.endMethod), nil
	case "span":
		return starlark.NewBuiltin("match.span", m.spanMethod), nil
	default:
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (m *starlarkMatch) AttrNames() []string {
	return []string{"group", "groups", "start", "end", "span"}
}

func (m *starlarkMatch) groupMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	n := 0
	if err := starlark.UnpackPositionalArgs("match.group", args, kwargs, 0, &n); err != nil {
		return nil, err
	}
	if n < 0 || n > m.numGroups() {
		return nil, fmt.Errorf("match.group: index %d out of range (0..%d)", n, m.numGroups())
	}
	return starlark.String(m.group(n)), nil
}

func (m *starlarkMatch) groupsMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, fmt.Errorf("match.groups: takes no arguments")
	}
	tuple := make(starlark.Tuple, m.numGroups())
	for i := 0; i < m.numGroups(); i++ {
		tuple[i] = starlark.String(m.group(i + 1))
	}
	return tuple, nil
}

func (m *starlarkMatch) startMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	n := 0
	if err := starlark.UnpackPositionalArgs("match.start", args, kwargs, 0, &n); err != nil {
		return nil, err
	}
	if n < 0 || n > m.numGroups() {
		return nil, fmt.Errorf("match.start: index %d out of range (0..%d)", n, m.numGroups())
	}
	return starlark.MakeInt(m.loc[2*n]), nil
}

func (m *starlarkMatch) endMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	n := 0
	if err := starlark.UnpackPositionalArgs("match.end", args, kwargs, 0, &n); err != nil {
		return nil, err
	}
	if n < 0 || n > m.numGroups() {
		return nil, fmt.Errorf("match.end: index %d out of range (0..%d)", n, m.numGroups())
	}
	return starlark.MakeInt(m.loc[2*n+1]), nil
}

func (m *starlarkMatch) spanMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	n := 0
	if err := starlark.UnpackPositionalArgs("match.span", args, kwargs, 0, &n); err != nil {
		return nil, err
	}
	if n < 0 || n > m.numGroups() {
		return nil, fmt.Errorf("match.span: index %d out of range (0..%d)", n, m.numGroups())
	}
	return starlark.Tuple{starlark.MakeInt(m.loc[2*n]), starlark.MakeInt(m.loc[2*n+1])}, nil
}
