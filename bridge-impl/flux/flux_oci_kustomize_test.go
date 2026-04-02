// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package flux

import (
	"encoding/json"
	"testing"

	"github.com/confighub/sdk/bridge-impl/helmutils"
	"github.com/confighub/sdk/bridge-impl/kubernetes"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestExtractFluxInventoryObjects_ValidEntries(t *testing.T) {
	ks := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"inventory": map[string]interface{}{
					"entries": []interface{}{
						map[string]interface{}{"id": "default_my-deploy_apps_Deployment", "v": "v1"},
						map[string]interface{}{"id": "default_my-svc__Service", "v": "v1"},
					},
				},
			},
		},
	}

	objects := extractFluxInventoryObjects(ks)
	assert.Len(t, objects, 2)

	assert.Equal(t, "Deployment", objects[0].GetKind())
	assert.Equal(t, "my-deploy", objects[0].GetName())
	assert.Equal(t, "default", objects[0].GetNamespace())
	gvk0 := objects[0].GroupVersionKind()
	assert.Equal(t, "apps", gvk0.Group)
	assert.Equal(t, "v1", gvk0.Version)

	assert.Equal(t, "Service", objects[1].GetKind())
	assert.Equal(t, "my-svc", objects[1].GetName())
	gvk1 := objects[1].GroupVersionKind()
	assert.Equal(t, "", gvk1.Group)
	assert.Equal(t, "v1", gvk1.Version)
}

func TestExtractFluxInventoryObjects_MalformedEntries(t *testing.T) {
	ks := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"inventory": map[string]interface{}{
					"entries": []interface{}{
						map[string]interface{}{"id": "bad_entry", "v": "v1"},        // too few parts
						map[string]interface{}{"id": "", "v": "v1"},                 // empty id
						map[string]interface{}{"id": "ns_name_g_Kind", "v": "v1"},   // valid
						map[string]interface{}{"id": "ns_name_g_Kind"},              // missing v
					},
				},
			},
		},
	}

	objects := extractFluxInventoryObjects(ks)
	assert.Len(t, objects, 1)
	assert.Equal(t, "Kind", objects[0].GetKind())
}

func TestExtractFluxInventoryObjects_Empty(t *testing.T) {
	ks := &unstructured.Unstructured{
		Object: map[string]interface{}{},
	}

	objects := extractFluxInventoryObjects(ks)
	assert.Nil(t, objects)
}

func TestBuildFluxResourceStatusMap_ReadyWithInventory(t *testing.T) {
	ks := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fluxKustomizationAPIVersion,
			"kind":       fluxKindKustomization,
			"metadata": map[string]interface{}{
				"name":      "test-ks",
				"namespace": "flux-system",
			},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   fluxConditionReady,
						"status": fluxConditionStatusTrue,
					},
				},
				"inventory": map[string]interface{}{
					"entries": []interface{}{
						map[string]interface{}{"id": "default_my-deploy_apps_Deployment", "v": "v1"},
						map[string]interface{}{"id": "default_my-svc__Service", "v": "v1"},
					},
				},
			},
		},
	}

	statusMap := buildFluxResourceStatusMap(ks)

	// Should have 3 entries: 1 Kustomization + 2 inventory objects
	assert.Len(t, statusMap, 3)

	for _, status := range statusMap {
		assert.Equal(t, api.ResourceSyncStatusSynced, status.SyncStatus)
		assert.Equal(t, api.ResourceReadinessReady, status.Readiness)
	}
}

func TestBuildFluxResourceStatusMap_FailedWithInventory(t *testing.T) {
	ks := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fluxKustomizationAPIVersion,
			"kind":       fluxKindKustomization,
			"metadata": map[string]interface{}{
				"name":      "test-ks",
				"namespace": "flux-system",
			},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"type":    fluxConditionReady,
						"status":  fluxConditionStatusFalse,
						"message": "apply failed",
					},
				},
				"inventory": map[string]interface{}{
					"entries": []interface{}{
						map[string]interface{}{"id": "default_my-deploy_apps_Deployment", "v": "v1"},
					},
				},
			},
		},
	}

	statusMap := buildFluxResourceStatusMap(ks)
	assert.Len(t, statusMap, 2)

	for _, status := range statusMap {
		assert.Equal(t, api.ResourceSyncStatusPending, status.SyncStatus)
		assert.Equal(t, api.ResourceReadinessFailed, status.Readiness)
	}
}

