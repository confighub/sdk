// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"strconv"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// Formats such as Java properties, INI and env files have no types: every
// value is text, which the application reading the file interprets. Their
// values are therefore strings in YAML, and are written back as their YAML
// text, so that 1.10 and 02134 come back as they were. The functions that
// get and set ints and bools read and write that text (see
// TextValuedResourceProvider).

// TextValuedResourceProvider is implemented by the resource provider of a
// format whose values are all text. For such a format, a function that gets
// an int or a bool reads it from a string whose text is one, and a function
// that sets one writes its text.
type TextValuedResourceProvider interface {
	ValuesAreText() bool
}

// ValuesAreText reports whether the values of a resource provider's format
// are all text.
func ValuesAreText(resourceProvider ResourceProvider) bool {
	tv, ok := resourceProvider.(TextValuedResourceProvider)
	return ok && tv.ValuesAreText()
}

// TextAs returns the value of type T that text is the text of: an int in
// decimal, or the bool true or false. A string is text itself.
func TextAs[T api.Scalar](text string) (T, bool) {
	var result T
	switch p := any(&result).(type) {
	case *string:
		*p = text
	case *int:
		i, err := strconv.Atoi(text)
		if err != nil {
			return result, false
		}
		*p = i
	case *bool:
		switch text {
		case "true":
			*p = true
		case "false":
			*p = false
		default:
			return result, false
		}
	default:
		return result, false
	}
	return result, true
}

// TextOf returns the text of an int, bool or string value.
func TextOf[T api.Scalar](value T) string {
	switch v := any(value).(type) {
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	case string:
		return v
	}
	return fmt.Sprint(value)
}

// ScalarText returns the text of a scalar node as an untyped format writes
// it: its YAML text, so that a number or a date is written as it appears in
// YAML, or the empty string for null.
func ScalarText(node *yaml.Node) string {
	node = gaby.ResolveAlias(node)
	if node.ShortTag() == yaml.NodeTagNull {
		return ""
	}
	return node.Value
}

// TextEntry is a key, made of path segments, and the text of its value.
type TextEntry struct {
	Path []string
	Text string
}

// FlattenToText returns the scalars under node in document order, with the
// path to each: a mapping's keys and a sequence's indexes are segments of
// the path. Comment keys are skipped, and merge keys and aliases are
// expanded. An empty mapping or sequence contributes nothing.
func FlattenToText(node *yaml.Node) ([]TextEntry, error) {
	var entries []TextEntry
	err := flattenToText(node, nil, &entries)
	return entries, err
}

func flattenToText(node *yaml.Node, path []string, entries *[]TextEntry) error {
	node = gaby.ResolveAlias(node)
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			if err := flattenToText(child, path, entries); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		content, err := gaby.MappingEntries(node)
		if err != nil {
			return err
		}
		for i := 0; i+1 < len(content); i += 2 {
			key := gaby.ResolveAlias(content[i])
			if IsCommentKey(key.Value) {
				continue
			}
			if err := flattenToText(content[i+1], appendSegment(path, key.Value), entries); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, child := range node.Content {
			if err := flattenToText(child, appendSegment(path, strconv.Itoa(i)), entries); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if len(path) == 0 {
			// A document that is a single scalar has no key; only a null
			// one, which is what an empty document is, means anything.
			if node.ShortTag() == yaml.NodeTagNull {
				return nil
			}
			return fmt.Errorf("the document is the scalar %q, not a mapping of keys to values", node.Value)
		}
		*entries = append(*entries, TextEntry{Path: path, Text: ScalarText(node)})
	}
	return nil
}

// appendSegment returns path with segment appended, never sharing the
// backing array of path, since the result is kept.
func appendSegment(path []string, segment string) []string {
	result := make([]string, len(path)+1)
	copy(result, path)
	result[len(path)] = segment
	return result
}
