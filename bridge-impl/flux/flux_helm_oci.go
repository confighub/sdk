// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package flux

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/confighub/sdk/bridge-impl/common"
	"github.com/confighub/sdk/bridge-impl/kubernetes"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	funcApi "github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/worker/lib"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

// Flux Helm API version constants
const (
	fluxHelmRepoAPIVersion    = "source.toolkit.fluxcd.io/v1"
	fluxHelmReleaseAPIVersion = "helm.toolkit.fluxcd.io/v2"
	fluxKindHelmRepository    = "HelmRepository"
	fluxKindHelmRelease       = "HelmRelease"
)

// generateFluxHelmRepository generates a Flux HelmRepository CR with spec.type=oci.
// The OCIRepoURL is expected to be the full unit URL (oci://host/unit/space/unitslug);
// this function strips the last path segment to derive the HelmRepository base URL.
func generateFluxHelmRepository(args *fluxOCIArgs) ([]byte, error) {
	// Strip the last path segment to get the base repo URL.
	// e.g., oci://host/unit/space/unitslug -> oci://host/unit/space
	helmRepoURL := args.OCIRepoURL
	if idx := strings.LastIndex(helmRepoURL, "/"); idx > len(ociURLScheme) {
		helmRepoURL = helmRepoURL[:idx]
	}

	spec := map[string]interface{}{
		"type":     "oci",
		"interval": args.Interval,
		"url":      helmRepoURL,
	}
	if args.Insecure {
		spec["insecure"] = true
	}
	if args.SecretName != "" {
		spec["secretRef"] = map[string]interface{}{
			"name": args.SecretName,
		}
	}

	repo := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fluxHelmRepoAPIVersion,
			"kind":       fluxKindHelmRepository,
			"metadata": map[string]interface{}{
				"name":      args.Name,
				"namespace": args.FluxNamespace,
				"annotations": map[string]interface{}{
					k8skit.UnitSlugAnnotation:    args.UnitSlug,
					k8skit.SpaceIDAnnotation:     args.SpaceID,
					k8skit.RevisionNumAnnotation: args.RevisionNum,
					annotationExternalLink:       fmt.Sprintf(configHubUnitURLFormat, args.ConfigHubURL, args.SpaceID, args.UnitID),
				},
				"labels": map[string]interface{}{
					k8skit.LabelManagedBy: labelFluxManagedByValue,
				},
			},
			"spec": spec,
		},
	}

	out, err := yaml.Marshal(repo.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Flux HelmRepository to YAML: %w", err)
	}
	return out, nil
}

// generateFluxHelmRelease generates a Flux HelmRelease CR referencing a HelmRepository.
func generateFluxHelmRelease(args *fluxOCIArgs) ([]byte, error) {
	chartSpec := map[string]interface{}{
		"chart":   args.UnitSlug,
		"version": args.HelmChartVersion,
		"sourceRef": map[string]interface{}{
			"kind": fluxKindHelmRepository,
			"name": args.Name,
		},
	}

	spec := map[string]interface{}{
		"interval":        args.Interval,
		"targetNamespace": args.TargetNamespace,
		"chart": map[string]interface{}{
			"spec": chartSpec,
		},
	}
	if args.HelmReleaseName != "" {
		spec["releaseName"] = args.HelmReleaseName
	}

	hr := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fluxHelmReleaseAPIVersion,
			"kind":       fluxKindHelmRelease,
			"metadata": map[string]interface{}{
				"name":      args.Name,
				"namespace": args.FluxNamespace,
				"annotations": map[string]interface{}{
					k8skit.UnitSlugAnnotation:    args.UnitSlug,
					k8skit.SpaceIDAnnotation:     args.SpaceID,
					k8skit.RevisionNumAnnotation: args.RevisionNum,
					annotationExternalLink:       fmt.Sprintf(configHubUnitURLFormat, args.ConfigHubURL, args.SpaceID, args.UnitID),
				},
				"labels": map[string]interface{}{
					k8skit.LabelManagedBy: labelFluxManagedByValue,
				},
			},
			"spec": spec,
		},
	}

	out, err := yaml.Marshal(hr.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Flux HelmRelease to YAML: %w", err)
	}
	return out, nil
}

// findFluxHelmReleaseObject returns the first HelmRelease object from a list of parsed objects.
func findFluxHelmReleaseObject(objects []*unstructured.Unstructured) *unstructured.Unstructured {
	for _, obj := range objects {
		if obj.GetKind() == fluxKindHelmRelease {
			return obj
		}
	}
	return nil
}

// findFluxHelmRepositoryObject returns the first HelmRepository object from a list of parsed objects.
func findFluxHelmRepositoryObject(objects []*unstructured.Unstructured) *unstructured.Unstructured {
	for _, obj := range objects {
		if obj.GetKind() == fluxKindHelmRepository {
			return obj
		}
	}
	return nil
}

