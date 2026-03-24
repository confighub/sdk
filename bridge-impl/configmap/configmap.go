// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package configmap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"

	"github.com/confighub/sdk/bridge-impl/common"
	"github.com/confighub/sdk/bridge-impl/kubernetes"
	"github.com/confighub/sdk/configkit/envkit"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	funcimpl "github.com/confighub/sdk/function-impl"
	functionapi "github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/executor"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/worker/lib"
	"github.com/confighub/sdk/core/workerapi"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var executorOnce sync.Once
var functionExecutor *executor.ConcreteFunctionExecutor

type ConfigMapBridgeWorker struct {
	kubernetes.KubernetesBridgeWorker
}

func initFunctionExecutor() {
	executorOnce.Do(func() {
		// Just Kubernetes + AppConfig functions and resource providers
		functionExecutor = funcimpl.NewStandardExecutor([]workerapi.ToolchainType{
			workerapi.ToolchainKubernetesYAML,
			workerapi.ToolchainAppConfigProperties,
			workerapi.ToolchainAppConfigTOML,
			workerapi.ToolchainAppConfigINI,
			workerapi.ToolchainAppConfigYAML,
			workerapi.ToolchainAppConfigJSON,
			workerapi.ToolchainAppConfigEnv,
			workerapi.ToolchainAppConfigText,
		})
	})
}

func NewConfigMapBridgeWorker() *ConfigMapBridgeWorker {
	initFunctionExecutor()
	// FIXME: Note that this doesn't call NewKubernetesBridgeWorker(), because ConfigMapBridgeWorker
	// inlines KubernetesBridgeWorker rather than storing a pointer.
	return &ConfigMapBridgeWorker{}
}

var _ api.BridgeWorker = (*ConfigMapBridgeWorker)(nil)
var _ api.WatchableWorker = (*ConfigMapBridgeWorker)(nil)

// The standard annotations are:
// confighub.com/UnitSlug
// confighub.com/SpaceID

// We also add a label so we can select all ConfigMaps for this Unit

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
    confighub.com/ResourceID: {{.ResourceID}}
    confighub.com/ResourcePrefix: {{.ResourcePrefix}}
    confighub.com/MutationOptions: MatchByIDOnly
    confighub.com/RenderRevision: Latest