func TestBuildFluxResourceStatusMap_NoConditionNoInventory(t *testing.T) {
	ks := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fluxKustomizationAPIVersion,
			"kind":       fluxKindKustomization,
			"metadata": map[string]interface{}{
				"name":      "test-ks",
				"namespace": "flux-system",
			},
		},
	}

	statusMap := buildFluxResourceStatusMap(ks)
	// Only the Kustomization itself (no inventory)
	assert.Len(t, statusMap, 1)

	for _, status := range statusMap {
		assert.Equal(t, api.ResourceSyncStatusPending, status.SyncStatus)
		assert.Equal(t, api.ResourceReadinessInProgress, status.Readiness)
	}
}

func TestGenerateFluxOCIRepository_Insecure(t *testing.T) {
	args := &fluxOCIArgs{
		Name:           "test-app",
		FluxNamespace:  "flux-system",
		UnitSlug:       "test-unit",
		UnitID:         "unit-123",
		SpaceID:        "space-123",
		RevisionNum:    "1",
		OCIRepoURL:     "oci://localhost:5000/test/manifests",
		OCIPath:        ".",
		TargetRevision: "latest",
		Interval:       "10m",
		Insecure:       true,
	}

	yamlBytes, err := generateFluxOCIRepository(args)
	assert.NoError(t, err)

	yamlStr := string(yamlBytes)
	assert.Contains(t, yamlStr, "insecure: true")
}

func TestGenerateFluxOCIRepository_Secure(t *testing.T) {
	args := &fluxOCIArgs{
		Name:           "test-app",
		FluxNamespace:  "flux-system",
		UnitSlug:       "test-unit",
		UnitID:         "unit-123",
		SpaceID:        "space-123",
		RevisionNum:    "1",
		OCIRepoURL:     "oci://ghcr.io/test/manifests",
		OCIPath:        ".",
		TargetRevision: "latest",
		Interval:       "10m",
		Insecure:       false,
	}

	yamlBytes, err := generateFluxOCIRepository(args)
	assert.NoError(t, err)

	yamlStr := string(yamlBytes)
	assert.NotContains(t, yamlStr, "insecure")
}

func TestGenerateFluxOCIRepository_Labels(t *testing.T) {
	args := &fluxOCIArgs{
		Name:           "test-app",
		FluxNamespace:  "flux-system",
		UnitSlug:       "test-unit",
		UnitID:         "unit-123",
		SpaceID:        "space-123",
		RevisionNum:    "1",
		OCIRepoURL:     "oci://ghcr.io/test/manifests",
		OCIPath:        ".",
		TargetRevision: "latest",
		Interval:       "10m",
	}

	yamlBytes, err := generateFluxOCIRepository(args)
	assert.NoError(t, err)

	yamlStr := string(yamlBytes)
	assert.Contains(t, yamlStr, "app.kubernetes.io/managed-by: flux-oci-bridge")
}

func TestGenerateFluxKustomization_Labels(t *testing.T) {
	args := &fluxOCIArgs{
		Name:            "test-app",
		FluxNamespace:   "flux-system",
		UnitSlug:        "test-unit",
		UnitID:          "unit-123",
		SpaceID:         "space-123",
		RevisionNum:     "1",
		OCIRepoURL:      "oci://ghcr.io/test/manifests",
		OCIPath:         ".",
		TargetRevision:  "latest",
		TargetNamespace: "default",
		Interval:        "10m",
		Prune:           true,
	}

	yamlBytes, err := generateFluxKustomization(args)
	assert.NoError(t, err)

	yamlStr := string(yamlBytes)
	assert.Contains(t, yamlStr, "app.kubernetes.io/managed-by: flux-oci-bridge")
}

func TestParseFluxOCIParams_PruneDefaultTrue(t *testing.T) {
	payload := api.BridgeWorkerPayload{}
	options, err := parseFluxOCIOptions(payload)
	require.NoError(t, err)
	assert.True(t, options.Prune, "Prune should default to true when TargetParams is empty")
}

