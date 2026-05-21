// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package appconfig

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/confighub/sdk/configkit/envkit"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
)

// render-configmap renders the AppConfig unit's data as a Kubernetes ConfigMap
// YAML document. It is non-mutating: the input data is returned unchanged and
// the rendered ConfigMap is returned as YAML output for use by callers that
// publish it elsewhere (e.g., an Upsert link).
//
// The function intentionally mirrors the bridge implementation in
// public/bridge-impl/configmap so that both code paths produce equivalent
// ConfigMaps. The bridge is retained while the new path bakes in.
func registerRenderConfigMap(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("render-configmap", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "render-configmap",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "immutable",
					Required:      false,
					Description:   "If true (default), produces a hashed-name immutable ConfigMap. If false, a stable-name mutable ConfigMap. Replaces the bridge's RevisionHistoryLimit option (0 == false).",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
				{
					ParameterName: "as-key-value",
					Required:      false,
					Description:   "AppConfig/Env only: render each environment variable as a separate ConfigMap data entry instead of a single file entry, enabling envFrom injection into pods.",
					DataType:      api.DataTypeBool,
					Example:       "false",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "configmap",
				Description: "The rendered Kubernetes ConfigMap as a YAML document.",
				OutputType:  api.OutputTypeYAML,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Renders the AppConfig unit's data as a Kubernetes ConfigMap YAML document. Intended to be used as the transform invocation on an Upsert Link whose downstream unit is Kubernetes/YAML.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return fnRenderConfigMap(converter, resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func fnRenderConfigMap(
	converter configkit.ConfigConverter,
	resourceProvider yamlkit.ResourceProvider,
	functionContext *api.FunctionContext,
	parsedData gaby.Container,
	args []api.FunctionArgument,
) (gaby.Container, any, error) {
	// Look up optional args by ParameterName since either may be omitted and
	// callers may pass them in any order.
	immutable := true
	asKeyValue := false
	for i := range args {
		switch args[i].ParameterName {
		case "immutable":
			if b, ok := args[i].Value.(bool); ok {
				immutable = b
			}
		case "as-key-value":
			if b, ok := args[i].Value.(bool); ok {
				asKeyValue = b
			}
		}
	}

	toolchain := functionContext.ToolchainType

	// Look up configName before stripping configHub. Use the resource provider's
	// configHub context prefix so the lookup follows the same convention as the
	// bridge.
	prefix := contextPrefix(resourceProvider)
	configName := getStringAtPath(parsedData, prefix+"."+configNameField)
	if configName == "" {
		configName = functionContext.UnitSlug
	}

	// Strip the configHub metadata block on a clone of parsedData. We must not
	// mutate the input — the function is declared non-mutating.
	strippedYAML, err := stripConfigHubAndSerializeNative(converter, parsedData, prefix)
	if err != nil {
		return parsedData, nil, fmt.Errorf("render-configmap: %w", err)
	}

	// The namespace is resolved at the ConfigMap unit level via links, not
	// from the AppConfig data. Always use the placeholder; the downstream
	// Needs/Provides resolution replaces it.
	namespace := yamlkit.PlaceHolderBlockApplyString

	configMapFormat := constants.ConfigMapFormatFile
	if asKeyValue && toolchain == workerapi.ToolchainAppConfigEnv {
		configMapFormat = constants.ConfigMapFormatKeyValue
	}

	// Use the (already-stripped) native data as the hash basis so the hash is
	// stable across the bridge and the function for identical inputs.
	hashSuffix := k8skit.K8sNormalizeName(truncateString(fmt.Sprintf("%x", sha256.Sum256(strippedYAML)), 10))
	namePrefix := k8skit.K8sNormalizeName(functionContext.UnitSlug)

	name := namePrefix
	if immutable {
		name = namePrefix + "-" + hashSuffix
	}

	metadata := &configMapMetadataArgs{
		Name:            name,
		Namespace:       namespace,
		UnitID:          functionContext.UnitID.String(),
		UnitSlug:        functionContext.UnitSlug,
		SpaceID:         functionContext.SpaceID.String(),
		RevisionNum:     fmt.Sprintf("%d", functionContext.RevisionNum),
		StableCore:      namePrefix,
		ResourceMergeID: uuid.New().String(),
		ConfigMapFormat: configMapFormat,
		Hash:            hashSuffix,
		Immutable:       immutable,
	}

	var rendered []byte
	if configMapFormat == constants.ConfigMapFormatKeyValue {
		rendered, err = generateKeyValueConfigMap(metadata, strippedYAML)
		if err != nil {
			return parsedData, nil, fmt.Errorf("render-configmap: %w", err)
		}
	} else {
		fileExtension := getFileExtensionForToolchain(toolchain)
		dataName := configName
		if filepath.Ext(configName) == "" {
			dataName += fileExtension
		}
		// Indent native data into the YAML block scalar
		rendered = generateFileConfigMap(metadata, dataName, string(strippedYAML))
	}

	return parsedData, api.YAMLPayload{Payload: string(rendered)}, nil
}

// contextPrefix returns the configHub metadata path prefix (without trailing dot)
// for the toolchain, derived from its ResourceProvider.
func contextPrefix(rp yamlkit.ResourceProvider) string {
	return strings.TrimSuffix(rp.ContextPath(""), ".")
}

const configNameField = "configName"

// getStringAtPath returns the string value at the given dot path in the first
// document of parsedData, or "" if not found.
func getStringAtPath(parsedData gaby.Container, path string) string {
	if len(parsedData) == 0 || parsedData[0] == nil {
		return ""
	}
	node := parsedData[0].Path(path)
	if node == nil {
		return ""
	}
	if s, ok := node.Data().(string); ok {
		return s
	}
	return ""
}

// stripConfigHubAndSerializeNative removes the configHub metadata block from a
// deep clone of parsedData and returns the result serialized as native bytes
// (the format the workload consumes). The original parsedData is untouched.
func stripConfigHubAndSerializeNative(converter configkit.ConfigConverter, parsedData gaby.Container, prefix string) ([]byte, error) {
	if len(parsedData) == 0 {
		return nil, nil
	}
	// Round-trip through YAML to clone, since gaby.YamlDoc has no public DeepCopy.
	yamlBytes := []byte(parsedData.String())
	clone, err := gaby.ParseAll(yamlBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to clone parsed data: %w", err)
	}
	for _, doc := range clone {
		if doc == nil {
			continue
		}
		// Ignore "not present" errors — DeleteP may return one if the path is missing.
		_ = doc.DeleteP(prefix)
	}
	native, err := converter.YAMLToNative([]byte(clone.String()))
	if err != nil {
		return nil, fmt.Errorf("failed to convert to native: %w", err)
	}
	return native, nil
}

// --- Below this line: byte-level ConfigMap rendering, duplicated from the
// bridge in public/bridge-impl/configmap so the bridge can keep running side
// by side. Keep these two implementations equivalent.

const configMapMetadataTemplate = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    confighub.com/UnitID: id{{.UnitID}}
  annotations:
    confighub.com/UnitSlug: {{.UnitSlug}}
    confighub.com/SpaceID: {{.SpaceID}}
    confighub.com/RevisionNum: "{{.RevisionNum}}"
    confighub.com/ResourceNameStableCore: {{.StableCore}}
    confighub.com/ResourceMergeID: {{.ResourceMergeID}}
    confighub.com/ConfigMapFormat: {{.ConfigMapFormat}}
    confighub.com/Hash: "{{.Hash}}"
    confighub.com/MutationOptions: MatchByIDOnly
    confighub.com/RenderRevision: Latest
{{- if .Immutable}}
immutable: true
{{- end}}
data:
`

const configMapFileDataTemplate = `  {{.DataName}}: |
{{.ConfigData}}
`

const configMapKeyValueDataTemplate = `{{.ConfigData}}`

type configMapMetadataArgs struct {
	Name            string
	Namespace       string
	UnitID          string
	UnitSlug        string
	SpaceID         string
	RevisionNum     string
	StableCore      string
	ResourceMergeID string
	ConfigMapFormat string
	Hash            string
	Immutable       bool
}

type configMapDataArgs struct {
	DataName   string
	ConfigData string
}

func executeTemplate(name, templateString string, args any) ([]byte, error) {
	tmpl, err := template.New(name).Parse(templateString)
	if err != nil {
		return nil, fmt.Errorf("template %s parse: %w", name, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, args); err != nil {
		return nil, fmt.Errorf("template %s execute: %w", name, err)
	}
	return out.Bytes(), nil
}

func generateFileConfigMap(metadata *configMapMetadataArgs, dataName, configData string) []byte {
	// Indent the config text for the YAML block scalar
	indented := "    " + strings.TrimSuffix(strings.ReplaceAll(configData, "\n", "\n    "), "    ")
	metaOut, _ := executeTemplate("metadata", configMapMetadataTemplate, metadata)
	dataOut, _ := executeTemplate("fileData", configMapFileDataTemplate, &configMapDataArgs{
		DataName:   dataName,
		ConfigData: indented,
	})
	return append(metaOut, dataOut...)
}

func generateKeyValueConfigMap(metadata *configMapMetadataArgs, envData []byte) ([]byte, error) {
	entries, err := envkit.ParseEnvEntries(envData)
	if err != nil {
		return nil, fmt.Errorf("error parsing env data for ConfigMap: %w", err)
	}
	var buf bytes.Buffer
	for _, entry := range entries {
		fmt.Fprintf(&buf, "  %s: %q\n", entry.Key, entry.Value)
	}
	metaOut, err := executeTemplate("metadata", configMapMetadataTemplate, metadata)
	if err != nil {
		return nil, err
	}
	dataOut, err := executeTemplate("keyValueData", configMapKeyValueDataTemplate, &configMapDataArgs{
		ConfigData: buf.String(),
	})
	if err != nil {
		return nil, err
	}
	return append(metaOut, dataOut...), nil
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func getFileExtensionForToolchain(toolchain workerapi.ToolchainType) string {
	configFormat := strings.ToLower(strings.TrimPrefix(string(toolchain), "AppConfig/"))
	if configFormat == "" {
		return ".config"
	}
	if configFormat == "text" {
		return ".txt"
	}
	return "." + configFormat
}
