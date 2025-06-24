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

// escapeKey escapes special characters in property keys
func (c *YAMLToPropertiesConverter) escapeKey(key string) string {
	result := strings.Builder{}
	for _, char := range key {
		switch char {
		case ' ', '\t', '\f':
			result.WriteRune('\\')
			result.WriteRune(char)
		case '=', ':':
			result.WriteRune('\\')
			result.WriteRune(char)
		case '\\':
			result.WriteString("\\\\")
		case '\n':
			result.WriteString("\\n")
		case '\r':
			result.WriteString("\\r")
		default:
			if char < 32 || char > 126 {
				// Escape non-printable or non-ASCII characters
				result.WriteString(fmt.Sprintf("\\u%04X", char))
			} else {
				result.WriteRune(char)
			}
		}
	}
	return result.String()
}

// escapeValue escapes special characters in property values
func (c *YAMLToPropertiesConverter) escapeValue(value string) string {
	result := strings.Builder{}
	for _, char := range value {
		switch char {
		case '\\':
			result.WriteString("\\\\")
		case '\n':
			result.WriteString("\\n")
		case '\r':
			result.WriteString("\\r")
		case '\t':
			result.WriteString("\\t")
		case '\f':
			result.WriteString("\\f")
		default:
			if char < 32 || (char > 126 && !unicode.IsPrint(char)) {
				// Escape non-printable characters
				result.WriteString(fmt.Sprintf("\\u%04X", char))
			} else {
				result.WriteRune(char)
			}
		}
	}
	return result.String()
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

// ConvertYAMLToProperties is a convenience function for one-shot conversion
func (*PropertiesResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	converter := NewYAMLToPropertiesConverter()
	if err := converter.ParseYAML(yamlData); err != nil {
		return nil, err
	}
	return converter.ToProperties()
}