func TestParseFluxOCIParams_PruneExplicitFalse(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"Prune": "false"},
	}
	options, err := parseFluxOCIOptions(payload)
	require.NoError(t, err)
	assert.False(t, options.Prune, "Prune should be false when explicitly set to false")
}

func TestParseFluxOCIParams_Defaults(t *testing.T) {
	payload := api.BridgeWorkerPayload{}
	options, err := parseFluxOCIOptions(payload)
	require.NoError(t, err)

	assert.Equal(t, defaultFluxNamespace, options.FluxNamespace)
	assert.Equal(t, defaultDestinationNamespace, options.TargetNamespace)
	assert.Equal(t, defaultFluxInterval, options.Interval)
	assert.Equal(t, defaultFluxOCIPath, options.OCIPath)
	assert.Equal(t, defaultFluxTargetRevision, options.TargetRevision)
	assert.Equal(t, kubernetes.LargeWaitTimeout.String(), options.WaitTimeout)
	assert.True(t, options.Prune)
}

func TestParseFluxOCIParams_TargetOptions(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{
			"FluxNamespace":    "my-flux",
			"TargetNamespace":  "my-ns",
			"Interval":         "5m",
			"OCIRepoURL":       "oci://registry.example.com/charts",
			"OCIHost":          "registry.example.com",
			"OCIPath":          "manifests",
			"TargetRevision":   "v1.0.0",
			"Prune":            "false",
			"DisableRepoCreds": "true",
			"KubeContext":      "my-cluster",
			"WaitTimeout":      "20m",
		},
	}
	options, err := parseFluxOCIOptions(payload)
	require.NoError(t, err)

	assert.Equal(t, "my-flux", options.FluxNamespace)
	assert.Equal(t, "my-ns", options.TargetNamespace)
	assert.Equal(t, "5m", options.Interval)
	assert.Equal(t, "oci://registry.example.com/charts", options.OCIRepoURL)
	assert.Equal(t, "registry.example.com", options.OCIHost)
	assert.Equal(t, "manifests", options.OCIPath)
	assert.Equal(t, "v1.0.0", options.TargetRevision)
	assert.False(t, options.Prune)
	assert.True(t, options.DisableRepoCreds)
	assert.Equal(t, "my-cluster", options.KubeContext)
	assert.Equal(t, "20m", options.WaitTimeout)
}


func TestGenerateFluxOCICreds_Valid(t *testing.T) {
	yamlBytes, err := generateFluxOCICreds("registry.example.com", "flux-system", "worker-id", "worker-secret")
	require.NoError(t, err)

	yamlStr := string(yamlBytes)
	assert.Contains(t, yamlStr, "confighub-oci-creds-")
	assert.Contains(t, yamlStr, "kubernetes.io/dockerconfigjson")
	assert.Contains(t, yamlStr, ".dockerconfigjson")
	assert.Contains(t, yamlStr, "registry.example.com")
}

func TestGenerateFluxOCICreds_SpecialChars(t *testing.T) {
	// Credentials with JSON-special characters that would break fmt.Sprintf
	yamlBytes, err := generateFluxOCICreds("registry.example.com", "flux-system", `user"name`, `pass"word\with\\special`)
	require.NoError(t, err)

	yamlStr := string(yamlBytes)
	assert.Contains(t, yamlStr, ".dockerconfigjson")

	// Verify the embedded JSON is valid by extracting and parsing it
	// The YAML contains stringData with the JSON value
	assert.Contains(t, yamlStr, "registry.example.com")

	// Parse the generated Secret YAML to extract dockerconfigjson
	objects, err := kubernetes.ParseObjects(yamlBytes)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	stringData, found, err := unstructured.NestedStringMap(objects[0].Object, "stringData")
	require.NoError(t, err)
	require.True(t, found)

	dockerCfgJSON := stringData[".dockerconfigjson"]
	var cfg dockerConfig
	err = json.Unmarshal([]byte(dockerCfgJSON), &cfg)
	require.NoError(t, err, "Docker config JSON should be valid JSON even with special characters")

	authEntry, ok := cfg.Auths["registry.example.com"]
	require.True(t, ok)
	assert.Equal(t, `user"name`, authEntry.Username)
	assert.Equal(t, `pass"word\with\\special`, authEntry.Password)
}

