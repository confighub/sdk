// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/fluxcd/pkg/ssa"
	ssautil "github.com/fluxcd/pkg/ssa/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/bridge-worker/lib"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/confighub/sdk/workerapi"
)

type KubernetesBridgeWorker struct {
	cfg *rest.Config
}

var _ api.BridgeWorker = (*KubernetesBridgeWorker)(nil)
var _ api.WatchableWorker = (*KubernetesBridgeWorker)(nil)

type KubernetesWorkerParams struct {
	KubeContext string `json:",omitempty"`
	WaitTimeout string `json:",omitempty"` // Duration string like "5m0s", "10h5m"
}

// getResourcesFromImportSource determines the appropriate resource fetching method
// based on the provided ExtraParams and returns the resources
func (w *KubernetesBridgeWorker) getResourcesFromImportSource(k8sclient KubernetesClient, extraParams []byte) ([]*unstructured.Unstructured, error) {
	config := &ImportConfig{IncludeSystem: false, IncludeCustom: true, IncludeCluster: true, Filters: []goclientnew.ImportFilter{}}
	if len(extraParams) > 0 {
		// Try to parse ExtraParams as ImportRequest structure
		var importRequest goclientnew.ImportRequest
		if err := json.Unmarshal(extraParams, &importRequest); err == nil {
			// Successfully parsed as ImportRequest - use new generic filters
			config = NewImportConfigFromRequest(&importRequest)
			return GetResourcesWithConfig(k8sclient, config, w.cfg)
		}
		// If parsing fails, fall through to default behavior
	}

	// Fall back to default behavior (get all cluster resources)
	return GetResourcesWithConfig(k8sclient, config, w.cfg)
}

func (p KubernetesWorkerParams) ToMap() map[string]interface{} {
	var result map[string]interface{}
	data, _ := json.Marshal(p)
	_ = json.Unmarshal(data, &result)
	return result
}

func (w *KubernetesBridgeWorker) Info(opts api.InfoOptions) api.BridgeWorkerInfo {
	return w.InfoForToolchainAndProvider(opts, workerapi.ToolchainKubernetesYAML, api.ProviderKubernetes)
}

// This supports ToolchainTypes and ProviderTypes that generate and apply Kubernetes resources.
func (w *KubernetesBridgeWorker) InfoForToolchainAndProvider(opts api.InfoOptions, toolchain workerapi.ToolchainType, provider api.ProviderType) api.BridgeWorkerInfo {
	// Get available contexts
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	k8sCmdConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)
	cfg, err := k8sCmdConfig.ClientConfig()
	// if we can get the config, use it
	// otherwise, we'll use the in-cluster config
	if err == nil {
		w.cfg = cfg
	}

	kubeConfig, err := k8sCmdConfig.RawConfig()
	if err != nil {
		return api.BridgeWorkerInfo{}
	}

	// Check if we're running inside a Kubernetes cluster
	// When running inside a cluster, the in-cluster config will be used
	// and we don't need to list available contexts
	if cfg, err := rest.InClusterConfig(); err == nil {
		w.cfg = cfg
		log.Log.Info("Running inside Kubernetes cluster, using in-cluster configuration")
		targetName := os.Getenv("IN_CLUSTER_TARGET_NAME")
		if targetName == "" {
			targetName = opts.Slug
		}
		return api.BridgeWorkerInfo{
			SupportedConfigTypes: []*api.ConfigType{
				{
					ToolchainType: toolchain,
					ProviderType:  provider,
					AvailableTargets: []api.Target{
						{
							Name: targetName,
							Params: KubernetesWorkerParams{
								WaitTimeout: "2m0s",
							}.ToMap(),
						},
					},
				},
			},
		}
	}

	// Create targets for each context
	// TODO: will workerSlug + contextName be unique enough?
	var targets []api.Target
	for contextName := range kubeConfig.Contexts {
		targets = append(targets, api.Target{
			Name: fmt.Sprintf("%s-%s", opts.Slug, contextName),
			Params: KubernetesWorkerParams{
				KubeContext: contextName,
				WaitTimeout: "2m0s",
			}.ToMap(),
		})
	}

	return api.BridgeWorkerInfo{
		SupportedConfigTypes: []*api.ConfigType{
			{
				ToolchainType:    toolchain,
				ProviderType:     provider,
				AvailableTargets: targets,
			},
		},
	}
}

