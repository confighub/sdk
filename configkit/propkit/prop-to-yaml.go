// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package propkit

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/third_party/gaby"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// PropertiesParser handles parsing of Java Properties files
type PropertiesParser struct {
	properties     map[string]string
	keyOrder       []string          // keys in source file order
	inlineComments map[string]string // key -> inline comment text
}

// NewPropertiesParser creates a new parser instance
func NewPropertiesParser() *PropertiesParser {
	return &PropertiesParser{
		properties:     make(map[string]string),
		inlineComments: make(map[string]string),
	}
}

// ParseProperties reads and parses Java Properties from byte slice
func (p *PropertiesParser) ParseProperties(propertiesData []byte) error {
	// Clear existing properties
	p.properties = make(map[string]string)
	p.keyOrder = nil
	p.inlineComments = make(map[string]string)

	reader := bytes.NewReader(propertiesData)
	scanner := bufio.NewScanner(reader)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		// Handle line continuations (an unescaped backslash at the end)
		for endsWithContinuation(line) {
			line = line[:len(line)-1] // Remove backslash
			if !scanner.Scan() {
				break
			}
			line += strings.TrimSpace(scanner.Text())
			lineNum++
		}

		// Parse key-value pair. The inline comment is split off before the
		// value is unescaped, so that an escaped # doesn't start one.
		rawKey, rawValue, ok := splitKeyValue(line)
		if !ok {
			return fmt.Errorf("error parsing line %d: no separator found in line: %s", lineNum, line)
		}
		key := unescapeProperty(rawKey)
		cleanValue, comment := yamlkit.SplitInlineComment(rawValue)
		value := unescapeProperty(cleanValue)

		if key != "" {
			if comment != "" {
				p.inlineComments[key] = comment
			}
			if _, exists := p.properties[key]; !exists {
				p.keyOrder = append(p.keyOrder, key)
			}
			p.properties[key] = value
		}
	}

	return scanner.Err()
}

// endsWithContinuation reports whether a line ends with a backslash that
// isn't itself escaped, which continues the line on the next one.
func endsWithContinuation(line string) bool {
	n := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// splitKeyValue splits a properties line at its first unescaped = or :,
// returning the key and the value as written, with surrounding whitespace
// removed, or false if the line has no separator.
func splitKeyValue(line string) (key, value string, ok bool) {
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '=', ':':
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
		}
	}
	return "", "", false
}

// unescapeProperty undoes Java properties escaping: \t, \n, \r and \f, \uXXXX,
// including surrogate pairs, and a backslash before any other character,
// which stands for that character.
func unescapeProperty(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var result strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			result.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			result.WriteByte('\n')
		case 't':
			result.WriteByte('\t')
		case 'r':
			result.WriteByte('\r')
		case 'f':
			result.WriteByte('\f')
		case 'u':
			r, ok := parseUnicodeEscape(s[i+1:])
			if !ok {
				result.WriteByte('u')
				continue
			}
			i += 4
			if utf16.IsSurrogate(r) && strings.HasPrefix(s[i+1:], "\\u") {
				if r2, ok := parseUnicodeEscape(s[i+3:]); ok {
					if combined := utf16.DecodeRune(r, r2); combined != utf8.RuneError {
						r = combined
						i += 6
					}
				}
			}
			result.WriteRune(r)
		default:
			result.WriteByte(s[i])
		}
	}
	return result.String()
}

// parseUnicodeEscape reads the four hex digits of a \u escape.
func parseUnicodeEscape(s string) (rune, bool) {
	if len(s) < 4 {
		return 0, false
	}
	code, err := strconv.ParseUint(s[:4], 16, 32)
	if err != nil {
		return 0, false
	}
	return rune(code), true
}

// ToNestedMap converts flat properties to nested map structure
func (p *PropertiesParser) ToNestedMap() map[string]interface{} {
	result := make(map[string]interface{})

	// Sort keys to ensure consistent output
	keys := make([]string, 0, len(p.properties))
	for key := range p.properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := p.properties[key]
		p.setNestedValue(result, key, value)
	}

	return result
}

// setNestedValue sets a value in a nested map structure based on dot notation
func (p *PropertiesParser) setNestedValue(m map[string]interface{}, key, value string) {
	parts := strings.Split(key, ".")
	current := m

	// Navigate/create nested structure
	for _, part := range parts[:len(parts)-1] {
		if existing, exists := current[part]; exists {
			if nestedMap, ok := existing.(map[string]interface{}); ok {
				current = nestedMap
			} else {
				// Convert existing value to nested structure
				newMap := make(map[string]interface{})
				newMap[""] = existing // Store original value with empty key
				current[part] = newMap
				current = newMap
			}
		} else {
			newMap := make(map[string]interface{})
			current[part] = newMap
			current = newMap
		}
	}

	// Set the final value
	finalKey := parts[len(parts)-1]

	// Try to convert value to appropriate type
	convertedValue := p.convertValue(value)
	current[finalKey] = convertedValue
}

// convertValue returns the value a property is stored as in YAML: its text.
// Properties have no types; the application reading them interprets them.
func (p *PropertiesParser) convertValue(value string) interface{} {
	return value
}

