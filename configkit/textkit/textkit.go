// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package textkit is used to interpret AppConfig/Text configuration units.
// Text files are represented as a single multi-line YAML string under a "text" key.
// Line-level three-way merge is handled by the Patch field on MutationInfo in the
// core mutation system, which computes and applies line-level diffs for multi-line
// string values.
package textkit

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
	"gopkg.in/yaml.v3"
)

type TextResourceProviderType struct {
	yamlkit.ResourceProviderRegistry
}

// NewTextResourceProvider creates a new TextResourceProviderType with its own path registry.
func NewTextResourceProvider() *TextResourceProviderType {
	return &TextResourceProviderType{
		ResourceProviderRegistry: yamlkit.NewResourceProviderRegistry(),
	}
}

func (*TextResourceProviderType) MergeKeysForPath(_ api.ResourceType, _ string) ([]string, bool) {
	return nil, false
}

func (*TextResourceProviderType) IsMapKeyPath(_ api.ResourceType, _ string) bool {
	return false
}

// DefaultResourceCategory returns the default resource category to assume, which is AppConfig in this case.
func (*TextResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryAppConfig
}

// ResourceCategoryGetter just returns ResourceCategoryAppConfig for Text documents.
func (*TextResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
	return api.ResourceCategoryAppConfig, nil
}

const (
	ResourceTypeNoSchema = api.ResourceType("NoSchema")
	ResourceNameNoName   = api.ResourceName("NoName")
	ConfigSchemaPath     = api.ResolvedPath(contextPathPrefix + "configSchema")
	ConfigNamePath       = api.ResolvedPath(contextPathPrefix + "configName")
)

// ResourceTypeGetter extracts the property configHub.configSchema, and returns NoSchema if not present.
func (*TextResourceProviderType) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
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
func (*TextResourceProviderType) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	name, hasName, err := yamlkit.YamlSafePathGetValue[string](doc, ConfigNamePath, true)
	if err != nil {
		return "", err
	}
	if hasName {
		return api.ResourceName(name), nil
	}
	return ResourceNameNoName, nil
}

func (*TextResourceProviderType) ScopelessResourceNamePath() api.ResolvedPath {
	return ConfigNamePath
}

func (*TextResourceProviderType) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, string(ConfigNamePath))
	return err
}

func (rp *TextResourceProviderType) ResourceNameStableCoreGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	resourceNameStableCorePath := rp.ContextPath(constants.ResourceNameStableCoreKeySuffix)
	name, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceNameStableCorePath), true)
	if err != nil || !found {
		return "", err
	}
	return api.ResourceName(name), nil
}

func (*TextResourceProviderType) TypeDescription() string {
	return "Schema"
}

const nameSeparatorString = ""

func (*TextResourceProviderType) NormalizeName(name string) string {
	return name
}

func (*TextResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

const contextPathPrefix = "frontMatter.configHub."

func (*TextResourceProviderType) ContextPath(contextField string) string {
	return contextPathPrefix + yamlkit.LowerFirst(contextField)
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from names to categories+types and categories+types to names.
func (rp *TextResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, rp)
}

func (*TextResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	return resourceName
}

func (*TextResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	return resourceTypeA == resourceTypeB
}

func (*TextResourceProviderType) DataType() api.DataType {
	return api.DataTypeText
}

func (*TextResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainAppConfigText
}

// TextDocument represents the YAML structure for a text file.
// FrontMatter holds metadata (including ConfigHub metadata under a "configHub"
// key) that is encoded as standard YAML frontmatter (delimited by "---") in
// the native text format.
type TextDocument struct {
	FrontMatter map[string]any `yaml:"frontMatter,omitempty"`
	Text        string         `yaml:"text"`
}

// frontMatterSeparator is the standard YAML frontmatter delimiter.
const frontMatterSeparator = "---\n"

// parseFrontMatter detects and parses YAML frontmatter from text data.
// Frontmatter must start with "---\n" on the first line and end with "---\n".
// Returns the parsed frontmatter map, the remaining body text, and whether
// frontmatter was found.
func parseFrontMatter(text string) (map[string]any, string, bool) {
	if !strings.HasPrefix(text, frontMatterSeparator) {
		return nil, text, false
	}

	// Find the closing separator (skip the opening "---\n")
	rest := text[len(frontMatterSeparator):]
	endIdx := strings.Index(rest, frontMatterSeparator)
	if endIdx < 0 {
		return nil, text, false
	}

	fmData := rest[:endIdx]
	body := rest[endIdx+len(frontMatterSeparator):]

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(fmData), &fm); err != nil {
		return nil, text, false
	}

	return fm, body, true
}

// NativeToYAML converts plain text data to YAML format.
// The text content is stored as a single multi-line string under a "text" key.
// If the text has YAML frontmatter (delimited by "---"), it is parsed and
// stored under the "configHub" key in the YAML representation.
func (*TextResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	doc := TextDocument{
		Text: string(data),
	}

	// Autodetect and parse YAML frontmatter
	if fm, body, ok := parseFrontMatter(string(data)); ok {
		doc.FrontMatter = fm
		doc.Text = body
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("error encoding YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("error closing YAML encoder: %w", err)
	}

	return buf.Bytes(), nil
}

// YAMLToNative converts YAML data back to plain text format.
// It extracts the "text" string value. If ConfigHub metadata (FrontMatter)
// is present, it is encoded as standard YAML frontmatter (delimited by "---")
// prepended to the text content.
func (*TextResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return []byte{}, nil
	}

	var doc TextDocument
	if err := yaml.Unmarshal(yamlData, &doc); err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	if len(doc.FrontMatter) > 0 {
		var buf bytes.Buffer
		buf.WriteString(frontMatterSeparator)
		encoder := yaml.NewEncoder(&buf)
		encoder.SetIndent(2)
		if err := encoder.Encode(doc.FrontMatter); err != nil {
			return nil, fmt.Errorf("error encoding frontmatter: %w", err)
		}
		if err := encoder.Close(); err != nil {
			return nil, fmt.Errorf("error closing frontmatter encoder: %w", err)
		}
		buf.WriteString(frontMatterSeparator)
		buf.WriteString(doc.Text)
		return buf.Bytes(), nil
	}

	return []byte(doc.Text), nil
}
