// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package flux

import (
	"testing"

	"github.com/confighub/sdk/bridge-impl/kubernetes"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGenerateFluxHelmRepository_URLStripsLastSegment(t *testing.T) {
	args := &fluxOCIArgs{
		Name:          "myspace-myapp",
		FluxNamespace: "flux-system",
		UnitSlug:      "myapp",
		UnitID:        "unit-123",
		SpaceID:       "space-456",
		RevisionNum:   "1",
		OCIRepoURL:    "oci://oci.hub.confighub.com/unit/myspace/myapp",
		Interval:      "10m",
		ConfigHubURL:  "https://hub.confighub.com",
	}

	yamlBytes, err := generateFluxHelmRepository(args)
	require.NoError(t, err)

	objects, err := kubernetes.ParseObjects(yamlBytes)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	obj := objects[0]
	assert.Equal(t, fluxHelmRepoAPIVersion, obj.GetAPIVersion())
	assert.Equal(t, fluxKindHelmRepository, obj.GetKind())
	assert.Equal(t, "myspace-myapp", obj.GetName())
	assert.Equal(t, "flux-system", obj.GetNamespace())

	url, _, _ := unstructured.NestedString(obj.Object, "spec", "url")
	// Last path segment (myapp) stripped: only the space prefix remains
	assert.Equal(t, "oci://oci.hub.confighub.com/unit/myspace", url)

	repoType, _, _ := unstructured.NestedString(obj.Object, "spec", "type")
	assert.Equal(t, "oci", repoType)
}

func TestGenerateFluxHelmRepository_Insecure(t *testing.T) {
	args := &fluxOCIArgs{
		Name:          "test-app",
		FluxNamespace: "flux-system",
		UnitSlug:      "test-unit",
		UnitID:        "unit-123",
		SpaceID:       "space-123",
		RevisionNum:   "1",
		OCIRepoURL:    "oci://localhost:5000/unit/myspace/myapp",
		Interval:      "10m",
		Insecure:      true,
		ConfigHubURL:  "https://hub.confighub.com",
	}

	yamlBytes, err := generateFluxHelmRepository(args)
	require.NoError(t, err)

	yamlStr := string(yamlBytes)
	assert.Contains(t, yamlStr, "insecure: true")
	assert.Contains(t, yamlStr, "type: oci")
}

func TestGenerateFluxHelmRepository_SecretRef(t *testing.T) {
	args := &fluxOCIArgs{
		Name:          "test-app",
		FluxNamespace: "flux-system",
		UnitSlug:      "test-unit",
		UnitID:        "unit-123",
		SpaceID:       "space-123",
		RevisionNum:   "1",
		OCIRepoURL:    "oci://registry.example.com/unit/myspace/myapp",
		Interval:      "10m",
		SecretName:    "confighub-oci-creds-registry-example-com",
		ConfigHubURL:  "https://hub.confighub.com",
	}

	yamlBytes, err := generateFluxHelmRepository(args)
	require.NoError(t, err)

	objects, err := kubernetes.ParseObjects(yamlBytes)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	secretName, _, _ := unstructured.NestedString(objects[0].Object, "spec", "secretRef", "name")
	assert.Equal(t, "confighub-oci-creds-registry-example-com", secretName)
}

func TestGenerateFluxHelmRepository_Labels(t *testing.T) {
	args := &fluxOCIArgs{
		Name:          "test-app",
		FluxNamespace: "flux-system",
		UnitSlug:      "test-unit",
		UnitID:        "unit-123",
		SpaceID:       "space-123",
		RevisionNum:   "1",
		OCIRepoURL:    "oci://registry.example.com/unit/space/app",
		Interval:      "10m",
		ConfigHubURL:  "https://hub.confighub.com",
	}

	yamlBytes, err := generateFluxHelmRepository(args)
	require.NoError(t, err)

	yamlStr := string(yamlBytes)
	assert.Contains(t, yamlStr, "app.kubernetes.io/managed-by: flux-oci-bridge")
}

func TestGenerateFluxHelmRelease_AllFields(t *testing.T) {
	args := &fluxOCIArgs{
		Name:             "myspace-myapp",
		FluxNamespace:    "flux-system",
		UnitSlug:         "myapp",
		UnitID:           "unit-123",
		SpaceID:          "space-456",
		RevisionNum:      "2",
		OCIRepoURL:       "oci://oci.host/unit/myspace/myapp",
		TargetNamespace:  "production",
		Interval:         "5m",
		ConfigHubURL:     "https://hub.confighub.com",
		IsHelm:           true,
		HelmReleaseName:  "my-release",
		HelmChartName:    "nginx",
		HelmChartVersion: "1.2.3",
	}

	yamlBytes, err := generateFluxHelmRelease(args)
	require.NoError(t, err)

	objects, err := kubernetes.ParseObjects(yamlBytes)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	obj := objects[0]
	assert.Equal(t, fluxHelmReleaseAPIVersion, obj.GetAPIVersion())
	assert.Equal(t, fluxKindHelmRelease, obj.GetKind())
	assert.Equal(t, "myspace-myapp", obj.GetName())
	assert.Equal(t, "flux-system", obj.GetNamespace())

	releaseName, _, _ := unstructured.NestedString(obj.Object, "spec", "releaseName")
	assert.Equal(t, "my-release", releaseName)

	targetNS, _, _ := unstructured.NestedString(obj.Object, "spec", "targetNamespace")
	assert.Equal(t, "production", targetNS)

	chartName, _, _ := unstructured.NestedString(obj.Object, "spec", "chart", "spec", "chart")
	assert.Equal(t, "myapp", chartName) // chart must use UnitSlug, not HelmChartName

	chartVersion, _, _ := unstructured.NestedString(obj.Object, "spec", "chart", "spec", "version")
	assert.Equal(t, "1.2.3", chartVersion)

	sourceRefKind, _, _ := unstructured.NestedString(obj.Object, "spec", "chart", "spec", "sourceRef", "kind")
	assert.Equal(t, fluxKindHelmRepository, sourceRefKind)

	sourceRefName, _, _ := unstructured.NestedString(obj.Object, "spec", "chart", "spec", "sourceRef", "name")
	assert.Equal(t, "myspace-myapp", sourceRefName)
}

func TestGenerateFluxHelmRelease_NoReleaseName(t *testing.T) {
	args := &fluxOCIArgs{
		Name:             "myspace-myapp",
		FluxNamespace:    "flux-system",
		UnitSlug:         "myapp",
		UnitID:           "unit-123",
		SpaceID:          "space-456",
		RevisionNum:      "1",
		OCIRepoURL:       "oci://registry.example.com/unit/space/myapp",
		TargetNamespace:  "default",
		Interval:         "10m",
		ConfigHubURL:     "https://hub.confighub.com",
		HelmChartName:    "mychart",
		HelmChartVersion: "2.0.0",
		HelmReleaseName:  "", // empty — should not appear in output
	}

	yamlBytes, err := generateFluxHelmRelease(args)
	require.NoError(t, err)

	yamlStr := string(yamlBytes)
	assert.NotContains(t, yamlStr, "releaseName")
}

func TestFindFluxHelmReleaseObject(t *testing.T) {
	hrYAML, err := generateFluxHelmRelease(&fluxOCIArgs{
		Name:             "test",
		FluxNamespace:    "flux-system",
		UnitSlug:         "test",
		UnitID:           "u1",
		SpaceID:          "s1",
		RevisionNum:      "1",
		TargetNamespace:  "default",
		Interval:         "10m",
		HelmChartName:    "chart",
		HelmChartVersion: "1.0.0",
	})
	require.NoError(t, err)

	objects, err := kubernetes.ParseObjects(hrYAML)
	require.NoError(t, err)

	found := findFluxHelmReleaseObject(objects)
	require.NotNil(t, found)
	assert.Equal(t, fluxKindHelmRelease, found.GetKind())
}

func TestFindFluxHelmRepositoryObject(t *testing.T) {
	repoYAML, err := generateFluxHelmRepository(&fluxOCIArgs{
		Name:          "test",
		FluxNamespace: "flux-system",
		UnitSlug:      "test",
		UnitID:        "u1",
		SpaceID:       "s1",
		RevisionNum:   "1",
		OCIRepoURL:    "oci://registry.example.com/unit/space/test",
		Interval:      "10m",
		ConfigHubURL:  "https://hub.confighub.com",
	})
	require.NoError(t, err)

	objects, err := kubernetes.ParseObjects(repoYAML)
	require.NoError(t, err)

	found := findFluxHelmRepositoryObject(objects)
	require.NotNil(t, found)
	assert.Equal(t, fluxKindHelmRepository, found.GetKind())
}

func TestBuildFluxHelmReleaseStatusMap_Ready(t *testing.T) {
	hr := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fluxHelmReleaseAPIVersion,
			"kind":       fluxKindHelmRelease,
			"metadata": map[string]interface{}{
				"name":      "test-hr",
				"namespace": "flux-system",
			},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   fluxConditionReady,
						"status": fluxConditionStatusTrue,
					},
				},
			},
		},
	}

	statusMap := buildFluxHelmReleaseStatusMap(hr)
	assert.Len(t, statusMap, 1)

	for _, status := range statusMap {
		assert.Equal(t, api.ResourceSyncStatusSynced, status.SyncStatus)
		assert.Equal(t, api.ResourceReadinessReady, status.Readiness)
	}
}