// buildFluxHelmReleaseStatusMap builds a ResourceStatusMap from a Flux HelmRelease.
// Reports the HelmRelease CR status. Managed resource discovery is handled
// separately via .status.inventory.entries[] when available.
func buildFluxHelmReleaseStatusMap(hr *unstructured.Unstructured) api.ResourceStatusMap {
	isReady, isFailed, _ := getFluxCondition(hr)

	statusMap := make(api.ResourceStatusMap)
	now := time.Now()

	var syncStatus api.ResourceSyncStatusType
	var readiness api.ResourceReadinessType
	if isReady {
		syncStatus = api.ResourceSyncStatusSynced
		readiness = api.ResourceReadinessReady
	} else if isFailed {
		syncStatus = api.ResourceSyncStatusPending
		readiness = api.ResourceReadinessFailed
	} else {
		syncStatus = api.ResourceSyncStatusPending
		readiness = api.ResourceReadinessInProgress
	}

	hrKey := buildHelmReleaseResourceKey(hr)
	statusMap[hrKey] = api.ResourceStatus{
		SyncStatus: syncStatus,
		Readiness:  readiness,
		UpdatedAt:  now,
	}

	return statusMap
}

// buildHelmReleaseResourceKey constructs a ResourceTypeAndName key for a HelmRelease CR.
func buildHelmReleaseResourceKey(hr *unstructured.Unstructured) funcApi.ResourceTypeAndName {
	return buildResourceKey("helm.toolkit.fluxcd.io", "v2", fluxKindHelmRelease, hr.GetNamespace(), hr.GetName())
}

// watchFluxHelmRelease polls a Flux HelmRelease until it is Ready or timed out.
func (w *FluxOCIWorker) watchFluxHelmRelease(
	wctx api.BridgeWorkerContext,
	payload api.BridgeWorkerPayload,
	options FluxOCIBridgeOptions,
	hrObj *unstructured.Unstructured,
	helmRepoObj *unstructured.Unstructured,
	originalData []byte,
) error {
	hrName := hrObj.GetName()
	hrNamespace := hrObj.GetNamespace()
	if hrNamespace == "" {
		hrNamespace = options.FluxNamespace
	}

	k8sClient, _, err := kubernetes.KubernetesClientFactory(options.KubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		), err)
	}

	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Waiting for Flux HelmRelease to reconcile...",
	)); err != nil {
		return err
	}

	var timeout time.Duration
	if options.WaitTimeout != "" {
		if t, parseErr := time.ParseDuration(options.WaitTimeout); parseErr == nil {
			timeout = t
		}
	}
	if timeout == 0 {
		timeout = kubernetes.LargeWaitTimeout
	}

	ctx := wctx.Context()
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			log.Log.Info("Flux HelmRelease WatchForApply cancelled")
			return nil
		default:
		}

		if time.Since(startTime) > timeout {
			return lib.SafeSendStatus(wctx, common.NewActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyWaitFailed,
				fmt.Sprintf("timed out waiting for Flux HelmRelease %s/%s to reconcile", hrNamespace, hrName),
			), context.DeadlineExceeded)
		}

		liveHR := &unstructured.Unstructured{}
		liveHR.SetGroupVersionKind(hrObj.GroupVersionKind())
		if err := k8sClient.Get(ctx, client.ObjectKey{
			Namespace: hrNamespace,
			Name:      hrName,
		}, liveHR); err != nil {
			log.Log.Error(err, "Failed to get Flux HelmRelease, will retry", "name", hrName, "namespace", hrNamespace)
			time.Sleep(defaultPollInterval)
			continue
		}

		isReady, isFailed, condMsg := getFluxCondition(liveHR)
		resourceStatuses := buildFluxHelmReleaseStatusMap(liveHR)

		log.Log.Info("Flux HelmRelease status",
			"name", hrName,
			"isReady", isReady,
			"isFailed", isFailed,
			"message", condMsg,
		)

		progressStatus := common.NewActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			fmt.Sprintf("Flux HelmRelease %s: ready=%v, message=%s", hrName, isReady, condMsg),
		)
		progressStatus.ResourceStatuses = resourceStatuses
		if err := wctx.SendStatus(progressStatus); err != nil {
			return err
		}

		if isFailed {
			return lib.SafeSendStatus(wctx, common.NewActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyWaitFailed,
				fmt.Sprintf("Flux HelmRelease reconciliation failed for %s/%s: %s", hrNamespace, hrName, condMsg),
			), fmt.Errorf("Flux HelmRelease reconciliation failed: %s", condMsg))
		}

		if isReady {
			liveStateYAML, liveDataYAML, liveErr := computeManagedResourceState(ctx, k8sClient, liveHR, originalData)
			if liveErr != nil {
				log.Log.Error(liveErr, "Failed to fetch managed resources")
			}

			select {
			case <-wctx.Context().Done():
				log.Log.Info("Flux HelmRelease apply operation was cancelled/overridden, skipping completion status")
				return nil
			default:
			}

			status := common.NewActionResult(
				api.ActionStatusCompleted,
				api.ActionResultApplyCompleted,
				fmt.Sprintf("Flux HelmRelease %s reconciled successfully at %s", hrName, time.Now().Format(time.RFC3339)),
			)
			status.ResourceStatuses = resourceStatuses
			status.LiveState = []byte(liveStateYAML)
			status.LiveData = []byte(liveDataYAML)

			// BridgeState = inventory ConfigMap tracking the Flux HelmRelease and HelmRepository CRs
			appliedObjects, _ := kubernetes.ParseObjects(payload.Data)
			status.BridgeState = w.BuildBridgeState(payload, appliedObjects)

			_ = wctx.SendStatus(status)
			return nil
		}

		time.Sleep(defaultPollInterval)
	}
}

