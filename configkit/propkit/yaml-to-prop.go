// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package propkit

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/third_party/gaby"
	"gopkg.in/yaml.v3"
)

// YAMLToPropertiesConverter handles conversion from YAML to Properties
type YAMLToPropertiesConverter struct {
	properties map[string]string
}

// NewYAMLToPropertiesConverter creates a new converter instance
func NewYAMLToPropertiesConverter() *YAMLToPropertiesConverter {
	return &YAMLToPropertiesConverter{
		properties: make(map[string]string),
	}
}

// ParseYAML reads and parses YAML from byte slice
func (c *YAMLToPropertiesConverter) ParseYAML(yamlData []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(yamlData))
	var data interface{}

	if err := decoder.Decode(&data); err != nil {
		return fmt.Errorf("error parsing YAML: %w", err)
	}

	// Clear existing properties
	c.properties = make(map[string]string)

	// Flatten the YAML structure
	c.flattenMap(data, "")

	return nil
}

// ParseYAMLFile reads and parses a YAML file (convenience method)
func (c *YAMLToPropertiesConverter) ParseYAMLFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}
	return c.ParseYAML(data)
}

// flattenMap recursively flattens nested structures into dot-notation keys
func (c *YAMLToPropertiesConverter) flattenMap(data interface{}, prefix string) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			newKey := c.buildKey(prefix, key)
			c.flattenMap(value, newKey)
		}
	case map[interface{}]interface{}:
		// Handle YAML maps with non-string keys
		for key, value := range v {
			keyStr := fmt.Sprintf("%v", key)
			newKey := c.buildKey(prefix, keyStr)
			c.flattenMap(value, newKey)
		}
	case []interface{}:
		// Handle arrays
		for i, item := range v {
			newKey := c.buildKey(prefix, strconv.Itoa(i))
			c.flattenMap(item, newKey)
		}
	default:
		// Leaf value - convert to string and store
		c.properties[prefix] = c.convertToString(v)
	}
}

// buildKey constructs the dot-notation key
func (c *YAMLToPropertiesConverter) buildKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// convertToString converts various value types to their string representation
func (c *YAMLToPropertiesConverter) convertToString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		// Check if it's actually an integer
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		// Check if it's actually an integer
		if v == float32(int32(v)) {
			return strconv.Itoa(int(v))
		}
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case time.Time:
		return v.Format(time.RFC3339)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// escapeKey escapes special characters in property keys: whitespace, the
// separators = and :, a # or ! that would start a comment line, backslashes,
// and anything outside printable ASCII.
func (c *YAMLToPropertiesConverter) escapeKey(key string) string {
	result := strings.Builder{}
	for i, char := range key {
		switch char {
		case ' ', '=', ':':
			result.WriteRune('\\')
			result.WriteRune(char)
		case '#', '!':
			if i == 0 {
				result.WriteRune('\\')
			}
			result.WriteRune(char)
		default:
			if char < 32 || char > 126 {
				writeEscapedRune(&result, char)
			} else {
				writeEscapedOrRune(&result, char)
			}
		}
	}
	return result.String()
}

// escapeValue escapes special characters in property values: backslashes,
// control characters and other non-printable characters, a # after a space,
// which would start an inline comment, and leading and trailing spaces,
// which the parser would otherwise trim.
func (c *YAMLToPropertiesConverter) escapeValue(value string) string {
	result := strings.Builder{}
	end := len(strings.TrimRight(value, " "))
	var previous rune
	for i, char := range value {
		afterSpace := previous == ' ' || previous == '\t'
		previous = char
		switch {
		case char == ' ' && i >= end:
			// \u0020 rather than "\ " at the end, where the parser trims
			// the line before reading escapes.
			result.WriteString("\\u0020")
		case char == ' ' && i == 0:
			result.WriteString("\\ ")
		case char == '#' && afterSpace:
			result.WriteString("\\#")
		case char < 32 || char == 127 || (char > 126 && !unicode.IsPrint(char)):
			writeEscapedRune(&result, char)
		default:
			writeEscapedOrRune(&result, char)
		}
	}
	return result.String()
}

// writeEscapedOrRune writes a printable character, doubling a backslash.
func writeEscapedOrRune(b *strings.Builder, char rune) {
	if char == '\\' {
		b.WriteString("\\\\")
		return
	}
	b.WriteRune(char)
}

// writeEscapedRune writes a character as an escape: \t, \n, \r or \f, or
// \uXXXX, as a surrogate pair outside the Basic Multilingual Plane.
func writeEscapedRune(b *strings.Builder, char rune) {
	switch char {
	case '\t':
		b.WriteString("\\t")
	case '\n':
		b.WriteString("\\n")
	case '\r':
		b.WriteString("\\r")
	case '\f':
		b.WriteString("\\f")
	default:
		if r1, r2 := utf16.EncodeRune(char); r1 != utf8.RuneError {
			fmt.Fprintf(b, "\\u%04X\\u%04X", r1, r2)
		} else {
			fmt.Fprintf(b, "\\u%04X", char)
		}
	}
}

// ToProperties converts the parsed YAML to Properties format as byte slice
func (c *YAMLToPropertiesConverter) ToProperties() ([]byte, error) {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	// Sort keys for consistent output
	keys := make([]string, 0, len(c.properties))
	for key := range c.properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Write properties
	for _, key := range keys {
		value := c.properties[key]
		escapedKey := c.escapeKey(key)
		escapedValue := c.escapeValue(value)

		// Use = as separator
		fmt.Fprintf(writer, "%s=%s\n", escapedKey, escapedValue)
	}

	if err := writer.Flush(); err != nil {
		return nil, fmt.Errorf("error flushing buffer: %w", err)
	}

	return buf.Bytes(), nil
}

// WritePropertiesToWriter writes properties to any writer (for stdout output)
func (c *YAMLToPropertiesConverter) WritePropertiesToWriter(writer *bufio.Writer) error {
	data, err := c.ToProperties()
	if err != nil {
		return err
	}

	_, err = writer.Write(data)
	if err != nil {
		return err
	}

	return writer.Flush()
}

// GetProperties returns the flattened properties map
func (c *YAMLToPropertiesConverter) GetProperties() map[string]string {
	return c.properties
}

// YAMLToNative converts YAML data to Java Properties format.
// If the YAML contains $comment$ map keys, they are converted back to # comments.
// Key order from the YAML source is preserved, and each value is written as
// its YAML text, or as nothing for null (see yamlkit.ScalarText).
func (*PropertiesResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	doc, err := gaby.ParseYAML(yamlData)
	if err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	_, comments := yamlkit.ExtractCommentsFromData(doc.DataOrdered())

	entries, err := yamlkit.FlattenToText(doc.YNode())
	if err != nil {
		return nil, err
	}

	converter := NewYAMLToPropertiesConverter()
	var buf bytes.Buffer
	for _, entry := range entries {
		fmt.Fprintf(&buf, "%s=%s\n", converter.escapeKey(strings.Join(entry.Path, ".")), converter.escapeValue(entry.Text))
	}

	return injectPropertiesComments(buf.Bytes(), comments), nil
}
