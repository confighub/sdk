// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package oci

import (
	"fmt"
	"strings"
	"time"

	"github.com/confighub/sdk/core/worker/api"
	funcApi "github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
)

// OCIBridge is a server-hosted bridge for the OCI transport: Unit data is published
// verbatim to an OCI repository for a puller (e.g. Argo/Flux) to consume, rather than
// being applied to a remote endpoint over a worker connection. The bridge performs no
// remote apply — like Noop it instantly succeeds and echoes the data back as live state
// — and it never routes by toolchain, so it advertises the ToolchainAny wildcard. That
// lets a single OCI Target carry Units of any toolchain (KRM, AppConfig, ConfigHub YAML)
// without enumerating each one.
type OCIBridge struct{}

var _ api.BridgeWorker = (*OCIBridge)(nil)

func NewOCIBridge() *OCIBridge {
	return &OCIBridge{}
}

func (w *OCIBridge) ID() api.BridgeWorkerID {
	return api.BridgeWorkerID{
		ProviderType:   api.ProviderOCI,
		ToolchainTypes: []workerapi.ToolchainType{workerapi.ToolchainAny},
	}
}

func (w *OCIBridge) Info(_ api.InfoOptions) api.BridgeWorkerInfo {
	return api.BridgeWorkerInfo{
		SupportedConfigTypes: []*api.SupportedConfigType{
			{
				ConfigTypeSignature: api.ConfigTypeSignature{
					ConfigType: api.ConfigType{
						ProviderType:  api.ProviderOCI,
						ToolchainType: workerapi.ToolchainAny,
					},
				},
			},
		},
	}
}

func (w *OCIBridge) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	resourceStatuses := buildResourceStatuses(payload.Data)

	result := newActionResult(api.ActionStatusCompleted, api.ActionResultApplyCompleted, "Published successfully (oci)")
	result.LiveData = payload.Data
	result.LiveState = payload.Data
	result.ResourceStatuses = resourceStatuses
	return wctx.SendStatus(result)
}

func (w *OCIBridge) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	result := newActionResult(api.ActionStatusCompleted, api.ActionResultRefreshAndNoDrift, "Refreshed (oci)")
	result.LiveData = payload.Data
	if payload.LiveState != nil {
		result.LiveState = payload.LiveState
	} else {
		result.LiveState = payload.Data
	}
	return wctx.SendStatus(result)
}

func (w *OCIBridge) Destroy(wctx api.BridgeWorkerContext, _ api.BridgeWorkerPayload) error {
	result := newActionResult(api.ActionStatusCompleted, api.ActionResultDestroyCompleted, "Destroyed (oci)")
	return wctx.SendStatus(result)
}

func (w *OCIBridge) Import(wctx api.BridgeWorkerContext, _ api.BridgeWorkerPayload) error {
	result := newActionResult(api.ActionStatusFailed, api.ActionResultImportFailed, "Import not supported for server-hosted workers")
	return wctx.SendStatus(result)
}

func (w *OCIBridge) Finalize(_ api.BridgeWorkerContext, _ api.BridgeWorkerPayload) error {
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

// buildResourceStatuses parses multi-doc YAML data and produces a ResourceStatusMap
// with all KRM resources marked as Synced and Ready. Non-KRM documents (e.g. a Helm
// Chart.yaml or values.yaml, which lack apiVersion/kind/metadata.name) are skipped.
func buildResourceStatuses(data []byte) api.ResourceStatusMap {
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