func (w *KubernetesBridgeWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	_, kubeContext, err := parseTargetParams(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		), err)
	}

	k8sclient, man, err := kubernetesClientFactory(kubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		), err)
	}

	objects, err := parseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		), err)
	}

	// If namespace is not declared in the unit, it will be set to default on namespaced resources
	setDefaultNamespaceIfNotDeclared(objects, k8sclient)

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to apply resources...",
	)); err != nil {
		return err
	}

	changeSet, err := man.ApplyAllStaged(context.Background(), objects, ssa.DefaultApplyOptions())
	if err != nil {
		log.Log.Error(err, "Failed to apply resources")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("Failed to apply resources: %v", err),
		), err)
	}

	log.Log.Info("🔄 Applying resources...", "count", len(objects))
	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Applying resources...",
	)); err != nil {
		return err
	}

	log.Log.Info("✅ Successfully initiated applying resources", "changeset_entries", len(changeSet.Entries))
	return nil
}

func setDefaultNamespaceIfNotDeclared(objects []*unstructured.Unstructured, k8sclient KubernetesClient) {
	for _, obj := range objects {
		// obj.GetNamespace() returns empty string for cluster scoped objects
		// and namespaced objects where namespace is not set
		if obj.GetNamespace() == "" {
			// check if it is a namespaced object so we don't set namespace on cluster scoped objects
			isns, err := k8sclient.IsObjectNamespaced(obj)
			if err == nil && isns {
				log.Log.Info("🔄 Setting namespace to default on ", "name", obj.GetName())
				// This is currently not configurable.
				obj.SetNamespace("default")
			}
		}
	}
}

func objectsToYAML(objects []*unstructured.Unstructured) (string, error) {
	yamlData, err := ssautil.ObjectsToYAML(objects)
	if err != nil {
		return "", err
	}
	// ObjectsToYAML adds a trailing doc separator. Remove it.
	return gaby.NormalizeYAML(yamlData), err
}

func (w *KubernetesBridgeWorker) WatchForApply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	log.Log.Info("🔄 Waiting for resources to be ready...")
	workerParams, kubeContext, err := parseTargetParams(payload)
	if err != nil {
		// if we can't parse the target params, we cannot look for the resources
		return backoff.Permanent(lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}

	k8sclient, man, err := kubernetesClientFactory(kubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err)
	}

	objects, err := parseObjects(payload.Data)
	if err != nil {
		// if we can't parse the objects, we can't wait for them
		return backoff.Permanent(lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}
	setDefaultNamespaceIfNotDeclared(objects, k8sclient)

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Waiting for the applied resources...",
	)); err != nil {
		return err
	}

	// Set up wait options with timeout if specified
	waitOpts := ssa.DefaultWaitOptions()
	if workerParams.WaitTimeout != "" {
		timeout, err := time.ParseDuration(workerParams.WaitTimeout)
		if err != nil {
			log.Log.Error(err, "Invalid wait timeout format, using default", "timeout", workerParams.WaitTimeout)
		} else {
			waitOpts.Timeout = timeout
			log.Log.Info("Using custom wait timeout", "timeout", timeout.String())
		}
	}

	// TODO: do we throw an error if the wait times out?
	// Default behavior is to wait 2m0s
	if err := man.Wait(objects, waitOpts); err != nil {
		log.Log.Error(err, "Failed to wait for resources")
		if errors.Is(err, context.DeadlineExceeded) {
			// log the error but don't return it
			lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusProgressing,
				api.ActionResultNone,
				fmt.Sprintf("Failed to wait for resources: %v", err),
			), err)
			// set retry back to initial interval 30s
			return backoff.RetryAfter(30)
		}
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("Failed to wait for resources: %v", err),
		), err)
	}
	log.Log.Info("✅ All resources are ready")

	liveObjects, err := getLiveObjects(wctx, man, objects, true)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("Failed to get Live Objects: %v", err),
		), err)
	}

	yamlData, err := objectsToYAML(liveObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("Failed to convert objects to YAML: %v", err),
		), err)
	}

	status := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Applied %d resources successfully at %s", len(liveObjects), time.Now().Format(time.RFC3339)),
	)
	status.LiveState = []byte(yamlData)
	wctx.SendStatus(status)
	return nil
}