// --- transformToFluxOCI tests ---

var testFluxOCITargetOptions = map[string]string{
	"KubeContext":     "test-context",
	"FluxNamespace":   "flux-system",
	"TargetNamespace": "production",
	"Interval":        "5m",
	"OCIRepoURL":      "oci://ghcr.io/myorg/manifests",
	"OCIPath":         "apps/myapp",
	"TargetRevision":  "v1.0.0",
}

func TestTransformToFluxOCI_Success(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockCtx.On("GetServerURL").Return("https://app.confighub.com")

	spaceID := uuid.New()
	unitID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testFluxOCITargetOptions,
		UnitSlug:     "my-app",
		UnitID:       unitID,
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	worker := NewFluxOCIWorker("", "")
	_, err := worker.transformToFluxOCI(mockCtx, &payload, false)
	require.NoError(t, err)

	yamlStr := string(payload.Data)
	assert.Contains(t, yamlStr, "apiVersion: source.toolkit.fluxcd.io/v1")
	assert.Contains(t, yamlStr, "kind: OCIRepository")
	assert.Contains(t, yamlStr, "kind: Kustomization")
	assert.Contains(t, yamlStr, "name: test-space-my-app")
	assert.Contains(t, yamlStr, "oci://ghcr.io/myorg/manifests")
	// Verify external link annotation
	assert.Contains(t, yamlStr, "link.argocd.argoproj.io/external-link: https://app.confighub.com/units/"+spaceID.String()+"/"+unitID.String())
}

func TestTransformToFluxOCI_InferredOCIHost(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockCtx.On("GetServerURL").Return("https://hub.confighub.com")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		UnitSlug:     "my-deployment",
		SpaceSlug:    "production",
		SpaceID:      spaceID,
		RevisionNum:  5,
		Data:         testConfigMapYAML,
	}

	worker := NewFluxOCIWorker("", "")
	_, err := worker.transformToFluxOCI(mockCtx, &payload, false)
	require.NoError(t, err)

	yamlStr := string(payload.Data)
	assert.Contains(t, yamlStr, "oci.hub.confighub.com")
	assert.Contains(t, yamlStr, "production-my-deployment")
}

func TestTransformToFluxOCI_MissingOCIConfig(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockCtx.On("GetServerURL").Return("")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		UnitSlug:     "my-app",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	worker := NewFluxOCIWorker("", "")
	_, err := worker.transformToFluxOCI(mockCtx, &payload, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot infer OCI host")
}

func TestTransformToFluxOCI_WithCreds(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockCtx.On("GetServerURL").Return("https://hub.confighub.com")

	spaceID := uuid.New()
	unitID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		UnitSlug:     "my-app",
		UnitID:       unitID,
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	worker := NewFluxOCIWorker("worker-id", "worker-secret")
	_, err := worker.transformToFluxOCI(mockCtx, &payload, false)
	require.NoError(t, err)

	yamlStr := string(payload.Data)
	// Should have Secret, OCIRepository, and Kustomization
	assert.Contains(t, yamlStr, "kind: Secret")
	assert.Contains(t, yamlStr, "kubernetes.io/dockerconfigjson")
	assert.Contains(t, yamlStr, "kind: OCIRepository")
	assert.Contains(t, yamlStr, "kind: Kustomization")
	// OCIRepository should reference the creds secret
	assert.Contains(t, yamlStr, "confighub-oci-creds-")
}

func TestTransformToFluxOCI_SkipRepoCreds(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockCtx.On("GetServerURL").Return("https://hub.confighub.com")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	worker := NewFluxOCIWorker("worker-id", "worker-secret")
	_, err := worker.transformToFluxOCI(mockCtx, &payload, true) // skipRepoCreds=true
	require.NoError(t, err)

	yamlStr := string(payload.Data)
	// Should NOT have Secret when skipRepoCreds is true
	assert.NotContains(t, yamlStr, "kind: Secret")
	assert.Contains(t, yamlStr, "kind: OCIRepository")
	assert.Contains(t, yamlStr, "kind: Kustomization")
}

func TestTransformToFluxOCI_HelmChartGeneratesHelmCRs(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockCtx.On("GetServerURL").Return("https://hub.confighub.com")

	spaceID := uuid.New()
	unitID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testFluxOCITargetOptions,
		UnitLabels: map[string]string{
			helmutils.HelmReleaseLabel:         "my-release",
			helmutils.HelmChartLabel:           "nginx",
			helmutils.HelmChartVersionLabel:    "1.2.3",
			helmutils.HelmChartAPIVersionLabel: "v2",
		},
		UnitSlug:    "my-nginx",
		UnitID:      unitID,
		SpaceSlug:   "test-space",
		SpaceID:     spaceID,
		RevisionNum: 1,
		Data:        testConfigMapYAML,
	}

	worker := NewFluxOCIWorker("", "")
	_, err := worker.transformToFluxOCI(mockCtx, &payload, false)
	require.NoError(t, err)

	yamlStr := string(payload.Data)
	// Should generate HelmRepository + HelmRelease, not OCIRepository + Kustomization
	assert.Contains(t, yamlStr, "kind: HelmRepository")
	assert.Contains(t, yamlStr, "kind: HelmRelease")
	assert.NotContains(t, yamlStr, "kind: OCIRepository")
	assert.NotContains(t, yamlStr, "kind: Kustomization")
	// HelmRepository should be type: oci
	assert.Contains(t, yamlStr, "type: oci")
	// HelmRelease chart must use UnitSlug (not HelmChartName) since ConfigHub pushes to oci://host/unit/space/<unit-slug>
	assert.Contains(t, yamlStr, "chart: my-nginx")
	assert.Contains(t, yamlStr, "version: 1.2.3")
	assert.Contains(t, yamlStr, "releaseName: my-release")
}

