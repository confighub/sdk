// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package inikit is used to interpret AppConfig/INI configuration units.
package inikit

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/go-ini/ini"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type INIResourceProviderType struct {
	yamlkit.ResourceProviderRegistry
}

// NewINIResourceProvider creates a new INIResourceProviderType with its own path registry.
func NewINIResourceProvider() *INIResourceProviderType {
	return &INIResourceProviderType{
		ResourceProviderRegistry: yamlkit.NewResourceProviderRegistry(workerapi.ToolchainAppConfigINI),
	}
}

// DefaultResourceCategory returns the default resource category to assume, which is AppConfig in this case.
func (*INIResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryAppConfig
}

// ResourceCategoryGetter just returns ResourceCategoryAppConfig for INI documents.
func (*INIResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
	// TODO: check that the document is non-empty?
	return api.ResourceCategoryAppConfig, nil
}

const (
	ResourceTypeNoSchema = api.ResourceType("NoSchema")
	ResourceNameNoName   = api.ResourceName("NoName")
	ConfigSchemaPath     = api.ResolvedPath("configHub.configSchema")
	ConfigNamePath       = api.ResolvedPath("configHub.configName")
)

// ResourceTypeGetter extracts the property configHub.configSchema, and returns NoSchema if not present.
func (*INIResourceProviderType) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
	// TODO: Decide how to use this. It would be useful to be able to distinguish different
	// app schemas from one another.
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
func (*INIResourceProviderType) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	// TODO: Decide how to use this. It would be useful to be able to distinguish different
	// files for different purposes from one another.
	name, hasName, err := yamlkit.YamlSafePathGetValue[string](doc, ConfigNamePath, true)
	if err != nil {
		return "", err
	}
	if hasName {
		return api.ResourceName(name), nil
	}
	return ResourceNameNoName, nil
}

func (*INIResourceProviderType) ScopelessResourceNamePath() api.ResolvedPath {
	return ConfigNamePath
}

func (*INIResourceProviderType) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, string(ConfigNamePath))
	return err
}

func (rp *INIResourceProviderType) ResourceNameStableCoreGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	resourceNameStableCorePath := rp.ContextPath(constants.ResourceNameStableCoreKeySuffix)
	name, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceNameStableCorePath), true)
	if err != nil || !found {
		return "", err
	}
	return api.ResourceName(name), nil
}

func (*INIResourceProviderType) TypeDescription() string {
	return "Schema"
}

const nameSeparatorString = ""

func (*INIResourceProviderType) NormalizeName(name string) string {
	// Virtually all characters are valid
	return name
}