func (w *KubernetesBridgeWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	_, kubeContext, err := parseTargetParams(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		), err)
	}

	k8sclient, man, err := kubernetesClientFactory(kubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		), err)
	}

	objects, err := parseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		), err)
	}

	setDefaultNamespaceIfNotDeclared(objects, k8sclient)

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to retrieve resources...",
	)); err != nil {
		return err
	}

	retrievedObjects, err := getLiveObjects(wctx, man, objects, true)
	if err != nil {
		log.Log.Error(err, "Failed to retrieve live objects")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to retrieve live objects: %v", err),
		), err)
	}

	log.Log.Info("🔄 Retrieving resources...", "count", len(objects))
	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Retrieving resources...",
	)); err != nil {
		return err
	}

	yamlData, err := objectsToYAML(retrievedObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to convert objects to YAML: %v", err),
		), err)
	}

	patched, drifted, err := yamlkit.DiffPatch(payload.LiveState, []byte(yamlData), payload.Data, k8skit.K8sResourceProvider)
	if err != nil {
		log.Log.Error(err, "Failed to diff patch")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to diff patch: %v", err),
		), err)
	}

	if !drifted {
		log.Log.Info("✅ No drift detected")
		result := newActionResult(
			api.ActionStatusCompleted,
			api.ActionResultRefreshAndNoDrift,
			"Live state matches - no drift detected",
		)
		return wctx.SendStatus(result)
	}

	log.Log.Info("✅ Successfully retrieved resources", "count", len(retrievedObjects))

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultRefreshAndDrifted,
		fmt.Sprintf("Retrieved %d resources successfully at %s", len(retrievedObjects), time.Now().Format(time.RFC3339)),
	)
	result.Data = patched
	result.LiveState = []byte(yamlData)
	return wctx.SendStatus(result)
}

func (w *KubernetesBridgeWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	_, kubeContext, err := parseTargetParams(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			err.Error(),
		), err)
	}

	k8sclient, man, err := kubernetesClientFactory(kubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			err.Error(),
		), err)
	}

	// Determine import source and get resource list
	var resourceInfoList []api.ResourceInfo
	var retrievedObjects []*unstructured.Unstructured
	if len(payload.Data) > 0 {
		// Legacy flow: Data is provided via stdin/file
		if err := wctx.SendStatus(newActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			"Parsing provided resource information...",
		)); err != nil {
			return err
		}

		if err := json.Unmarshal(payload.Data, &resourceInfoList); err != nil {
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultImportFailed,
				fmt.Sprintf("Failed to parse resource info list: %v", err),
			), err)
		}

		if err := wctx.SendStatus(newActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			fmt.Sprintf("Found %d resources to import", len(resourceInfoList)),
		)); err != nil {
			return err
		}

		if err := wctx.SendStatus(newActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			"Converting resources to unstructured format...",
		)); err != nil {
			return err
		}
		// Convert ResourceInfoList to Unstructured objects
		objects := []*unstructured.Unstructured{}
		for _, resourceInfo := range resourceInfoList {
			u := &unstructured.Unstructured{}
			gvk, err := parseGroupVersionKind(resourceInfo.ResourceType)
			if err != nil {
				return lib.SafeSendStatus(wctx, newActionResult(
					api.ActionStatusFailed,
					api.ActionResultImportFailed,
					err.Error(),
				), err)
			}
			u.SetGroupVersionKind(gvk)
			parts := strings.Split(resourceInfo.ResourceName, "/")
			if len(parts) == 2 {
				u.SetNamespace(parts[0])
				u.SetName(parts[1])
			} else if len(parts) == 1 {
				u.SetName(resourceInfo.ResourceName)
			}
			objects = append(objects, u)
		}

		setDefaultNamespaceIfNotDeclared(objects, k8sclient)
		// Only get live objects if we're importing from stdin/file (legacy flow)
		if err := wctx.SendStatus(newActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			"Retrieving live state of resources...",
		)); err != nil {
			return err
		}

		retrievedObjects, err = getLiveObjects(wctx, man, objects, true)
		if err != nil {
			log.Log.Error(err, "Failed to retrieve live objects")
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultImportFailed,
				fmt.Sprintf("Failed to retrieve live objects: %v", err),
			), err)
		}
	} else {
		// New flow: Fetch resources from cluster using parameters
		if err := wctx.SendStatus(newActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			"Fetching resources from Kubernetes cluster...",
		)); err != nil {
			return err
		}

		retrievedObjects, err = w.getResourcesFromImportSource(k8sclient, payload.ExtraParams)
		if err != nil {
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultImportFailed,
				fmt.Sprintf("Failed to get cluster resources: %v", err),
			), err)
		}
	}

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Converting resources to YAML format...",
	)); err != nil {
		return err
	}

	yamlForLiveState, err := objectsToYAML(retrievedObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML for live state")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			fmt.Sprintf("Failed to convert live state objects to YAML: %v", err),
		), err)
	}

	//heuristic extra cleanup setups for objects to make them suitable for being unit.Data
	yamlForData, err := objectsToYAML(extraCleanupObjects(retrievedObjects))
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML for data")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			fmt.Sprintf("Failed to convert data objects to YAML: %v", err),
		), err)
	}

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultImportCompleted,
		fmt.Sprintf("Imported %d resources successfully at %s", len(retrievedObjects), time.Now().Format(time.RFC3339)),
	)
	result.Data = []byte(yamlForData)
	result.LiveState = []byte(yamlForLiveState)
	return wctx.SendStatus(result)
}

