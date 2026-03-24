// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// LineAccessor is an EmbeddedAccessor that accesses individual lines of a multi-line
// string value by line number. The path is a 1-based line number (as a string).
//
// For example, given a multi-line string "line one\nline two\nline three\n":
//   - ExistsP(doc, "2") returns true
//   - Data(doc, "2") returns "line two"
//   - SetP(doc, "new line two", "2") replaces line 2
//   - Extract("line one\nline two\n", "1") returns "line one"
//
// The config string is not used (pass "" when creating).
type LineAccessor struct{}

func newLineAccessor(_ string) (*LineAccessor, error) {
	return &LineAccessor{}, nil
}

func (la *LineAccessor) ExistsP(scalarYamlDoc *gaby.YamlDoc, path string) bool {
	lineNum, err := strconv.Atoi(path)
	if err != nil || lineNum < 1 {
		return false
	}
	value, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return false
	}
	lines := splitLines(value)
	return lineNum <= len(lines)
}

func (la *LineAccessor) Data(scalarYamlDoc *gaby.YamlDoc, path string) (any, error) {
	value, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return "", EmbeddedPathNotFound
	}
	return la.Extract(value, path)
}

func (la *LineAccessor) Extract(currentFieldValue, path string) (any, error) {
	lineNum, err := strconv.Atoi(path)
	if err != nil || lineNum < 1 {
		return "", errors.Mark(fmt.Errorf("invalid line number: %s", path), EmbeddedPathNotFound)
	}
	lines := splitLines(currentFieldValue)
	if lineNum > len(lines) {
		return "", errors.Mark(fmt.Errorf("line %d out of range (have %d lines)", lineNum, len(lines)), EmbeddedPathNotFound)
	}
	return lines[lineNum-1], nil
}

func (la *LineAccessor) Replace(currentFieldValue string, value any, path string) (string, error) {
	stringValue, ok := value.(string)
	if !ok {
		return currentFieldValue, UnsupportedValueType
	}
	lineNum, err := strconv.Atoi(path)
	if err != nil || lineNum < 1 {
		return currentFieldValue, errors.Mark(fmt.Errorf("invalid line number: %s", path), EmbeddedPathNotFound)
	}
	lines := splitLines(currentFieldValue)
	if lineNum > len(lines) {
		return currentFieldValue, errors.Mark(fmt.Errorf("line %d out of range (have %d lines)", lineNum, len(lines)), EmbeddedPathNotFound)
	}
	lines[lineNum-1] = stringValue
	return joinLines(lines, strings.HasSuffix(currentFieldValue, "\n")), nil
}

func (la *LineAccessor) SetP(scalarYamlDoc *gaby.YamlDoc, value any, path string) error {
	currentFieldValue, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return errors.Mark(fmt.Errorf("line %s not found", path), EmbeddedPathNotFound)
	}
	newFieldValue, err := la.Replace(currentFieldValue, value, path)
	if err != nil {
		return err
	}
	if newFieldValue == currentFieldValue {
		return nil
	}
	// Modify the scalar node's value directly. gaby.Set() with no hierarchy
	// doesn't modify the node, so we set the underlying YAML node value.
	ynode := scalarYamlDoc.YNode()
	ynode.Value = newFieldValue
	return nil
}

// splitLines splits a string into lines, handling the trailing newline correctly.
// "a\nb\nc\n" -> ["a", "b", "c"]
// "a\nb\nc"   -> ["a", "b", "c"]
func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// joinLines joins lines with newlines, optionally adding a trailing newline.
func joinLines(lines []string, trailingNewline bool) string {
	result := strings.Join(lines, "\n")
	if trailingNewline {
		result += "\n"
	}
	return result
}
