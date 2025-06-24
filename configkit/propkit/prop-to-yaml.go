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

	"gopkg.in/yaml.v3"
)

// PropertiesParser handles parsing of Java Properties files
type PropertiesParser struct {
	properties map[string]string
}

// NewPropertiesParser creates a new parser instance
func NewPropertiesParser() *PropertiesParser {
	return &PropertiesParser{
		properties: make(map[string]string),
	}
}

// ParseProperties reads and parses Java Properties from byte slice
func (p *PropertiesParser) ParseProperties(propertiesData []byte) error {
	// Clear existing properties
	p.properties = make(map[string]string)

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

// hasNumericSegment checks if a key contains numeric segments (indicating arrays)
func hasNumericSegment(key string) bool {
	parts := strings.Split(key, ".")
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err == nil {
			return true
		}
	}
	return false
}

func (*PropertiesResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	// Parse properties config
	parser := NewPropertiesParser()
	if err := parser.ParseProperties(data); err != nil {
		return []byte{}, err
	}

	nestedData := parser.ToNestedMap()

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(nestedData); err != nil {
		return nil, fmt.Errorf("error encoding YAML: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("error closing YAML encoder: %w", err)
	}

	return buf.Bytes(), nil
}