// GetProperties returns the flat properties map
func (p *PropertiesParser) GetProperties() map[string]string {
	return p.properties
}

// NativeToYAML converts Properties data to YAML format.
// Comments are preserved as $comment$ keys. Key order from the source is preserved.
// Every value is a string, since properties have no types (see
// yamlkit.TextValuedResourceProvider).
func (*PropertiesResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	// Parse properties config (tracks key order)
	parser := NewPropertiesParser()
	if err := parser.ParseProperties(data); err != nil {
		return []byte{}, err
	}

	// Extract comments from raw Properties source
	comments := extractPropertiesComments(data)
	// Add inline comments detected during parsing
	for key, text := range parser.inlineComments {
		parts := strings.Split(key, ".")
		targetKey := parts[len(parts)-1]
		parentPath := strings.Join(parts[:len(parts)-1], ".")
		commentKey := yamlkit.CommentKey(yamlkit.CommentLine, targetKey)
		if parentPath != "" {
			comments[parentPath+"."+commentKey] = text
		} else {
			comments[commentKey] = text
		}
	}

	// Build a gaby YamlDoc directly, preserving source key order.
	doc := gaby.New()

	for _, flatKey := range parser.keyOrder {
		parts := strings.Split(flatKey, ".")
		targetKey := parts[len(parts)-1]
		parentPath := strings.Join(parts[:len(parts)-1], ".")

		// Add head comment
		headCK := yamlkit.CommentKey(yamlkit.CommentHead, targetKey)
		flatHead := flatCommentPath(parentPath, headCK)
		if text, ok := comments[flatHead]; ok {
			if err := yamlkit.SetPath(doc, text, splitPath(flatHead)...); err != nil {
				return []byte{}, err
			}
			delete(comments, flatHead)
		}

		// Add data value
		if err := yamlkit.SetPath(doc, parser.properties[flatKey], parts...); err != nil {
			return []byte{}, err
		}

		// Add line comment
		lineCK := yamlkit.CommentKey(yamlkit.CommentLine, targetKey)
		flatLine := flatCommentPath(parentPath, lineCK)
		if text, ok := comments[flatLine]; ok {
			if err := yamlkit.SetPath(doc, text, splitPath(flatLine)...); err != nil {
				return []byte{}, err
			}
			delete(comments, flatLine)
		}
	}

	// Add any remaining comments (e.g., trailing foot comments)
	for path, text := range comments {
		if err := yamlkit.SetPath(doc, text, splitPath(path)...); err != nil {
			return []byte{}, err
		}
	}

	// Convert mapping nodes with contiguous 0-based integer keys to sequences.
	// Properties encodes arrays as key.0=val, key.1=val, etc.
	convertNumericMapsToSequences(doc.YNode())

	return doc.Bytes(), nil
}

func flatCommentPath(parentPath, commentKey string) string {
	if parentPath != "" {
		return parentPath + "." + commentKey
	}
	return commentKey
}

func splitPath(dotPath string) []string {
	return strings.Split(dotPath, ".")
}

// convertNumericMapsToSequences walks a yaml.Node tree and converts mapping nodes
// whose non-comment keys are contiguous 0-based integers (0, 1, 2, ...) into
// sequence nodes. This handles the Properties convention of encoding arrays as
// key.0=val, key.1=val, etc.
func convertNumericMapsToSequences(node *yaml.Node) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.MappingNode:
		// First recurse into value children so nested arrays are converted bottom-up
		for i := 1; i < len(node.Content); i += 2 {
			convertNumericMapsToSequences(node.Content[i])
		}
		// Then check if this mapping should become a sequence
		if isContiguousNumericMapping(node) {
			convertMappingToSequence(node)
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			convertNumericMapsToSequences(child)
		}
	case yaml.DocumentNode:
		for _, child := range node.Content {
			convertNumericMapsToSequences(child)
		}
	}
}

// isContiguousNumericMapping checks if a mapping node's non-comment keys are
// contiguous integers starting from 0.
func isContiguousNumericMapping(node *yaml.Node) bool {
	if node.Kind != yaml.MappingNode || len(node.Content) == 0 {
		return false
	}
	expectedIdx := 0
	for i := 0; i < len(node.Content)-1; i += 2 {
		key := node.Content[i]
		if key.Kind != yaml.ScalarNode {
			return false
		}
		if yamlkit.IsCommentKey(key.Value) {
			continue
		}
		idx, err := strconv.Atoi(key.Value)
		if err != nil || idx != expectedIdx {
			return false
		}
		expectedIdx++
	}
	return expectedIdx > 0
}

// convertMappingToSequence converts a mapping node with numeric keys to a
// sequence node. Comment keys at this level are dropped (they can't be
// represented as siblings in a YAML sequence).
func convertMappingToSequence(node *yaml.Node) {
	var values []*yaml.Node
	for i := 0; i < len(node.Content)-1; i += 2 {
		key := node.Content[i]
		if yamlkit.IsCommentKey(key.Value) {
			continue
		}
		values = append(values, node.Content[i+1])
	}
	node.Kind = yaml.SequenceNode
	node.Content = values
	node.Tag = ""
}