func (w *KubernetesBridgeWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	_, kubeContext, err := parseTargetParams(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		), err)
	}

	k8sclient, man, err := kubernetesClientFactory(kubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		), err)
	}

	objects, err := parseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		), err)
	}

	setDefaultNamespaceIfNotDeclared(objects, k8sclient)
	if err = wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to destroy resources...",
	)); err != nil {
		return err
	}

	log.Log.Info("🔄 Starting resource destruction...")
	changeSet, err := man.DeleteAll(context.Background(), objects, ssa.DefaultDeleteOptions())
	if err != nil {
		log.Log.Error(err, "Failed to delete resources")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			fmt.Sprintf("Failed to delete resources: %v", err),
		), err)
	}
	log.Log.Info("✅ Successfully initiated destruction of resources", "changeset_entries", len(changeSet.Entries))
	return nil
}

func (w *KubernetesBridgeWorker) WatchForDestroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	log.Log.Info("🔄 Waiting for resources to be terminated...")
	workerParams, kubeContext, err := parseTargetParams(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			err.Error(),
		), err)
	}

	k8sclient, man, err := kubernetesClientFactory(kubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			err.Error(),
		), err)
	}

	objects, err := parseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			err.Error(),
		), err)
	}
	setDefaultNamespaceIfNotDeclared(objects, k8sclient)

	if err = wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Waiting for resources to be terminated...",
	)); err != nil {
		return err
	}

	// Set up wait options with timeout if specified
	waitOpts := ssa.DefaultWaitOptions()
	if workerParams.WaitTimeout != "" {
		timeout, err := time.ParseDuration(workerParams.WaitTimeout)
		if err != nil {
			log.Log.Error(err, "Invalid wait timeout format, using default", "timeout", workerParams.WaitTimeout)
		} else {
			waitOpts.Timeout = timeout
			log.Log.Info("Using custom wait timeout", "timeout", timeout.String())
		}
	}
	if err := man.WaitForTermination(objects, waitOpts); err != nil {
		log.Log.Error(err, "Failed to wait for resource termination")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			fmt.Sprintf("Failed to wait for resource termination: %v", err),
		), err)
	}
	log.Log.Info("✅ All resources terminated successfully")

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultDestroyCompleted,
		fmt.Sprintf("Destroyed resources successfully at %s", time.Now().Format(time.RFC3339)),
	)
	result.LiveState = []byte{}
	return wctx.SendStatus(result)
}

func (w *KubernetesBridgeWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting finalization...",
	)); err != nil {
		return err
	}

	log.Log.Info("✅ Finalization completed successfully")

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultNone,
		"Finalization completed successfully",
	)
	return wctx.SendStatus(result)
}