// --- generateFluxOCIRepository/generateFluxKustomization field value tests ---

func TestGenerateFluxOCIRepository_AllFields(t *testing.T) {
	args := &fluxOCIArgs{
		Name:           "test-app",
		FluxNamespace:  "flux-system",
		UnitSlug:       "my-unit",
		UnitID:         "unit-123",
		SpaceID:        "space-456",
		RevisionNum:    "3",
		OCIRepoURL:     "oci://registry.example.com/org/repo",
		TargetRevision: "v2.0.0",
		Interval:       "5m",
		Insecure:       false,
		SecretName:     "my-secret",
		ConfigHubURL:   "https://hub.confighub.com",
	}

	yamlBytes, err := generateFluxOCIRepository(args)
	require.NoError(t, err)

	objects, err := kubernetes.ParseObjects(yamlBytes)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	obj := objects[0]
	assert.Equal(t, fluxOCIRepoAPIVersion, obj.GetAPIVersion())
	assert.Equal(t, fluxKindOCIRepository, obj.GetKind())
	assert.Equal(t, "test-app", obj.GetName())
	assert.Equal(t, "flux-system", obj.GetNamespace())

	// spec fields
	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	assert.Equal(t, "5m", spec["interval"])
	assert.Equal(t, "oci://registry.example.com/org/repo", spec["url"])

	ref, _, _ := unstructured.NestedMap(obj.Object, "spec", "ref")
	assert.Equal(t, "v2.0.0", ref["tag"])

	secretRef, _, _ := unstructured.NestedMap(obj.Object, "spec", "secretRef")
	assert.Equal(t, "my-secret", secretRef["name"])

	// insecure should not be present when false
	_, insecureFound, _ := unstructured.NestedBool(obj.Object, "spec", "insecure")
	assert.False(t, insecureFound, "insecure should not be set when false")
}

func TestGenerateFluxOCIRepository_InsecureTrue(t *testing.T) {
	args := &fluxOCIArgs{
		Name:           "test-app",
		FluxNamespace:  "flux-system",
		UnitSlug:       "my-unit",
		UnitID:         "unit-123",
		SpaceID:        "space-456",
		RevisionNum:    "1",
		OCIRepoURL:     "oci://localhost:5000/repo",
		TargetRevision: "latest",
		Interval:       "10m",
		Insecure:       true,
		ConfigHubURL:   "https://hub.confighub.com",
	}

	yamlBytes, err := generateFluxOCIRepository(args)
	require.NoError(t, err)

	objects, err := kubernetes.ParseObjects(yamlBytes)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	insecure, found, _ := unstructured.NestedBool(objects[0].Object, "spec", "insecure")
	assert.True(t, found, "insecure should be present when true")
	assert.True(t, insecure)
}

