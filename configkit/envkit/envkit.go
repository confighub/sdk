// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package envkit is used to interpret AppConfig/Env configuration units.
package envkit

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/confighub/sdk/core/third_party/gaby"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type EnvResourceProviderType struct {
	yamlkit.ResourceProviderRegistry
}

// NewEnvResourceProvider creates a new EnvResourceProviderType with its own path registry.
func NewEnvResourceProvider() *EnvResourceProviderType {
	return &EnvResourceProviderType{
		ResourceProviderRegistry: yamlkit.NewResourceProviderRegistry(),
	}
}

func (*EnvResourceProviderType) MergeKeysForPath(_ api.ResourceType, _ string) ([]string, bool) {
	return nil, false
}

// ExclusiveFieldsForPath returns no union: this format has no schema declaring one.
func (*EnvResourceProviderType) ExclusiveFieldsForPath(_ api.ResourceType, _ string) (yamlkit.ExclusiveFields, bool) {
	return yamlkit.ExclusiveFields{}, false
}

func (*EnvResourceProviderType) IsMapKeyPath(_ api.ResourceType, _ string) bool {
	return false
}

// DefaultResourceCategory returns the default resource category to assume, which is AppConfig in this case.
func (*EnvResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryAppConfig
}

// ResourceCategoryGetter just returns ResourceCategoryAppConfig for Env documents.
func (*EnvResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
	return api.ResourceCategoryAppConfig, nil
}

const (
	ResourceTypeNoSchema = api.ResourceType("NoSchema")
	ResourceNameNoName   = api.ResourceName("NoName")
	ConfigSchemaPath     = api.ResolvedPath("configHub.configSchema")
	ConfigNamePath       = api.ResolvedPath("configHub.configName")
)

// ResourceTypeGetter extracts the property configHub.configSchema, and returns NoSchema if not present.
func (*EnvResourceProviderType) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
	schemaType, hasSchema, err := yamlkit.YamlSafePathGetValue[string](doc, ConfigSchemaPath, true)
	if err != nil {
		return "", err
	}
	if hasSchema {
		return api.ResourceType(schemaType), nil
	}
	return ResourceTypeNoSchema, nil
}

// ResourceNameGetter extracts the property configHub.configName, and returns NoName if not present.
func (*EnvResourceProviderType) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	name, hasName, err := yamlkit.YamlSafePathGetValue[string](doc, ConfigNamePath, true)
	if err != nil {
		return "", err
	}
	if hasName {
		return api.ResourceName(name), nil
	}
	return ResourceNameNoName, nil
}

func (*EnvResourceProviderType) ScopelessResourceNamePath() api.ResolvedPath {
	return ConfigNamePath
}

func (*EnvResourceProviderType) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, string(ConfigNamePath))
	return err
}

func (rp *EnvResourceProviderType) ResourceNameStableCoreGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	resourceNameStableCorePath := rp.ContextPath(constants.ResourceNameStableCoreKeySuffix)
	name, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceNameStableCorePath), true)
	if err != nil || !found {
		return "", err
	}
	return api.ResourceName(name), nil
}


func (*EnvResourceProviderType) TypeDescription() string {
	return "Schema"
}

const nameSeparatorString = ""

func (*EnvResourceProviderType) NormalizeName(name string) string {
	return name
}

