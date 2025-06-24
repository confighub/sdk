// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/bridge-worker/lib"
	"github.com/confighub/sdk/workerapi"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ConfigMapBridgeWorker struct {
	KubernetesBridgeWorker
}

var _ api.BridgeWorker = (*ConfigMapBridgeWorker)(nil)
var _ api.WatchableWorker = (*ConfigMapBridgeWorker)(nil)

const configMapTemplateString = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    confighub.com/UnitSlug: {{.Label}}
  annotations:
    confighub.com/RevisionNum: "{{.RevisionNum}}"
data:
  {{.DataName}}: |
{{.ConfigData}}
`

// This is a label rather than an annotation so that we can select all the generated ConfigMaps.
const configMapLabelKey = "confighub.com/UnitSlug"

type configMapTemplateArgs struct {
	Name        string
	Namespace   string
	Label       string
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

func (w *ConfigMapBridgeWorker) Info(opts api.InfoOptions) api.BridgeWorkerInfo {
	// TODO: Support other AppConfig types
	return w.KubernetesBridgeWorker.InfoForToolchainAndProvider(opts, workerapi.ToolchainAppConfigProperties, api.ProviderConfigMap)
}

// This is also defined in the function executor.
const configHubPrefix = "configHub."

const NamespaceProperty = configHubPrefix + "kubernetes.namespace"

// '.' doesn't match newlines, so we need to permit them explicitly. The underlying syntax
// package supports matching newlines and also supports matching beginning and end of lines,
// but these capabilities don't appear to be accessible through the standard regexp functions.
var namespaceRegexpString = "^(?:(?:.|\n)*\n)?[ \t]*" + strings.ReplaceAll(NamespaceProperty, ".", "\\.") + "[ \t]*[:=][ \t]*([a-z0-9\\-]+)"

var namespaceRegexp = regexp.MustCompile(namespaceRegexpString)

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func transformAppConfigToConfigMap(payload *api.BridgeWorkerPayload) {
	configData := string(payload.Data)
	// Extract the namespace. We could use get-string-path, but that would require conversion to YAML, etc.
	namespaceMatch := namespaceRegexp.FindStringSubmatch(configData)
	var namespace string
	if namespaceMatch == nil || len(namespaceMatch) < 2 {
		namespace = "default"
	} else {
		namespace = namespaceMatch[1]
	}
	// Comment out configHub fields. We may want to uncomment these in functions instead.
	configData = strings.ReplaceAll(configData, configHubPrefix, "#"+configHubPrefix)
	nameSuffix := truncateString(fmt.Sprintf("%x", sha256.Sum256(payload.Data)), 10)
	args := &configMapTemplateArgs{
		// TODO: ensure slug character set is valid
		Name:        payload.UnitSlug + "-" + nameSuffix,
		Namespace:   namespace,
		Label:       payload.UnitSlug,
		RevisionNum: fmt.Sprintf("%d", payload.RevisionNum),
		DataName:    payload.UnitSlug + ".properties", // TODO: support other AppConfig types
		ConfigData:  configData,
	}
	configMap := generateConfigMapFromData(args)
	payload.Data = []byte(configMap)
}

func (w *ConfigMapBridgeWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	transformAppConfigToConfigMap(&payload)
	// TODO: GC configmaps more than a designated amount
	return w.KubernetesBridgeWorker.Apply(wctx, payload)
}

func (w *ConfigMapBridgeWorker) WatchForApply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	transformAppConfigToConfigMap(&payload)
	return w.KubernetesBridgeWorker.WatchForApply(wctx, payload)
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
	// TODO: delete all generated configmaps
	transformAppConfigToConfigMap(&payload)
	return w.KubernetesBridgeWorker.Destroy(wctx, payload)
}

func (w *ConfigMapBridgeWorker) WatchForDestroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// TODO: delete all generated configmaps
	transformAppConfigToConfigMap(&payload)
	return w.KubernetesBridgeWorker.WatchForDestroy(wctx, payload)
}

func (w *ConfigMapBridgeWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return w.KubernetesBridgeWorker.Finalize(wctx, payload)
}