func TestGenerateFluxOCIRepository_NoSecretRef(t *testing.T) {
	args := &fluxOCIArgs{
		Name:           "test-app",
		FluxNamespace:  "flux-system",
		UnitSlug:       "my-unit",
		UnitID:         "unit-123",
		SpaceID:        "space-456",
		RevisionNum:    "1",
		OCIRepoURL:     "oci://ghcr.io/org/repo",
		TargetRevision: "latest",
		Interval:       "10m",
		SecretName:     "", // no secret
		ConfigHubURL:   "https://hub.confighub.com",
	}

	yamlBytes, err := generateFluxOCIRepository(args)
	require.NoError(t, err)

	objects, err := kubernetes.ParseObjects(yamlBytes)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	_, secretRefFound, _ := unstructured.NestedMap(objects[0].Object, "spec", "secretRef")
	assert.False(t, secretRefFound, "secretRef should not be set when SecretName is empty")
}

func TestGenerateFluxKustomization_AllFields(t *testing.T) {
	args := &fluxOCIArgs{
		Name:            "test-app",
		FluxNamespace:   "flux-system",
		UnitSlug:        "my-unit",
		UnitID:          "unit-123",
		SpaceID:         "space-456",
		RevisionNum:     "2",
		OCIPath:         "overlays/prod",
		TargetNamespace: "production",
		Interval:        "5m",
		Prune:           true,
		ConfigHubURL:    "https://hub.confighub.com",
	}

	yamlBytes, err := generateFluxKustomization(args)
	require.NoError(t, err)

	objects, err := kubernetes.ParseObjects(yamlBytes)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	obj := objects[0]
	assert.Equal(t, fluxKustomizationAPIVersion, obj.GetAPIVersion())
	assert.Equal(t, fluxKindKustomization, obj.GetKind())
	assert.Equal(t, "test-app", obj.GetName())
	assert.Equal(t, "flux-system", obj.GetNamespace())

	// spec fields
	interval, _, _ := unstructured.NestedString(obj.Object, "spec", "interval")
	assert.Equal(t, "5m", interval)

	path, _, _ := unstructured.NestedString(obj.Object, "spec", "path")
	assert.Equal(t, "overlays/prod", path)

	prune, _, _ := unstructured.NestedBool(obj.Object, "spec", "prune")
	assert.True(t, prune)

	targetNs, _, _ := unstructured.NestedString(obj.Object, "spec", "targetNamespace")
	assert.Equal(t, "production", targetNs)

	wait, _, _ := unstructured.NestedBool(obj.Object, "spec", "wait")
	assert.True(t, wait)

	// sourceRef
	sourceRefKind, _, _ := unstructured.NestedString(obj.Object, "spec", "sourceRef", "kind")
	assert.Equal(t, fluxKindOCIRepository, sourceRefKind)

	sourceRefName, _, _ := unstructured.NestedString(obj.Object, "spec", "sourceRef", "name")
	assert.Equal(t, "test-app", sourceRefName)

	// external link annotation
	annotations := obj.GetAnnotations()
	assert.Contains(t, annotations[annotationExternalLink], "https://hub.confighub.com/units/space-456/unit-123")
}

func TestGenerateFluxKustomization_PruneFalse(t *testing.T) {
	args := &fluxOCIArgs{
		Name:            "test-app",
		FluxNamespace:   "flux-system",
		UnitSlug:        "my-unit",
		UnitID:          "unit-123",
		SpaceID:         "space-456",
		RevisionNum:     "1",
		OCIPath:         ".",
		TargetNamespace: "default",
		Interval:        "10m",
		Prune:           false,
		ConfigHubURL:    "https://hub.confighub.com",
	}

	yamlBytes, err := generateFluxKustomization(args)
	require.NoError(t, err)

	objects, err := kubernetes.ParseObjects(yamlBytes)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	prune, _, _ := unstructured.NestedBool(objects[0].Object, "spec", "prune")
	assert.False(t, prune)
}