func (*EnvResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

const (
	contextPathPrefx = "configHub."
)

func (*EnvResourceProviderType) ContextPath(contextField string) string {
	return contextPathPrefx + yamlkit.LowerFirst(contextField)
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func (rp *EnvResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, rp)
}

func (*EnvResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	return resourceName
}

func (*EnvResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	return resourceTypeA == resourceTypeB
}

func (*EnvResourceProviderType) DataType() api.DataType {
	return api.DataTypeEnv
}

func (*EnvResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainAppConfigEnv
}

// EnvEntry represents a single key-value pair from an env file.
type EnvEntry struct {
	Key   string
	Value string
}

// ParseEnvEntries parses env file data into key-value string pairs, skipping
// empty lines, comments, and configHub metadata keys. Values are kept as raw strings.
func ParseEnvEntries(data []byte) ([]EnvEntry, error) {
	var entries []EnvEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eqIndex := strings.Index(line, "=")
		if eqIndex == -1 {
			return nil, fmt.Errorf("error parsing line %d: no '=' separator found", lineNum)
		}
		key := strings.TrimSpace(line[:eqIndex])
		value := strings.TrimSpace(line[eqIndex+1:])
		value = stripQuotes(value)
		if key == "" {
			continue
		}
		// Skip configHub metadata keys
		if strings.HasPrefix(key, contextPathPrefx) {
			continue
		}
		entries = append(entries, EnvEntry{Key: key, Value: value})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env data: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}

// NativeToYAML converts env file data (KEY=value lines) to YAML format.
// Env files use a flat KEY=value format. Non-configHub keys must not contain
// dots, since shells such as bash do not support dots in environment variable
// names. Keys under the configHub metadata prefix (e.g., configHub.configName)
// are allowed to contain dots because they are removed before rendering.
// Key order from the source file is preserved.
func (*EnvResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	// Extract comments from raw env data
	comments := extractEnvComments(data)

	// Build a gaby YamlDoc directly, preserving source key order.
	doc := gaby.New()

	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Find the first = separator
		eqIndex := strings.Index(line, "=")
		if eqIndex == -1 {
			return nil, fmt.Errorf("error parsing line %d: no '=' separator found", lineNum)
		}

		key := strings.TrimSpace(line[:eqIndex])
		value := strings.TrimSpace(line[eqIndex+1:])

		// Strip surrounding quotes from value
		value = stripQuotes(value)

		// Strip inline comments from unquoted values
		cleanValue, _ := yamlkit.SplitInlineComment(value)
		value = cleanValue

		if key == "" {
			continue
		}

		// configHub metadata keys may contain dots for nesting; all other keys must not.
		if strings.Contains(key, ".") && !strings.HasPrefix(key, contextPathPrefx) {
			return nil, fmt.Errorf("error parsing line %d: key %q contains dots, which are not valid in environment variable names; only keys under the %q prefix may use dots", lineNum, key, strings.TrimSuffix(contextPathPrefx, "."))
		}

		parts := strings.Split(key, ".")
		targetKey := parts[len(parts)-1]
		parentPath := strings.Join(parts[:len(parts)-1], ".")

		// Add head comment
		headCK := yamlkit.CommentKey(yamlkit.CommentHead, targetKey)
		flatHead := flatCommentPath(parentPath, headCK)
		if text, ok := comments[flatHead]; ok {
			doc.Set(text, splitPath(flatHead)...)
			delete(comments, flatHead)
		}

		// Env values are always strings — shells don't have typed variables.
		doc.Set(value, parts...)

		// Add line comment
		lineCK := yamlkit.CommentKey(yamlkit.CommentLine, targetKey)
		flatLine := flatCommentPath(parentPath, lineCK)
		if text, ok := comments[flatLine]; ok {
			doc.Set(text, splitPath(flatLine)...)
			delete(comments, flatLine)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env data: %w", err)
	}

	// Add any remaining comments (e.g., trailing foot comments)
	for path, text := range comments {
		doc.Set(text, splitPath(path)...)
	}

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

// YAMLToNative converts YAML data to env file format (KEY=value lines).
// If the YAML contains $comment$ map keys, they are converted back to # comments.
// Key order from the YAML source is preserved.
func (*EnvResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return []byte{}, nil
	}

	// Parse YAML with gaby to preserve key order
	doc, err := gaby.ParseYAML(yamlData)
	if err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	// Extract comment keys from ordered data
	cleanData, comments := yamlkit.ExtractCommentsFromData(doc.DataOrdered())

	// Flatten to ordered key=value pairs
	var keys []string
	props := make(map[string]string)
	flattenOrdered(cleanData, "", &keys, props)

	// Validate that non-configHub keys don't contain dots (env vars can't have dots).
	for _, key := range keys {
		if strings.Contains(key, ".") && !strings.HasPrefix(key, contextPathPrefx) {
			return nil, fmt.Errorf("key %q contains dots, which are not valid in environment variable names; only keys under the %q prefix may use dots", key, strings.TrimSuffix(contextPathPrefx, "."))
		}
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	for _, key := range keys {
		value := props[key]
		// Quote values that contain spaces or special characters
		if needsQuoting(value) {
			fmt.Fprintf(writer, "%s=\"%s\"\n", key, escapeValue(value))
		} else {
			fmt.Fprintf(writer, "%s=%s\n", key, value)
		}
	}
	if err := writer.Flush(); err != nil {
		return nil, fmt.Errorf("error flushing buffer: %w", err)
	}

	// Re-inject comments into env output
	result := injectEnvComments(buf.Bytes(), comments)

	return result, nil
}

// stripQuotes removes surrounding single or double quotes from a value.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// flattenOrdered recursively flattens nested structures into dot-notation keys,
// preserving order from ordered maps. Keys are appended to the keys slice in order.
func flattenOrdered(data interface{}, prefix string, keys *[]string, result map[string]string) {
	switch v := data.(type) {
	case *orderedmap.OrderedMap[string, interface{}]:
		for pair := v.Oldest(); pair != nil; pair = pair.Next() {
			newKey := pair.Key
			if prefix != "" {
				newKey = prefix + "." + pair.Key
			}
			flattenOrdered(pair.Value, newKey, keys, result)
		}
	case map[string]interface{}:
		for key, value := range v {
			newKey := key
			if prefix != "" {
				newKey = prefix + "." + key
			}
			flattenOrdered(value, newKey, keys, result)
		}
	case []interface{}:
		for i, item := range v {
			newKey := strconv.Itoa(i)
			if prefix != "" {
				newKey = prefix + "." + newKey
			}
			flattenOrdered(item, newKey, keys, result)
		}
	default:
		*keys = append(*keys, prefix)
		result[prefix] = fmt.Sprintf("%v", v)
	}
}

// needsQuoting returns true if a value contains characters that need quoting.
func needsQuoting(value string) bool {
	return strings.ContainsAny(value, " \t\"'\\#$")
}

// escapeValue escapes special characters in a value for env file format.
func escapeValue(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}