func (*INIResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

const (
	contextPathPrefix = "configHub."
)

func (*INIResourceProviderType) ContextPath(contextField string) string {
	return contextPathPrefix + yamlkit.LowerFirst(contextField)
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func (rp *INIResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, rp)
}

func (*INIResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	return resourceName
}

// RemoveAPIVersionFromResourceType removes the version from a configuration schema, such as
// v1alpha3 from registrybot.confighub.com/v1alpha3 (see yamlkit.RemoveSchemaVersionSegments).
func (*INIResourceProviderType) RemoveAPIVersionFromResourceType(resourceType api.ResourceType) api.ResourceType {
	return yamlkit.RemoveSchemaVersionSegments(resourceType)
}

func (rp *INIResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	return api.SameResourceType(rp, resourceTypeA, resourceTypeB)
}

// ValuesAreText reports that every INI value is text, which functions
// that get and set ints and bools read and write (see
// yamlkit.TextValuedResourceProvider).
func (*INIResourceProviderType) ValuesAreText() bool {
	return true
}

func (*INIResourceProviderType) DataType() api.DataType {
	return api.DataTypeINI
}

func (*INIResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainAppConfigINI
}

// NativeToYAML converts INI data to YAML format using native libraries.
// Comments from the INI source are preserved as $comment$ map keys in the output.
// Every value is a string, since INI has no types (see
// yamlkit.TextValuedResourceProvider).
// Key order from the INI source is preserved by building a gaby YamlDoc directly.
func (*INIResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	// Parse INI data (go-ini preserves section and key order)
	cfg, err := ini.Load(data)
	if err != nil {
		return nil, fmt.Errorf("error parsing INI: %w", err)
	}

	// Extract comments from raw INI source
	comments := extractINIComments(data)

	// Build a gaby YamlDoc directly, preserving source key order.
	doc := gaby.New()

	for _, section := range cfg.Sections() {
		sectionName := section.Name()

		if sectionName != ini.DefaultSection {
			// Add section head comment if present
			parts := strings.Split(sectionName, ".")
			targetKey := parts[len(parts)-1]
			parentPath := strings.Join(parts[:len(parts)-1], ".")
			sectionHeadKey := yamlkit.CommentKey(yamlkit.CommentHead, targetKey)
			if text, ok := comments[flatCommentPath(parentPath, sectionHeadKey)]; ok {
				if err := yamlkit.SetPath(doc, text, appendPath(parentPath, sectionHeadKey)...); err != nil {
					return nil, err
				}
				delete(comments, flatCommentPath(parentPath, sectionHeadKey))
			}
		}

		for _, key := range section.Keys() {
			value := key.Value()

			var dataPath []string
			var commentParentPath string
			if sectionName == ini.DefaultSection {
				dataPath = []string{key.Name()}
				commentParentPath = ""
			} else {
				dataPath = append(strings.Split(sectionName, "."), key.Name())
				commentParentPath = sectionName
			}

			// Add head comment for this key
			headKey := yamlkit.CommentKey(yamlkit.CommentHead, key.Name())
			flatHead := flatCommentPath(commentParentPath, headKey)
			if text, ok := comments[flatHead]; ok {
				if err := yamlkit.SetPath(doc, text, appendPath(commentParentPath, headKey)...); err != nil {
					return nil, err
				}
				delete(comments, flatHead)
			}

			// Add the data value
			if err := yamlkit.SetPath(doc, value, dataPath...); err != nil {
				return nil, err
			}

			// Add line comment for this key
			lineKey := yamlkit.CommentKey(yamlkit.CommentLine, key.Name())
			flatLine := flatCommentPath(commentParentPath, lineKey)
			if text, ok := comments[flatLine]; ok {
				if err := yamlkit.SetPath(doc, text, appendPath(commentParentPath, lineKey)...); err != nil {
					return nil, err
				}
				delete(comments, flatLine)
			}
		}
	}

	// Add any remaining comments (e.g., trailing foot comments)
	for path, text := range comments {
		if err := yamlkit.SetPath(doc, text, strings.Split(path, ".")...); err != nil {
			return nil, err
		}
	}

	return doc.Bytes(), nil
}

// flatCommentPath builds a dot-separated comment path from a parent path and comment key.
func flatCommentPath(parentPath, commentKey string) string {
	if parentPath != "" {
		return parentPath + "." + commentKey
	}
	return commentKey
}

// appendPath splits a parent path and appends a key, returning the path hierarchy.
func appendPath(parentPath, key string) []string {
	if parentPath != "" {
		return append(strings.Split(parentPath, "."), key)
	}
	return []string{key}
}

// addINISection adds the keys of a mapping to the INI section named
// sectionName, or to the default section if it is empty, and its nested
// mappings as sections named with dots, preserving key order. INI has no
// lists, so a sequence is an error.
func addINISection(cfg *ini.File, sectionName string, node *yaml.Node) error {
	content, err := gaby.MappingEntries(node)
	if err != nil {
		return err
	}
	var section *ini.Section
	for i := 0; i+1 < len(content); i += 2 {
		key := gaby.ResolveAlias(content[i]).Value
		if yamlkit.IsCommentKey(key) {
			continue
		}
		value := gaby.ResolveAlias(content[i+1])
		path := key
		if sectionName != "" {
			path = sectionName + "." + key
		}
		switch value.Kind {
		case yaml.MappingNode:
			if err := addINISection(cfg, path, value); err != nil {
				return err
			}
		case yaml.SequenceNode:
			return fmt.Errorf("%s is a list, which INI can't represent", path)
		default:
			if section == nil {
				name := sectionName
				if name == "" {
					name = ini.DefaultSection
				}
				if section, err = cfg.NewSection(name); err != nil {
					return fmt.Errorf("error creating section %s: %w", name, err)
				}
			}
			if _, err := section.NewKey(key, yamlkit.ScalarText(value)); err != nil {
				return fmt.Errorf("error creating key %s: %w", path, err)
			}
		}
	}
	return nil
}

// YAMLToNative converts YAML data to INI format using native libraries.
// If the YAML contains $comment$ map keys, they are converted back to INI comments.
// Key order from the YAML source is preserved, and each value is written as
// its YAML text, or as nothing for null (see yamlkit.ScalarText).
func (*INIResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return []byte{}, nil
	}

	doc, err := gaby.ParseYAML(yamlData)
	if err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	_, comments := yamlkit.ExtractCommentsFromData(doc.DataOrdered())

	cfg := ini.Empty()
	root := gaby.ResolveAlias(doc.YNode())
	switch {
	case root.Kind == yaml.MappingNode:
		if err := addINISection(cfg, "", root); err != nil {
			return nil, err
		}
	case root.Kind == yaml.ScalarNode && root.ShortTag() == yaml.NodeTagNull:
	default:
		return nil, fmt.Errorf("an INI document has to be a mapping of keys and sections")
	}

	var output bytes.Buffer
	if _, err := cfg.WriteTo(&output); err != nil {
		return nil, fmt.Errorf("error converting YAML to INI: %w", err)
	}

	return injectINIComments(output.Bytes(), comments), nil
}