// refreshFluxHelmRelease refreshes the state of a Flux HelmRelease.
// Uses .status.inventory.entries[] for managed resource discovery.
// If no inventory entries are found, LiveState and LiveData will be empty.
func (w *FluxOCIWorker) refreshFluxHelmRelease(
	wctx api.BridgeWorkerContext,
	payload api.BridgeWorkerPayload,
	options FluxOCIBridgeOptions,
	expectedHR *unstructured.Unstructured,
	expectedHelmRepo *unstructured.Unstructured,
	originalData []byte,
	refreshParams *api.RefreshParams,
) error {
	k8sClient, _, err := kubernetes.KubernetesClientFactory(options.KubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		), err)
	}

	hrNamespace := expectedHR.GetNamespace()
	if hrNamespace == "" {
		hrNamespace = options.FluxNamespace
	}
	hrName := expectedHR.GetName()

	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Retrieving Flux HelmRelease state...",
	)); err != nil {
		return err
	}

	liveHR := &unstructured.Unstructured{}
	liveHR.SetGroupVersionKind(expectedHR.GroupVersionKind())
	if err := k8sClient.Get(wctx.Context(), client.ObjectKey{
		Namespace: hrNamespace,
		Name:      hrName,
	}, liveHR); err != nil {
		if apierrors.IsNotFound(err) {
			return wctx.SendStatus(common.NewActionResult(
				api.ActionStatusCompleted,
				api.ActionResultRefreshAndDrifted,
				fmt.Sprintf("Flux HelmRelease %s/%s not found - drift detected", hrNamespace, hrName),
			))
		}
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to get HelmRelease CR: %v", err),
		), err)
	}

	isReady, _, condMsg := getFluxCondition(liveHR)
	resourceStatuses := buildFluxHelmReleaseStatusMap(liveHR)

	log.Log.Info("Flux HelmRelease refresh",
		"name", hrName,
		"isReady", isReady,
		"message", condMsg,
	)

	syncDrifted := !isReady
	contentDrifted := false
	var patchedData []byte

	liveStateYAML, liveDataYAML, liveErr := computeManagedResourceState(wctx.Context(), k8sClient, liveHR, originalData)
	if liveErr != nil {
		log.Log.Error(liveErr, "Failed to fetch managed resources")
	}

	if liveDataYAML != "" {
		// Content drift detection via diff-patch.
		//
		// "Base data" is the last-known-good revision — the YAML that was last
		// successfully applied to the cluster. We compare it against LiveData
		// (what's actually running now) to detect whether someone changed the
		// cluster state outside of ConfigHub. If BaseRevisionData is not
		// available (e.g. first refresh after import), we fall back to the
		// current revision's rendered data (originalData).
		var baseData []byte
		if refreshParams != nil && len(refreshParams.BaseRevisionData) > 0 {
			baseData = refreshParams.BaseRevisionData
		} else {
			baseData = originalData
		}

		cleanedBaseData, cleanErr := kubernetes.CleanBaseDataForDrift(baseData)
		if cleanErr != nil {
			log.Log.Error(cleanErr, "Failed to clean base data for drift comparison")
			cleanedBaseData = baseData
		}

		patched, drifted, diffErr := yamlkit.DiffPatchWithOptions(cleanedBaseData, []byte(liveDataYAML), originalData, w.GetResourceProvider(), false, nil)
		if diffErr != nil {
			log.Log.Error(diffErr, "Failed to diff-patch managed resources")
		} else {
			contentDrifted = drifted
			if drifted {
				patchedData = patched
			}
		}
	}

	isDrifted := syncDrifted || contentDrifted

	var resultType api.ActionResultType
	var message string
	if isDrifted {
		resultType = api.ActionResultRefreshAndDrifted
		message = fmt.Sprintf("Flux HelmRelease %s: ready=%v - drift detected", hrName, isReady)
	} else {
		resultType = api.ActionResultRefreshAndNoDrift
		message = fmt.Sprintf("Flux HelmRelease %s: ready=%v - no drift", hrName, isReady)
	}

	result := common.NewActionResult(api.ActionStatusCompleted, resultType, message)
	result.LiveData = []byte(liveDataYAML)
	result.LiveState = []byte(liveStateYAML)
	result.ResourceStatuses = resourceStatuses
	if contentDrifted && len(patchedData) > 0 {
		result.Data = patchedData
	}

	// BridgeState = inventory ConfigMap tracking the Flux HelmRelease and HelmRepository CRs
	appliedObjects, _ := kubernetes.ParseObjects(payload.Data)
	result.BridgeState = w.BuildBridgeState(payload, appliedObjects)

	return wctx.SendStatus(result)
}