immutable: true
data:
`

const configMapFileDataTemplate = `  {{.DataName}}: |
{{.ConfigData}}
`

const configMapKeyValueDataTemplate = `{{.ConfigData}}`

type configMapMetadataArgs struct {
	Name           string
	Namespace      string
	UnitID         string
	UnitSlug       string
	SpaceID        string
	RevisionNum    string
	ResourceID     string
	ResourcePrefix string
}

type configMapDataArgs struct {
	DataName   string
	ConfigData string
}

// kubectl's generator is here:
// https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/kubectl/pkg/cmd/create/create_configmap.go

func executeTemplate(name, templateString string, args interface{}) []byte {
	tmpl, err := template.New(name).Parse(templateString)
	if err != nil {
		log.Log.Error(err, "ConfigMap template failed to parse", "template", name)
	}
	var out bytes.Buffer
	err = tmpl.Execute(&out, args)
	if err != nil {
		log.Log.Error(err, "ConfigMap template failed to evaluate", "template", name)
	}
	return out.Bytes()
}

func generateFileConfigMap(metadata *configMapMetadataArgs, dataName, configData string) []byte {
	// Indent the config text for the YAML block scalar
	indented := "    " + strings.TrimSuffix(strings.ReplaceAll(configData, "\n", "\n    "), "    ")
	out := executeTemplate("metadata", configMapMetadataTemplate, metadata)
	out = append(out, executeTemplate("fileData", configMapFileDataTemplate, &configMapDataArgs{
		DataName:   dataName,
		ConfigData: indented,
	})...)
	return out
}

func generateKeyValueConfigMap(metadata *configMapMetadataArgs, envData []byte) ([]byte, error) {
	entries, err := envkit.ParseEnvEntries(envData)
	if err != nil {
		return nil, fmt.Errorf("error parsing env data for ConfigMap: %w", err)
	}
	// Build YAML map with all values quoted as strings (ConfigMap data is map[string]string).
	var buf bytes.Buffer
	for _, entry := range entries {
		fmt.Fprintf(&buf, "  %s: %q\n", entry.Key, entry.Value)
	}
	out := executeTemplate("metadata", configMapMetadataTemplate, metadata)
	out = append(out, executeTemplate("keyValueData", configMapKeyValueDataTemplate, &configMapDataArgs{
		ConfigData: buf.String(),
	})...)
	return out, nil
}

func (w *ConfigMapBridgeWorker) ID() api.BridgeWorkerID {
	return api.BridgeWorkerID{
		ProviderType: api.ProviderConfigMapRenderer,
		// Support multiple AppConfig types
		ToolchainTypes: []workerapi.ToolchainType{
			workerapi.ToolchainAppConfigProperties,
			workerapi.ToolchainAppConfigTOML,
			workerapi.ToolchainAppConfigINI,
			workerapi.ToolchainAppConfigJSON,
			workerapi.ToolchainAppConfigEnv,
			workerapi.ToolchainAppConfigText,
			workerapi.ToolchainAppConfigYAML,
		},
	}
}

func (w *ConfigMapBridgeWorker) Info(opts api.InfoOptions) api.BridgeWorkerInfo {
	id := w.ID()
	supportedToolchains := id.ToolchainTypes

	// Don't create a Target for every ToolchainType. Just advertise their availability.

	var configTypes []*api.SupportedConfigType
	for _, toolchain := range supportedToolchains {
		newConfigType := &api.SupportedConfigType{
			ConfigTypeSignature: api.ConfigTypeSignature{
				ConfigType: api.ConfigType{
					ProviderType:  id.ProviderType,
					ToolchainType: toolchain,
					LiveStateType: workerapi.ToolchainKubernetesYAML,
				},
			},
			AvailableTargets: []api.Target{
				{
					BridgeHandle: "ConfigMap", // no secret needed, can be identical for all ToolchainTypes
				},
			},
		}
		// RevisionHistoryLimit controls how many immutable ConfigMap versions to retain
		// in LiveState. During rolling updates, Pods may still reference old ConfigMaps,
		// so we keep multiple versions to avoid breaking running workloads.
		newConfigType.Options = append(newConfigType.Options, api.BridgeOption{
			Name:        "RevisionHistoryLimit",
			Description: "Maximum number of immutable ConfigMap versions to retain in LiveState. Old versions are kept so that Pods referencing them during rolling updates are not disrupted. Defaults to 10 (matching the Kubernetes Deployment default).",
			Required:    false,
			DataType:    functionapi.DataTypeInt,
			Example:     "10",
		})
		// AppConfig/Env supports the AsKeyValue option for envFrom injection.
		if toolchain == workerapi.ToolchainAppConfigEnv {
			newConfigType.Options = append(newConfigType.Options, api.BridgeOption{
				Name:        "AsKeyValue",
				Description: "When true, each environment variable is rendered as a separate ConfigMap data entry instead of a single file, enabling envFrom injection into pods.",
				Required:    false,
				DataType:    functionapi.DataTypeBool,
				Example:     "true",
			})
		}
		configTypes = append(configTypes, newConfigType)
	}

	return api.BridgeWorkerInfo{
		SupportedConfigTypes: configTypes,
	}
}

// contextPrefix returns the configHub metadata path prefix (without trailing dot)
// for the given toolchain, using its ResourceProvider.
func contextPrefix(executor *executor.ConcreteFunctionExecutor, toolchain workerapi.ToolchainType) string {
	rp, ok := executor.GetResourceProvider(toolchain)
	if !ok {
		return "configHub" // fallback
	}
	return strings.TrimSuffix(rp.ContextPath(""), ".")
}

// const configSchemaField = "configSchema"
const configNameField = "configName"
const namespaceContextField = "kubernetes.namespace"

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

// defaultRevisionHistoryLimit matches the Kubernetes default for Deployment revision history.
const defaultRevisionHistoryLimit = 10

// getRevisionHistoryLimit returns the configured RevisionHistoryLimit from bridge options,
// or the default value if not set or invalid.
func getRevisionHistoryLimit(payload api.BridgeWorkerPayload) int {
	if v, ok := payload.TargetOptions["RevisionHistoryLimit"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultRevisionHistoryLimit
}

// isAsKeyValue returns whether the AsKeyValue bridge option is set to true.
func isAsKeyValue(payload api.BridgeWorkerPayload) bool {
	if v, ok := payload.TargetOptions["AsKeyValue"]; ok {
		return v == "true"
	}
	return false
}

func getConfigHubMetadataField(ctx context.Context, toolchain workerapi.ToolchainType, prefix, field string, data []byte) string {
	fieldPath := prefix + "." + field

	// Invoke get-string-path to extract namespace
	getStringPathInvocation := functionapi.FunctionInvocation{
		FunctionName: "get-string-path",
		Arguments: []functionapi.FunctionArgument{
			{Value: "*"},
			{Value: fieldPath},
		},
	}

	getStringPathRequest := &functionapi.FunctionInvocationRequest{
		FunctionContext: functionapi.FunctionContext{
			ToolchainType: toolchain,
		},
		ConfigData:          data,
		FunctionInvocations: []functionapi.FunctionInvocation{getStringPathInvocation},
	}

	getStringPathResp, err := functionExecutor.Invoke(ctx, getStringPathRequest)
	if err == nil && getStringPathResp.Success {
		// Extract namespace from AttributeValueList output
		if outputData, exists := getStringPathResp.Outputs[functionapi.OutputTypeAttributeValueList]; exists {
			var attrList functionapi.AttributeValueList
			if err := json.Unmarshal(outputData, &attrList); err == nil && len(attrList) > 0 {
				if strValue, ok := attrList[0].Value.(string); ok {
					return strValue
				}
			}
		}
	}
	return ""
}

func transformAppConfigToConfigMap(payload *api.BridgeWorkerPayload) error {
	configData := string(payload.Data)

	ctx := context.Background()

	// Get the context path prefix from the ResourceProvider for this toolchain
	prefix := contextPrefix(functionExecutor, payload.ToolchainType)

	// Extract the namespace using get-string-path function
	namespace := getConfigHubMetadataField(ctx, payload.ToolchainType, prefix, namespaceContextField, payload.Data)
	if namespace == "" {
		namespace = "default"
	}

	// The filename is referenced directly by the workload and needs to be
	// expected by the workload and only unique within a container, so use
	// the configName. Fall back to the UnitSlug if there isn't one.
	configName := getConfigHubMetadataField(ctx, payload.ToolchainType, prefix, configNameField, payload.Data)
	if configName == "" {
		configName = payload.UnitSlug
	}

	// Delete configHub properties using delete-path function
	deletePathInvocation := functionapi.FunctionInvocation{
		FunctionName: "delete-path",
		Arguments: []functionapi.FunctionArgument{
			{Value: "*"},
			{Value: prefix},
		},
	}

	deletePathRequest := &functionapi.FunctionInvocationRequest{
		FunctionContext: functionapi.FunctionContext{
			ToolchainType: payload.ToolchainType,
		},
		ConfigData:          payload.Data,
		FunctionInvocations: []functionapi.FunctionInvocation{deletePathInvocation},
	}

	deletePathResp, err := functionExecutor.Invoke(ctx, deletePathRequest)
	if err == nil && deletePathResp.Success {
		configData = string(deletePathResp.ConfigData)
	}
	// If delete-path fails, configData remains as the original string(payload.Data)

	nameSuffix := k8skit.K8sNormalizeName(truncateString(fmt.Sprintf("%x", sha256.Sum256(payload.Data)), 10))
	// The resource name needs to be unique within its Namespace. The UnitSlug
	// is likely to be more unique than the configName, it isn't referenced directly
	// by the workload, and references are replaced automatically by Needs/Provides, so use
	// the UnitSlug.
	namePrefix := k8skit.K8sNormalizeName(payload.UnitSlug)
	metadata := &configMapMetadataArgs{
		Name:           namePrefix + "-" + nameSuffix,
		Namespace:      namespace,
		UnitID:         payload.UnitID.String(),
		UnitSlug:       payload.UnitSlug,
		SpaceID:        payload.SpaceID.String(),
		RevisionNum:    fmt.Sprintf("%d", payload.RevisionNum),
		ResourceID:     uuid.New().String(),
		ResourcePrefix: namePrefix, // specify a predictable name for Needs/Provides matching
	}

	if isAsKeyValue(*payload) && payload.ToolchainType == workerapi.ToolchainAppConfigEnv {
		configMap, err := generateKeyValueConfigMap(metadata, []byte(configData))
		if err != nil {
			return err
		}
		payload.Data = configMap
	} else {
		fileExtension := getFileExtensionForToolchain(payload.ToolchainType)
		dataName := configName + fileExtension
		payload.Data = generateFileConfigMap(metadata, dataName, configData)
	}
	return nil
}

func (w *ConfigMapBridgeWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if err := transformAppConfigToConfigMap(&payload); err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		), err)
	}

	// Build a multi-version LiveState that retains up to RevisionHistoryLimit ConfigMap
	// versions. During rolling updates, Pods may still reference old ConfigMap versions
	// by name, so we keep previous versions in LiveState to prevent them from being
	// pruned by downstream appliers (e.g., ArgoCD, Flux) while still in use.
	liveState := mergeConfigMapLiveState(payload.Data, payload.LiveState, getRevisionHistoryLimit(payload))

	status := common.NewActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Rendered ConfigMap successfully at %s", time.Now().Format(time.RFC3339)),
	)
	status.LiveState = liveState
	return wctx.SendStatus(status)
}

// mergeConfigMapLiveState inserts the new ConfigMap at the top of the existing LiveState,
// removes any duplicate (by metadata.name) of the new ConfigMap from the existing entries,
// and trims the result to at most revisionHistoryLimit entries.
func mergeConfigMapLiveState(newConfigMap, existingLiveState []byte, revisionHistoryLimit int) []byte {
	newDoc, err := gaby.ParseAll(newConfigMap)
	if err != nil || len(newDoc) == 0 {
		return newConfigMap
	}

	// If there is no existing LiveState, just return the new ConfigMap.
	if len(existingLiveState) == 0 {
		return newConfigMap
	}

	existingDocs, err := gaby.ParseAll(existingLiveState)
	if err != nil {
		return newConfigMap
	}

	// Get the name of the new ConfigMap to deduplicate.
	newName := getConfigMapName(newDoc[0])

	// Path to the RenderRevision annotation (dots in "confighub.com" must be escaped).
	renderRevisionPath := "metadata.annotations." + yamlkit.EscapeDotsInPathSegment("confighub.com/RenderRevision")

	// Build the merged list: new ConfigMap first, then existing ones (excluding duplicates).
	var merged gaby.Container
	merged = append(merged, newDoc[0])
	for _, doc := range existingDocs {
		if doc == nil {
			continue
		}
		if newName != "" && getConfigMapName(doc) == newName {
			// Skip duplicate of the new ConfigMap.
			continue
		}
		// Remove the RenderRevision annotation from older ConfigMaps so that
		// only the newest version is matched by --where-resource filters.
		_ = doc.DeleteP(renderRevisionPath)
		merged = append(merged, doc)
	}

	// Trim to RevisionHistoryLimit.
	if len(merged) > revisionHistoryLimit {
		merged = merged[:revisionHistoryLimit]
	}

	return []byte(merged.String())
}

// getConfigMapName extracts metadata.name from a parsed YAML document,
// using the same path as K8sResourceProvider.ScopelessResourceNamePath ("metadata.name").
func getConfigMapName(doc *gaby.YamlDoc) string {
	if doc == nil {
		return ""
	}
	name, _, err := yamlkit.YamlSafePathGetValue[string](doc, functionapi.ResolvedPath("metadata.name"), true)
	if err != nil {
		return ""
	}
	return name
}

func (w *ConfigMapBridgeWorker) WatchForApply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// ConfigMapRenderer doesn't apply to a cluster, so there's nothing to watch
	return nil
}

func (w *ConfigMapBridgeWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// Refresh doesn't really make sense
	return lib.SafeSendStatus(wctx, common.NewActionResult(
		api.ActionStatusFailed,
		api.ActionResultRefreshFailed,
		"Refresh not supported",
	), errors.New("Refresh not supported"))
}

func (w *ConfigMapBridgeWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// Import doesn't really make sense
	return lib.SafeSendStatus(wctx, common.NewActionResult(
		api.ActionStatusFailed,
		api.ActionResultImportFailed,
		"Import not supported",
	), errors.New("Import not supported"))
}

func (w *ConfigMapBridgeWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	result := common.NewActionResult(
		api.ActionStatusCompleted,
		api.ActionResultDestroyCompleted,
		fmt.Sprintf("Destroyed successfully at %s", time.Now().Format(time.RFC3339)),
	)
	return wctx.SendStatus(result)
}

func (w *ConfigMapBridgeWorker) WatchForDestroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// ConfigMapRenderer doesn't apply to a cluster, so there's nothing to watch
	return nil
}

func (w *ConfigMapBridgeWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return nil
}
