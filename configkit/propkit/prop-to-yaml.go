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

		// Handle line continuations (backslash at end)
		for strings.HasSuffix(line, "\\") {
			line = line[:len(line)-1] // Remove backslash
			if scanner.Scan() {
				nextLine := strings.TrimSpace(scanner.Text())
				line += nextLine
				lineNum++
			}
		}

		// Parse key-value pair
		key, value, err := p.parseKeyValue(line)
		if err != nil {
			return fmt.Errorf("error parsing line %d: %w", lineNum, err)
		}

		if key != "" {
			// Strip inline comments from value
			cleanValue, comment := yamlkit.SplitInlineComment(value)
			if comment != "" {
				p.inlineComments[key] = comment
				value = cleanValue
			}
			if _, exists := p.properties[key]; !exists {
				p.keyOrder = append(p.keyOrder, key)
			}
			p.properties[key] = value
		}
	}

	return scanner.Err()
}

// parseKeyValue extracts key and value from a properties line
func (p *PropertiesParser) parseKeyValue(line string) (string, string, error) {
	// Find the separator (= or :)
	sepIndex := -1
	for i, char := range line {
		if char == '=' || char == ':' {
			// Make sure it's not escaped
			if i == 0 || line[i-1] != '\\' {
				sepIndex = i
				break
			}
		}
	}

	if sepIndex == -1 {
		return "", "", fmt.Errorf("no separator found in line: %s", line)
	}

	key := strings.TrimSpace(line[:sepIndex])
	value := strings.TrimSpace(line[sepIndex+1:])

	// Unescape key and value
	key = p.unescapeString(key)
	value = p.unescapeString(value)

	return key, value, nil
}

// unescapeString handles Java Properties escaping
func (p *PropertiesParser) unescapeString(s string) string {
	result := strings.Builder{}
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				result.WriteByte('\n')
			case 't':
				result.WriteByte('\t')
			case 'r':
				result.WriteByte('\r')
			case '\\':
				result.WriteByte('\\')
			case '=':
				result.WriteByte('=')
			case ':':
				result.WriteByte(':')
			case 'u':
				// Unicode escape sequence \uXXXX
				if i+5 < len(s) {
					if code, err := strconv.ParseInt(s[i+2:i+6], 16, 32); err == nil {
						result.WriteRune(rune(code))
						i += 4 // Skip the next 4 characters
					} else {
						result.WriteByte(s[i+1])
					}
				} else {
					result.WriteByte(s[i+1])
				}
			default:
				result.WriteByte(s[i+1])
			}
			i++ // Skip the escaped character
		} else {
			result.WriteByte(s[i])
		}
	}
	return result.String()
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

// convertValue attempts to convert string values to appropriate types
func (p *PropertiesParser) convertValue(value string) interface{} {
	// Try boolean
	if value == "true" || value == "false" {
		return value == "true"
	}

	// Try integer
	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		return intVal
	}

	// Try float
	if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
		return floatVal
	}

	// Return as string
	return value
}

// GetProperties returns the flat properties map
func (p *PropertiesParser) GetProperties() map[string]string {
	return p.properties
}


// NativeToYAML converts Properties data to YAML format.
// Comments are preserved as $comment$ keys. Key order from the source is preserved.
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
			doc.Set(text, splitPath(flatHead)...)
			delete(comments, flatHead)
		}

		// Add data value
		doc.Set(parser.convertValue(parser.properties[flatKey]), parts...)

		// Add line comment
		lineCK := yamlkit.CommentKey(yamlkit.CommentLine, targetKey)
		flatLine := flatCommentPath(parentPath, lineCK)
		if text, ok := comments[flatLine]; ok {
			doc.Set(text, splitPath(flatLine)...)
			delete(comments, flatLine)
		}
	}

	// Add any remaining comments (e.g., trailing foot comments)
	for path, text := range comments {
		doc.Set(text, splitPath(path)...)
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
