// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/bridge-worker/lib"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/function"
	functionapi "github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/workerapi"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ConfigMapBridgeWorker struct {
	KubernetesBridgeWorker
}

var _ api.BridgeWorker = (*ConfigMapBridgeWorker)(nil)
var _ api.WatchableWorker = (*ConfigMapBridgeWorker)(nil)

// The standard annotations are:
// confighub.com/UnitSlug
// confighub.com/SpaceID

// We also add a label so we can select all ConfigMaps for this Unit

const configMapTemplateString = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    confighub.com/UnitSlug: {{.UnitSlug}}
  annotations:
    confighub.com/UnitSlug: {{.UnitSlug}}
    confighub.com/SpaceID: {{.SpaceID}}
    confighub.com/RevisionNum: "{{.RevisionNum}}"
data:
  {{.DataName}}: |
{{.ConfigData}}
`

type configMapTemplateArgs struct {
	Name        string
	Namespace   string
	UnitSlug    string
	SpaceID     string
	RevisionNum string
	DataName    string
	ConfigData  string
}

// kubectl's generator is here:
// https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/kubectl/pkg/cmd/create/create_configmap.go

func generateConfigMapFromData(args *configMapTemplateArgs) []byte {
	// Indent the config text
	args.ConfigData = "    " + strings.TrimSuffix(strings.ReplaceAll(args.ConfigData, "\n", "\n    "), "    ")
	tmpl, err := template.New("configMap").Parse(configMapTemplateString)
	if err != nil {
		// Shouldn't happen
		log.Log.Error(err, "ConfigMap template failed to parse")
	}
	var out bytes.Buffer
	err = tmpl.Execute(&out, args)
	if err != nil {
		// Shouldn't happen.
		log.Log.Error(err, "ConfigMap template failed to evaluate")

	}
	return out.Bytes()
}

func (w *ConfigMapBridgeWorker) ID() api.BridgeWorkerID {
	return api.BridgeWorkerID{
		ProviderType: api.ProviderConfigMapRenderer,
		// Support multiple AppConfig types
		ToolchainTypes: []workerapi.ToolchainType{
			workerapi.ToolchainAppConfigProperties,
			workerapi.ToolchainAppConfigTOML,
			workerapi.ToolchainAppConfigINI,
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
		configTypes = append(configTypes, newConfigType)
	}

	return api.BridgeWorkerInfo{
		SupportedConfigTypes: configTypes,
	}
}

// This is also defined in the function executor.
const configHubPrefix = "configHub"

const NamespaceProperty = configHubPrefix + ".kubernetes.namespace"

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func getFileExtensionForToolchain(toolchain workerapi.ToolchainType) string {
	switch toolchain {
	case workerapi.ToolchainAppConfigProperties:
		return ".properties"
	case workerapi.ToolchainAppConfigTOML:
		return ".toml"
	case workerapi.ToolchainAppConfigINI:
		return ".ini"
	case workerapi.ToolchainAppConfigYAML:
		return ".yaml"
	default:
		return ".config"
	}
}

func transformAppConfigToConfigMap(payload *api.BridgeWorkerPayload) {
	configData := string(payload.Data)

	// Extract the namespace using get-string-path function
	namespace := "default" // Default value

	// FIXME: This re-initializes the path registry currently, which is not a good
	// thing, but it only happens when this is invoked. We should pass the executor
	// down from the worker instead.
	// Create function executor
	functionExecutor := function.NewStandardExecutor()

	// Invoke get-string-path to extract namespace
	getStringPathInvocation := functionapi.FunctionInvocation{
		FunctionName: "get-string-path",
		Arguments: []functionapi.FunctionArgument{
			{Value: "*"},
			{Value: NamespaceProperty},
		},
	}

	getStringPathRequest := &functionapi.FunctionInvocationRequest{
		FunctionContext: functionapi.FunctionContext{
			ToolchainType: payload.ToolchainType,
		},
		ConfigData:          payload.Data,
		FunctionInvocations: []functionapi.FunctionInvocation{getStringPathInvocation},
	}

	ctx := context.Background()
	getStringPathResp, err := functionExecutor.Invoke(ctx, getStringPathRequest)
	if err == nil && getStringPathResp.Success {
		// Extract namespace from AttributeValueList output
		if outputData, exists := getStringPathResp.Outputs[functionapi.OutputTypeAttributeValueList]; exists {
			var attrList functionapi.AttributeValueList
			if err := json.Unmarshal(outputData, &attrList); err == nil && len(attrList) > 0 {
				if strValue, ok := attrList[0].Value.(string); ok {
					namespace = strValue
				}
			}
		}
	}

	// Delete configHub properties using delete-path function
	deletePathInvocation := functionapi.FunctionInvocation{
		FunctionName: "delete-path",
		Arguments: []functionapi.FunctionArgument{
			{Value: "*"},
			{Value: configHubPrefix},
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

	nameSuffix := truncateString(fmt.Sprintf("%x", sha256.Sum256(payload.Data)), 10)
	fileExtension := getFileExtensionForToolchain(payload.ToolchainType)
	args := &configMapTemplateArgs{
		Name:        k8skit.K8sResourceProvider.NormalizeName(payload.UnitSlug + "-" + nameSuffix),
		Namespace:   namespace,
		UnitSlug:    payload.UnitSlug,
		SpaceID:     payload.SpaceID.String(),
		RevisionNum: fmt.Sprintf("%d", payload.RevisionNum),
		DataName:    payload.UnitSlug + fileExtension,
		ConfigData:  configData,
	}
	configMap := generateConfigMapFromData(args)
	payload.Data = []byte(configMap)
}

func (w *ConfigMapBridgeWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	transformAppConfigToConfigMap(&payload)
	status := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Rendered ConfigMap successfully at %s", time.Now().Format(time.RFC3339)),
	)
	status.LiveState = payload.Data
	return wctx.SendStatus(status)
}

func (w *ConfigMapBridgeWorker) WatchForApply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// ConfigMapRenderer doesn't apply to a cluster, so there's nothing to watch
	return nil
}

func (w *ConfigMapBridgeWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// Refresh doesn't really make sense
	return lib.SafeSendStatus(wctx, newActionResult(
		api.ActionStatusFailed,
		api.ActionResultRefreshFailed,
		"Refresh not supported",
	), errors.New("Refresh not supported"))
}

func (w *ConfigMapBridgeWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// Import doesn't really make sense
	return lib.SafeSendStatus(wctx, newActionResult(
		api.ActionStatusFailed,
		api.ActionResultImportFailed,
		"Import not supported",
	), errors.New("Import not supported"))
}

func (w *ConfigMapBridgeWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	result := newActionResult(
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
