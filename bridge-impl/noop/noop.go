// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package noop

import (
	"fmt"
	"strings"
	"time"

	"github.com/confighub/sdk/worker/api"
	funcApi "github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/confighub/sdk/workerapi"
)

// NoopBridge is a server-hosted bridge that instantly succeeds all operations
// without interacting with any real infrastructure. It is used for demos and testing.
type NoopBridge struct{}

var _ api.BridgeWorker = (*NoopBridge)(nil)

func NewNoopBridge() *NoopBridge {
	return &NoopBridge{}
}

func (w *NoopBridge) ID() api.BridgeWorkerID {
	return api.BridgeWorkerID{
		ProviderType:   api.ProviderNoop,
		ToolchainTypes: []workerapi.ToolchainType{workerapi.ToolchainKubernetesYAML},
	}
}

func (w *NoopBridge) Info(_ api.InfoOptions) api.BridgeWorkerInfo {
	return api.BridgeWorkerInfo{
		SupportedConfigTypes: []*api.SupportedConfigType{
			{
				ConfigTypeSignature: api.ConfigTypeSignature{
					ConfigType: api.ConfigType{
						ProviderType:  api.ProviderNoop,
						ToolchainType: workerapi.ToolchainKubernetesYAML,
					},
				},
			},
		},
	}
}

func (w *NoopBridge) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	resourceStatuses := buildNoopResourceStatuses(payload.Data)

	result := newActionResult(api.ActionStatusCompleted, api.ActionResultApplyCompleted, "Applied successfully (noop)")
	result.LiveData = payload.Data
	result.LiveState = payload.Data
	result.ResourceStatuses = resourceStatuses
	return wctx.SendStatus(result)
}

func (w *NoopBridge) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	result := newActionResult(api.ActionStatusCompleted, api.ActionResultRefreshAndNoDrift, "Refreshed (noop)")
	result.LiveData = payload.Data
	if payload.LiveState != nil {
		result.LiveState = payload.LiveState
	} else {
		result.LiveState = payload.Data
	}
	return wctx.SendStatus(result)
}

func (w *NoopBridge) Destroy(wctx api.BridgeWorkerContext, _ api.BridgeWorkerPayload) error {
	result := newActionResult(api.ActionStatusCompleted, api.ActionResultDestroyCompleted, "Destroyed (noop)")
	return wctx.SendStatus(result)
}

func (w *NoopBridge) Import(wctx api.BridgeWorkerContext, _ api.BridgeWorkerPayload) error {
	result := newActionResult(api.ActionStatusFailed, api.ActionResultImportFailed, "Import not supported for server-hosted workers")
	return wctx.SendStatus(result)
}

func (w *NoopBridge) Finalize(_ api.BridgeWorkerContext, _ api.BridgeWorkerPayload) error {
	return nil
}

func newActionResult(status api.ActionStatusType, result api.ActionResultType, message string) *api.ActionResult {
	now := time.Now()
	return &api.ActionResult{
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			Status:       status,
			Result:       result,
			Message:      message,
			StartedAt:    now,
			TerminatedAt: &now,
		},
	}
}

// buildNoopResourceStatuses parses multi-doc YAML data and produces a ResourceStatusMap
// with all resources marked as Synced and Ready.
func buildNoopResourceStatuses(data []byte) api.ResourceStatusMap {
	if len(data) == 0 {
		return nil
	}

	docs, err := gaby.ParseAll(data)
	if err != nil {
		return nil
	}

	now := time.Now()
	statuses := make(api.ResourceStatusMap, len(docs))

	for _, doc := range docs {
		apiVersionNode := doc.Search("apiVersion")
		kindNode := doc.Search("kind")
		nameNode := doc.Search("metadata", "name")

		if apiVersionNode == nil || kindNode == nil || nameNode == nil {
			continue
		}

		apiVersion := fmt.Sprintf("%v", apiVersionNode.Data())
		kind := fmt.Sprintf("%v", kindNode.Data())
		name := fmt.Sprintf("%v", nameNode.Data())

		namespace := ""
		nsNode := doc.Search("metadata", "namespace")
		if nsNode != nil {
			namespace = fmt.Sprintf("%v", nsNode.Data())
		}

		// Parse apiVersion into group and version.
		// Core resources: "v1" -> group="", version="v1"
		// Others: "apps/v1" -> group="apps", version="v1"
		group := ""
		version := apiVersion
		if parts := strings.SplitN(apiVersion, "/", 2); len(parts) == 2 {
			group = parts[0]
			version = parts[1]
		}

		// Build resource key in format "group/version/kind#namespace/name"
		var resourceType string
		if group != "" {
			resourceType = fmt.Sprintf("%s/%s/%s", group, version, kind)
		} else {
			resourceType = fmt.Sprintf("%s/%s", version, kind)
		}
		key := funcApi.ResourceTypeAndName(fmt.Sprintf("%s#%s/%s", resourceType, namespace, name))

		statuses[key] = api.ResourceStatus{
			SyncStatus: api.ResourceSyncStatusSynced,
			Readiness:  api.ResourceReadinessReady,
			UpdatedAt:  now,
		}
	}

	if len(statuses) == 0 {
		return nil
	}
	return statuses
}