func TestBuildFluxHelmReleaseStatusMap_Failed(t *testing.T) {
	hr := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fluxHelmReleaseAPIVersion,
			"kind":       fluxKindHelmRelease,
			"metadata": map[string]interface{}{
				"name":       "test-hr",
				"namespace":  "flux-system",
				"generation": int64(1),
			},
			"status": map[string]interface{}{
				// observedGeneration must equal generation; otherwise the
				// Ready=False condition is treated as still-reconciling.
				"observedGeneration": int64(1),
				"conditions": []interface{}{
					map[string]interface{}{
						"type":    fluxConditionReady,
						"status":  fluxConditionStatusFalse,
						"message": "install retries exhausted",
					},
				},
			},
		},
	}

	statusMap := buildFluxHelmReleaseStatusMap(hr)
	assert.Len(t, statusMap, 1)

	for _, status := range statusMap {
		assert.Equal(t, api.ResourceSyncStatusPending, status.SyncStatus)
		assert.Equal(t, api.ResourceReadinessFailed, status.Readiness)
	}
}

func TestBuildFluxHelmReleaseStatusMap_NoCondition(t *testing.T) {
	hr := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fluxHelmReleaseAPIVersion,
			"kind":       fluxKindHelmRelease,
			"metadata": map[string]interface{}{
				"name":      "test-hr",
				"namespace": "flux-system",
			},
		},
	}

	statusMap := buildFluxHelmReleaseStatusMap(hr)
	assert.Len(t, statusMap, 1)

	for _, status := range statusMap {
		assert.Equal(t, api.ResourceSyncStatusPending, status.SyncStatus)
		assert.Equal(t, api.ResourceReadinessInProgress, status.Readiness)
	}
}
