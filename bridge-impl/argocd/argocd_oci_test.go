// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package argocd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cenkalti/backoff/v5"
	"github.com/confighub/sdk/bridge-impl/helmutils"
	"github.com/confighub/sdk/bridge-impl/kubernetes"
	"github.com/confighub/sdk/configkit/k8skit"
	funcApi "github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mockNotFoundError implements apierrors.IsNotFound check
type mockNotFoundError struct{}

func (e *mockNotFoundError) Error() string {
	return "not found"
}

func (e *mockNotFoundError) Status() metav1.Status {
	return metav1.Status{Reason: metav1.StatusReasonNotFound}
}

// mockForbiddenError implements apierrors.IsForbidden check
type mockForbiddenError struct{}

func (e *mockForbiddenError) Error() string {
	return "configmaps is forbidden: User \"system:serviceaccount:default:test\" cannot get resource \"configmaps\""
}

func (e *mockForbiddenError) Status() metav1.Status {
	return metav1.Status{
		Status:  metav1.StatusFailure,
		Reason:  metav1.StatusReasonForbidden,
		Message: e.Error(),
		Code:    403,
	}
}

// Test data for ArgoCD OCI worker
var (
	testArgoCDOCITargetOptions = map[string]string{
		"KubeContext":          "test-context",
		"ArgoCDNamespace":      "argocd",
		"DestinationServer":    "https://kubernetes.default.svc",
		"DestinationNamespace": "production",
		"Project":              "my-project",
		"SyncPolicy":           "automated",
		"PruneEnabled":         "true",
		"SelfHealEnabled":      "true",
		"OCIRepoURL":           "oci://ghcr.io/myorg/manifests",
		"OCIPath":              "apps/myapp",
		"TargetRevision":       "v1.0.0",
	}

	testArgoCDOCIMinimalOptions = map[string]string{
		"KubeContext": "test-context",
		"OCIRepoURL":  "oci://ghcr.io/myorg/manifests",
	}
)

func TestParseArgoCDOCIParams_FullParams(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
	}

	options, err := parseArgoCDOCIOptions(payload)
	assert.NoError(t, err)
	assert.Equal(t, "test-context", options.KubeContext)
	assert.Equal(t, "argocd", options.ArgoCDNamespace)
	assert.Equal(t, "https://kubernetes.default.svc", options.DestinationServer)
	assert.Equal(t, "production", options.DestinationNamespace)
	assert.Equal(t, "my-project", options.Project)
	assert.Equal(t, "automated", options.SyncPolicy)
	assert.True(t, options.PruneEnabled)
	assert.True(t, options.SelfHealEnabled)
	assert.Equal(t, "oci://ghcr.io/myorg/manifests", options.OCIRepoURL)
	assert.Equal(t, "apps/myapp", options.OCIPath)
	assert.Equal(t, "v1.0.0", options.TargetRevision)
}

func TestParseArgoCDOCIParams_WithDefaults(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCIMinimalOptions,
	}

	options, err := parseArgoCDOCIOptions(payload)
	assert.NoError(t, err)
	assert.Equal(t, "test-context", options.KubeContext)
	assert.Equal(t, defaultArgoCDNamespace, options.ArgoCDNamespace)
	assert.Equal(t, defaultDestinationServer, options.DestinationServer)
	assert.Equal(t, defaultDestinationNamespace, options.DestinationNamespace)
	assert.Equal(t, defaultProject, options.Project)
	assert.Equal(t, defaultSyncPolicy, options.SyncPolicy)
	assert.False(t, options.PruneEnabled)
	assert.False(t, options.SelfHealEnabled)
	assert.Equal(t, "oci://ghcr.io/myorg/manifests", options.OCIRepoURL)
	assert.Equal(t, defaultOCIPath, options.OCIPath)
	assert.Equal(t, defaultTargetRevision, options.TargetRevision)
}

func TestParseArgoCDOCIParams_EmptyParams(t *testing.T) {
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{},
	}

	options, err := parseArgoCDOCIOptions(payload)
	assert.NoError(t, err)
	// All defaults should be applied
	assert.Equal(t, defaultArgoCDNamespace, options.ArgoCDNamespace)
	assert.Equal(t, defaultDestinationServer, options.DestinationServer)
	assert.Equal(t, defaultDestinationNamespace, options.DestinationNamespace)
	assert.Equal(t, defaultProject, options.Project)
	assert.Equal(t, defaultSyncPolicy, options.SyncPolicy)
	assert.Equal(t, defaultOCIPath, options.OCIPath)
	assert.Equal(t, defaultTargetRevision, options.TargetRevision)
}

func TestParseArgoCDOCIParams_EmptyOptions(t *testing.T) {
	payload := api.BridgeWorkerPayload{}

	options, err := parseArgoCDOCIOptions(payload)
	assert.NoError(t, err)
	// All defaults should be applied
	assert.Equal(t, defaultArgoCDNamespace, options.ArgoCDNamespace)
	assert.Equal(t, defaultDestinationServer, options.DestinationServer)
	assert.Equal(t, defaultDestinationNamespace, options.DestinationNamespace)
	assert.Equal(t, defaultProject, options.Project)
	assert.Equal(t, defaultSyncPolicy, options.SyncPolicy)
	assert.Equal(t, defaultOCIPath, options.OCIPath)
	assert.Equal(t, defaultTargetRevision, options.TargetRevision)
}

func TestGenerateArgoCDApplication_Success(t *testing.T) {
	args := &argoCDApplicationArgs{
		Name:                 "test-app-abc123",
		ArgoCDNamespace:      "argocd",
		UnitSlug:             "test-unit",
		SpaceID:              "space-123",
		RevisionNum:          "1",
		Project:              "default",
		OCIRepoURL:           "oci://ghcr.io/myorg/manifests",
		OCIPath:              ".",
		TargetRevision:       "latest",
		DestinationServer:    "https://kubernetes.default.svc",
		DestinationNamespace: "default",
		SyncPolicy:           "automated",
		PruneEnabled:         true,
		SelfHealEnabled:      true,
	}

	yamlBytes, err := generateArgoCDApplication(args)
	assert.NoError(t, err)
	assert.NotEmpty(t, yamlBytes)

	yamlStr := string(yamlBytes)

	// Verify the generated YAML contains expected values
	assert.Contains(t, yamlStr, "apiVersion: argoproj.io/v1alpha1")
	assert.Contains(t, yamlStr, "kind: Application")
	assert.Contains(t, yamlStr, "name: test-app-abc123")
	assert.Contains(t, yamlStr, "namespace: argocd")
	assert.Contains(t, yamlStr, "confighub.com/UnitSlug: test-unit")
	assert.Contains(t, yamlStr, "confighub.com/SpaceID: space-123")
	assert.Contains(t, yamlStr, "project: default")
	assert.Contains(t, yamlStr, "repoURL: oci://ghcr.io/myorg/manifests")
	assert.Contains(t, yamlStr, "path: .")
	assert.Contains(t, yamlStr, "targetRevision: latest")
	assert.Contains(t, yamlStr, "server: https://kubernetes.default.svc")
	assert.Contains(t, yamlStr, "prune: true")
	assert.Contains(t, yamlStr, "selfHeal: true")

	// Should also have operation.sync block
	assert.Contains(t, yamlStr, "operation:")
	assert.Contains(t, yamlStr, "  sync:")
}

func TestGenerateArgoCDApplication_ManualSyncPolicy(t *testing.T) {
	args := &argoCDApplicationArgs{
		Name:                 "test-app-manual",
		ArgoCDNamespace:      "argocd",
		UnitSlug:             "test-unit",
		SpaceID:              "space-123",
		RevisionNum:          "1",
		Project:              "default",
		OCIRepoURL:           "oci://ghcr.io/myorg/manifests",
		OCIPath:              ".",
		TargetRevision:       "latest",
		DestinationServer:    "https://kubernetes.default.svc",
		DestinationNamespace: "default",
		SyncPolicy:           "manual",
		PruneEnabled:         false,
		SelfHealEnabled:      false,
	}

	yamlBytes, err := generateArgoCDApplication(args)
	assert.NoError(t, err)

	yamlStr := string(yamlBytes)

	// Manual sync policy should have syncOptions but not automated block
	assert.Contains(t, yamlStr, "syncPolicy:")
	assert.Contains(t, yamlStr, "CreateNamespace=true")
	assert.NotContains(t, yamlStr, "automated:")
	assert.NotContains(t, yamlStr, "prune:")
	assert.NotContains(t, yamlStr, "selfHeal:")

	// But should have operation.sync block for immediate one-time sync
	assert.Contains(t, yamlStr, "operation:")
	assert.Contains(t, yamlStr, "  sync:")
}

func TestGenerateArgoCDApplication_ExternalLinkAnnotation(t *testing.T) {
	args := &argoCDApplicationArgs{
		Name:                 "test-app",
		ArgoCDNamespace:      "argocd",
		UnitSlug:             "test-unit",
		UnitID:               "unit-uuid-123",
		SpaceID:              "space-uuid-456",
		RevisionNum:          "1",
		Project:              "default",
		OCIRepoURL:           "oci://ghcr.io/myorg/manifests",
		OCIPath:              ".",
		TargetRevision:       "latest",
		DestinationServer:    "https://kubernetes.default.svc",
		DestinationNamespace: "default",
		SyncPolicy:           "manual",
		ConfigHubURL:         "https://app.confighub.com",
	}

	yamlBytes, err := generateArgoCDApplication(args)
	assert.NoError(t, err)

	yamlStr := string(yamlBytes)
	assert.Contains(t, yamlStr, "link.argocd.argoproj.io/external-link: https://app.confighub.com/units/space-uuid-456/unit-uuid-123")
}

func TestGenerateArgoCDApplication_DefaultExternalLinkWhenURLEmpty(t *testing.T) {
	args := &argoCDApplicationArgs{
		Name:                 "test-app",
		ArgoCDNamespace:      "argocd",
		UnitSlug:             "test-unit",
		UnitID:               "unit-uuid-123",
		SpaceID:              "space-uuid-456",
		RevisionNum:          "1",
		Project:              "default",
		OCIRepoURL:           "oci://ghcr.io/myorg/manifests",
		OCIPath:              ".",
		TargetRevision:       "latest",
		DestinationServer:    "https://kubernetes.default.svc",
		DestinationNamespace: "default",
		SyncPolicy:           "manual",
		ConfigHubURL:         "",
	}

	yamlBytes, err := generateArgoCDApplication(args)
	assert.NoError(t, err)

	yamlStr := string(yamlBytes)
	assert.Contains(t, yamlStr, "link.argocd.argoproj.io/external-link: https://hub.confighub.com/units/space-uuid-456/unit-uuid-123")
}

func TestTransformToArgoCDOCIApplication_Success(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("https://app.confighub.com")

	spaceID := uuid.New()
	unitID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		UnitID:       unitID,
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	worker := &ArgoCDOCIWorker{}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)

	// Verify the data was transformed to an ArgoCD Application
	yamlStr := string(payload.Data)
	assert.Contains(t, yamlStr, "apiVersion: argoproj.io/v1alpha1")
	assert.Contains(t, yamlStr, "kind: Application")
	assert.Contains(t, yamlStr, "name: test-space-my-app")
	assert.Contains(t, yamlStr, "oci://ghcr.io/myorg/manifests")
	// Verify external link annotation
	assert.Contains(t, yamlStr, "link.argocd.argoproj.io/external-link: https://app.confighub.com/units/"+spaceID.String()+"/"+unitID.String())
}

func TestTransformToArgoCDOCIApplication_MissingOCIConfig(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		UnitSlug:     "my-app",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	worker := &ArgoCDOCIWorker{}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot infer OCI host")
}

func TestTransformToArgoCDOCIApplication_InferredOCIHost(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
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

	worker := &ArgoCDOCIWorker{}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)

	yamlStr := string(payload.Data)
	// Should use stable name based on SpaceSlug + UnitSlug
	assert.Contains(t, yamlStr, "name: production-my-deployment")
	// Should infer OCI host from server URL and auto-construct the OCI URL
	assert.Contains(t, yamlStr, "repoURL: oci://oci.hub.confighub.com/unit/production/my-deployment")
	assert.Contains(t, yamlStr, "targetRevision: latest")
}

func TestTransformToArgoCDOCIApplication_AutoConstructOCIURL(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("https://app.confighub.com")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"OCIHost": "oci.hub.confighub.com"},
		UnitSlug:     "my-deployment",
		SpaceSlug:    "production",
		SpaceID:      spaceID,
		RevisionNum:  5,
		Data:         testConfigMapYAML,
	}

	worker := &ArgoCDOCIWorker{}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)

	yamlStr := string(payload.Data)
	// Should use stable name based on SpaceSlug + UnitSlug
	assert.Contains(t, yamlStr, "name: production-my-deployment")
	// Should auto-construct the OCI URL with "latest" as default
	assert.Contains(t, yamlStr, "repoURL: oci://oci.hub.confighub.com/unit/production/my-deployment")
	assert.Contains(t, yamlStr, "targetRevision: latest")
}

func TestTransformToArgoCDOCIApplication_DefaultParams(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("http://localhost:9090")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{},
		UnitSlug:      "my-app",
		SpaceSlug:     "production",
		SpaceID:       spaceID,
		RevisionNum:   1,
		Data:          testConfigMapYAML,
	}

	worker := &ArgoCDOCIWorker{}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)
}

func TestArgoCDOCIWorker_Info(t *testing.T) {
	worker := &ArgoCDOCIWorker{}
	options := api.InfoOptions{
		WorkerSlug: "test-worker",
	}

	// Note: This will fail without a Kubernetes context, but we're testing that
	// the method returns the correct provider type
	info := worker.Info(options)

	// The Info method should return BridgeWorkerInfo with ArgoCD OCI configuration
	// In a real test environment with a kubeconfig, this would contain targets
	assert.NotNil(t, info.SupportedConfigTypes)
}

func TestArgoCDOCIWorker_Apply_TransformError(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	kubernetes.SetupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyFailed, "cannot infer OCI host")

	worker := &ArgoCDOCIWorker{}
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		Data:         testConfigMapYAML,
	}

	err := worker.Apply(mockCtx, payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot infer OCI host")
	mockCtx.AssertCalled(t, "SendStatus", mock.Anything)
}

func TestArgoCDOCIWorker_Apply_Success(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockApplier := new(kubernetes.MockK8sApplier)

	// Setup mock expectations for the parent's Apply method
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Starting to apply resources...")
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Applying resources...")
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusProgressing &&
			status.Result == api.ActionResultApplySynced &&
			status.Message == "Resources applied successfully, waiting for ready state"
	})).Return(nil).Once()

	// Mock the applier factory
	originalFactory := kubernetes.K8sApplierFactory
	kubernetes.K8sApplierFactory = func(name kubernetes.ApplierName, config kubernetes.ApplierConfig) (kubernetes.K8sApplier, error) {
		return mockApplier, nil
	}
	defer func() { kubernetes.K8sApplierFactory = originalFactory }()

	// Create an ArgoCD Application object to return from Apply
	argoCDApp := &unstructured.Unstructured{}
	argoCDApp.SetAPIVersion(argoCDAPIVersion)
	argoCDApp.SetKind(argoCDKindApplication)
	argoCDApp.SetName("my-app-abc123")
	argoCDApp.SetNamespace("argocd")

	mockApplier.On("Apply", mock.Anything, mock.MatchedBy(func(objects []*unstructured.Unstructured) bool {
		// Verify the transformed payload contains ArgoCD Application
		if len(objects) != 1 {
			return false
		}
		return objects[0].GetKind() == argoCDKindApplication && objects[0].GetAPIVersion() == argoCDAPIVersion
	})).Return(kubernetes.ApplyResult{
		ResourceSet: &kubernetes.SimpleResourceSet{
			Entries: []kubernetes.SimpleResourceSetEntry{
				{
					Name:      "my-app-abc123",
					Namespace: "argocd",
					Kind:      argoCDKindApplication,
					Action:    "configured",
				},
			},
		},
		LiveObjects: []*unstructured.Unstructured{argoCDApp},
		Error:       nil,
	})

	worker := &ArgoCDOCIWorker{}
	worker.SetApplierType(kubernetes.CLIUtilsSSA)

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.Apply(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 3)
	mockApplier.AssertNumberOfCalls(t, "Apply", 1)
}

func TestArgoCDOCIWorker_Import_NotSupported(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	kubernetes.SetupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultImportFailed, "Import not supported")

	worker := &ArgoCDOCIWorker{}
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
	}

	err := worker.Import(mockCtx, payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Import not supported")
	mockCtx.AssertCalled(t, "SendStatus", mock.Anything)
}

func TestArgoCDOCIWorker_Destroy_TransformError(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	kubernetes.SetupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultDestroyFailed, "cannot infer OCI host")

	worker := &ArgoCDOCIWorker{}
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		Data:         testConfigMapYAML,
	}

	err := worker.Destroy(mockCtx, payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot infer OCI host")
	mockCtx.AssertCalled(t, "SendStatus", mock.Anything)
}

func TestArgoCDOCIWorker_WatchForApply_TransformError(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	kubernetes.SetupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyWaitFailed, "cannot infer OCI host")

	worker := &ArgoCDOCIWorker{}
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot infer OCI host")
	mockCtx.AssertCalled(t, "SendStatus", mock.Anything)
}

func TestArgoCDOCIWorker_WatchForDestroy_TransformError(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	kubernetes.SetupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultDestroyWaitFailed, "cannot infer OCI host")

	worker := &ArgoCDOCIWorker{}
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForDestroy(mockCtx, payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot infer OCI host")
	mockCtx.AssertCalled(t, "SendStatus", mock.Anything)
}

func TestArgoCDOCIWorker_Refresh_NoDrift(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	// 1. Progress status
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Retrieving ArgoCD Application state...")
	// 2. Completed with no drift
	var capturedResult *api.ActionResult
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted &&
			status.Result == api.ActionResultRefreshAndNoDrift
	})).Return(nil).Run(func(args mock.Arguments) {
		capturedResult = args.Get(0).(*api.ActionResult)
	}).Once()

	// Mock K8s client Get - return a Synced+Healthy Application with ConfigMap resource
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "argocd"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion(argoCDAPIVersion)
		obj.SetKind(argoCDKindApplication)
		obj.SetName("test-space-my-app")
		obj.SetNamespace("argocd")
		obj.Object["status"] = map[string]interface{}{
			"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
			"sync":   map[string]interface{}{"status": argoCDSyncStatusSynced},
			"resources": []interface{}{
				map[string]interface{}{
					"group": "", "version": "v1", "kind": "ConfigMap",
					"name": "test-configmap", "namespace": "default",
					"status": argoCDSyncStatusSynced,
					"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
				},
			},
		}
	}).Once()

	// Mock managed resource Get - return matching ConfigMap (same content as original)
	mockClient.On("Get",
		mock.Anything,
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace != "argocd"
		}),
		mock.Anything,
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion("v1")
		obj.SetKind("ConfigMap")
		obj.SetName("test-configmap")
		obj.SetNamespace("default")
		obj.Object["data"] = map[string]interface{}{
			"key": "value",
		}
	}).Once()

	worker := &ArgoCDOCIWorker{}
	worker.SetApplierType(kubernetes.CLIUtilsSSA)

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.Refresh(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)

	// Verify result — Synced + matching managed resources = no drift, result.Data stays nil
	assert.NotNil(t, capturedResult)
	assert.Contains(t, capturedResult.Message, "no drift")
	assert.NotEmpty(t, capturedResult.LiveData)
	assert.NotEmpty(t, capturedResult.LiveState)
	assert.NotNil(t, capturedResult.ResourceStatuses)
	assert.Nil(t, capturedResult.Data)
}

func TestArgoCDOCIWorker_Refresh_Drifted(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	// 1. Progress status
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Retrieving ArgoCD Application state...")
	// 2. Completed with drift detected
	var capturedResult *api.ActionResult
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted &&
			status.Result == api.ActionResultRefreshAndDrifted
	})).Return(nil).Run(func(args mock.Arguments) {
		capturedResult = args.Get(0).(*api.ActionResult)
	}).Once()

	// Mock K8s client Get - return an OutOfSync Application
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "argocd"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion(argoCDAPIVersion)
		obj.SetKind(argoCDKindApplication)
		obj.SetName("test-space-my-app")
		obj.SetNamespace("argocd")
		obj.Object["status"] = map[string]interface{}{
			"health": map[string]interface{}{"status": argoCDHealthStatusDegraded},
			"sync":   map[string]interface{}{"status": argoCDSyncStatusOutOfSync},
			"resources": []interface{}{
				map[string]interface{}{
					"group": "apps", "version": "v1", "kind": "Deployment",
					"name": "my-deploy", "namespace": "default",
					"status": argoCDSyncStatusOutOfSync,
					"health": map[string]interface{}{"status": argoCDHealthStatusDegraded},
				},
			},
		}
	}).Once()

	// Mock managed resource Get - return not found (skips diff-patch)
	mockClient.On("Get",
		mock.Anything,
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace != "argocd"
		}),
		mock.Anything,
		mock.Anything,
	).Return(&mockNotFoundError{}).Maybe()

	worker := &ArgoCDOCIWorker{}
	worker.SetApplierType(kubernetes.CLIUtilsSSA)

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.Refresh(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)

	// Verify result — OutOfSync means drift; managed resources NotFound means LiveState/LiveData/Data stay nil
	assert.NotNil(t, capturedResult)
	assert.Contains(t, capturedResult.Message, "drift detected")
	assert.Empty(t, capturedResult.LiveData, "LiveData should be nil when managed resources are not found")
	assert.Empty(t, capturedResult.LiveState, "LiveState should be nil when managed resources are not found")
	assert.NotNil(t, capturedResult.ResourceStatuses)
	assert.Nil(t, capturedResult.Data, "result.Data should be nil when managed resources are not found")
}

func TestArgoCDOCIWorker_Refresh_NotFound(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	// 1. Progress status
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Retrieving ArgoCD Application state...")
	// 2. Completed with drift (not found = drift)
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted &&
			status.Result == api.ActionResultRefreshAndDrifted &&
			strings.Contains(status.Message, "not found")
	})).Return(nil).Once()

	// Mock K8s client Get - return NotFound error
	notFoundErr := &mockNotFoundError{}
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool { return true }),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(notFoundErr).Once()

	worker := &ArgoCDOCIWorker{}

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.Refresh(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
}

func TestArgoCDOCIWorker_Refresh_TransformError(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	kubernetes.SetupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultRefreshFailed, "cannot infer OCI host")

	worker := &ArgoCDOCIWorker{}
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		Data:         testConfigMapYAML,
	}

	err := worker.Refresh(mockCtx, payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot infer OCI host")
	mockCtx.AssertCalled(t, "SendStatus", mock.Anything)
}

func TestArgoCDOCIWorker_Refresh_WithInventory(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	// 1. Progress status
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Retrieving ArgoCD Application state...")
	// 2. Completed - capture LiveData for inventory verification
	var capturedLiveData []byte
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted
	})).Return(nil).Run(func(args mock.Arguments) {
		status := args.Get(0).(*api.ActionResult)
		capturedLiveData = status.LiveData
	}).Once()

	// Mock K8s client Get for Application CR — with managed ConfigMap in .status.resources[]
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "argocd"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion(argoCDAPIVersion)
		obj.SetKind(argoCDKindApplication)
		obj.SetName("test-space-my-app")
		obj.SetNamespace("argocd")
		obj.Object["status"] = map[string]interface{}{
			"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
			"sync":   map[string]interface{}{"status": argoCDSyncStatusSynced},
			"resources": []interface{}{
				map[string]interface{}{
					"group": "", "version": "v1", "kind": "ConfigMap",
					"name": "test-configmap", "namespace": "default",
					"status": argoCDSyncStatusSynced,
					"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
				},
			},
		}
	}).Once()

	// Mock managed resource Get - return matching ConfigMap
	mockClient.On("Get",
		mock.Anything,
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace != "argocd"
		}),
		mock.Anything,
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion("v1")
		obj.SetKind("ConfigMap")
		obj.SetName("test-configmap")
		obj.SetNamespace("default")
		obj.Object["data"] = map[string]interface{}{
			"key": "value",
		}
	}).Once()

	worker := &ArgoCDOCIWorker{}
	worker.SetApplierType(kubernetes.CLIUtilsSSA)

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.Refresh(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)

	// Verify LiveData contains managed resources (ConfigMap), not the Application CR
	assert.NotEmpty(t, capturedLiveData, "LiveData should not be empty")
	liveDataStr := string(capturedLiveData)
	assert.Contains(t, liveDataStr, "kind: ConfigMap", "LiveData should contain managed ConfigMap")
	assert.NotContains(t, liveDataStr, "kind: Application", "LiveData should not contain Application CR")
}

func TestArgoCDOCIBridgeOptions_JSONMarshaling(t *testing.T) {
	options := ArgoCDOCIBridgeOptions{
		KubeContext:          "test-context",
		ArgoCDNamespace:      "argocd",
		DestinationServer:    "https://kubernetes.default.svc",
		DestinationNamespace: "production",
		Project:              "my-project",
		SyncPolicy:           "automated",
		PruneEnabled:         true,
		SelfHealEnabled:      true,
		OCIRepoURL:           "oci://ghcr.io/myorg/manifests",
		OCIPath:              "apps/myapp",
		TargetRevision:       "v1.0.0",
	}

	// Test marshaling
	jsonBytes, err := json.Marshal(options)
	assert.NoError(t, err)
	assert.NotEmpty(t, jsonBytes)

	// Test unmarshaling
	var unmarshaled ArgoCDOCIBridgeOptions
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	assert.NoError(t, err)
	assert.Equal(t, options, unmarshaled)
}

func TestArgoCDOCIBridgeOptions_OmitEmpty(t *testing.T) {
	options := ArgoCDOCIBridgeOptions{
		OCIRepoURL: "oci://ghcr.io/myorg/manifests",
	}

	jsonBytes, err := json.Marshal(options)
	assert.NoError(t, err)

	jsonStr := string(jsonBytes)
	// Check that empty fields are omitted
	assert.NotContains(t, jsonStr, "KubeContext")
	assert.NotContains(t, jsonStr, "ArgoCDNamespace")
	// OCIRepoURL should be present
	assert.Contains(t, jsonStr, "OCIRepoURL")
}

func TestMapArgoCDSyncStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected api.ResourceSyncStatusType
	}{
		{argoCDSyncStatusSynced, api.ResourceSyncStatusSynced},
		{argoCDSyncStatusOutOfSync, api.ResourceSyncStatusPending},
		{argoCDSyncStatusUnknown, api.ResourceSyncStatusPending},
		{"", api.ResourceSyncStatusPending},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, mapArgoCDSyncStatus(tt.input))
		})
	}
}

func TestMapArgoCDHealthStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected api.ResourceReadinessType
	}{
		{argoCDHealthStatusHealthy, api.ResourceReadinessReady},
		{argoCDHealthStatusProgressing, api.ResourceReadinessInProgress},
		{argoCDHealthStatusDegraded, api.ResourceReadinessFailed},
		{argoCDHealthStatusSuspended, api.ResourceReadinessInProgress},
		{argoCDHealthStatusMissing, api.ResourceReadinessFailed},
		{argoCDHealthStatusUnknown, api.ResourceReadinessUnknown},
		{"", api.ResourceReadinessUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, mapArgoCDHealthStatus(tt.input))
		})
	}
}

func TestGetArgoCDAppHealthStatus(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{
			"status": map[string]interface{}{
				"health": map[string]interface{}{
					"status": argoCDHealthStatusHealthy,
				},
			},
		}}
		assert.Equal(t, argoCDHealthStatusHealthy, getArgoCDAppHealthStatus(app))
	})

	t.Run("missing status", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{}}
		assert.Equal(t, argoCDHealthStatusUnknown, getArgoCDAppHealthStatus(app))
	})
}

func TestGetArgoCDAppSyncStatus(t *testing.T) {
	t.Run("synced", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{
			"status": map[string]interface{}{
				"sync": map[string]interface{}{
					"status": argoCDSyncStatusSynced,
				},
			},
		}}
		assert.Equal(t, argoCDSyncStatusSynced, getArgoCDAppSyncStatus(app))
	})

	t.Run("missing status", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{}}
		assert.Equal(t, argoCDSyncStatusUnknown, getArgoCDAppSyncStatus(app))
	})
}

func TestGetArgoCDOperationPhase(t *testing.T) {
	t.Run("succeeded", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{
			"status": map[string]interface{}{
				"operationState": map[string]interface{}{
					"phase": argoCDOperationPhaseSucceeded,
				},
			},
		}}
		assert.Equal(t, argoCDOperationPhaseSucceeded, getArgoCDOperationPhase(app))
	})

	t.Run("missing status", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{}}
		assert.Equal(t, "", getArgoCDOperationPhase(app))
	})
}

func TestBuildArgoCDResourceStatusMap(t *testing.T) {
	t.Run("with resources", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{
			"status": map[string]interface{}{
				"resources": []interface{}{
					map[string]interface{}{
						"group":     "apps",
						"version":   "v1",
						"kind":      "Deployment",
						"name":      "my-deploy",
						"namespace": "default",
						"status":    argoCDSyncStatusSynced,
						"health": map[string]interface{}{
							"status": argoCDHealthStatusHealthy,
						},
					},
					map[string]interface{}{
						"group":     "",
						"version":   "v1",
						"kind":      "Service",
						"name":      "my-svc",
						"namespace": "default",
						"status":    argoCDSyncStatusOutOfSync,
						"health": map[string]interface{}{
							"status": argoCDHealthStatusProgressing,
						},
					},
				},
			},
		}}

		statusMap := buildArgoCDResourceStatusMap(app)
		assert.Len(t, statusMap, 2)

		deployKey := funcApi.ResourceTypeAndName("apps/v1/Deployment#default/my-deploy")
		assert.Equal(t, api.ResourceSyncStatusSynced, statusMap[deployKey].SyncStatus)
		assert.Equal(t, api.ResourceReadinessReady, statusMap[deployKey].Readiness)

		svcKey := funcApi.ResourceTypeAndName("v1/Service#default/my-svc")
		assert.Equal(t, api.ResourceSyncStatusPending, statusMap[svcKey].SyncStatus)
		assert.Equal(t, api.ResourceReadinessInProgress, statusMap[svcKey].Readiness)
	})

	t.Run("no resources", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{
			"status": map[string]interface{}{},
		}}
		statusMap := buildArgoCDResourceStatusMap(app)
		assert.Nil(t, statusMap)
	})

	t.Run("no status", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{}}
		statusMap := buildArgoCDResourceStatusMap(app)
		assert.Nil(t, statusMap)
	})
}

func TestExtractResourceObjects(t *testing.T) {
	t.Run("extracts resources with GVK and namespace/name", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{
			"status": map[string]interface{}{
				"resources": []interface{}{
					map[string]interface{}{
						"group": "apps", "version": "v1", "kind": "Deployment",
						"name": "my-deploy", "namespace": "production",
					},
					map[string]interface{}{
						"group": "", "version": "v1", "kind": "ConfigMap",
						"name": "my-config", "namespace": "production",
					},
				},
			},
		}}

		objects := extractResourceObjects(app)
		assert.Len(t, objects, 2)

		assert.Equal(t, "Deployment", objects[0].GetKind())
		assert.Equal(t, "apps/v1", objects[0].GetAPIVersion())
		assert.Equal(t, "my-deploy", objects[0].GetName())
		assert.Equal(t, "production", objects[0].GetNamespace())

		assert.Equal(t, "ConfigMap", objects[1].GetKind())
		assert.Equal(t, "v1", objects[1].GetAPIVersion())
		assert.Equal(t, "my-config", objects[1].GetName())
		assert.Equal(t, "production", objects[1].GetNamespace())
	})

	t.Run("skips entries with empty kind", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{
			"status": map[string]interface{}{
				"resources": []interface{}{
					map[string]interface{}{
						"group": "", "version": "v1", "kind": "",
						"name": "bad-entry", "namespace": "default",
					},
					map[string]interface{}{
						"group": "", "version": "v1", "kind": "ConfigMap",
						"name": "good-entry", "namespace": "default",
					},
				},
			},
		}}

		objects := extractResourceObjects(app)
		assert.Len(t, objects, 1)
		assert.Equal(t, "good-entry", objects[0].GetName())
	})

	t.Run("skips entries with empty name", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{
			"status": map[string]interface{}{
				"resources": []interface{}{
					map[string]interface{}{
						"group": "", "version": "v1", "kind": "ConfigMap",
						"name": "", "namespace": "default",
					},
				},
			},
		}}

		objects := extractResourceObjects(app)
		assert.Len(t, objects, 0)
	})

	t.Run("skips entries with empty version", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{
			"status": map[string]interface{}{
				"resources": []interface{}{
					map[string]interface{}{
						"group": "", "version": "", "kind": "ConfigMap",
						"name": "my-config", "namespace": "default",
					},
				},
			},
		}}

		objects := extractResourceObjects(app)
		assert.Len(t, objects, 0)
	})

	t.Run("returns nil for no resources", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{
			"status": map[string]interface{}{},
		}}
		objects := extractResourceObjects(app)
		assert.Nil(t, objects)
	})

	t.Run("returns nil for no status", func(t *testing.T) {
		app := &unstructured.Unstructured{Object: map[string]interface{}{}}
		objects := extractResourceObjects(app)
		assert.Nil(t, objects)
	})
}

func TestArgoCDOCIWorker_Refresh_DriftedWithManagedResourceData(t *testing.T) {
	// When ArgoCD reports OutOfSync and .status.resources[] entries exist,
	// managed resource Get returns live objects → diff-patch detects content drift.
	// Use a ConfigMap as managed resource so diff-patch can compare against originalData.
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Retrieving ArgoCD Application state...")
	var capturedResult *api.ActionResult
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted &&
			status.Result == api.ActionResultRefreshAndDrifted
	})).Return(nil).Run(func(args mock.Arguments) {
		capturedResult = args.Get(0).(*api.ActionResult)
	}).Once()

	// Mock K8s client Get for Application CR — OutOfSync with .status.resources[]
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "argocd"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion(argoCDAPIVersion)
		obj.SetKind(argoCDKindApplication)
		obj.SetName("test-space-my-app")
		obj.SetNamespace("argocd")
		obj.Object["status"] = map[string]interface{}{
			"health": map[string]interface{}{"status": argoCDHealthStatusDegraded},
			"sync":   map[string]interface{}{"status": argoCDSyncStatusOutOfSync},
			"resources": []interface{}{
				map[string]interface{}{
					"group": "", "version": "v1", "kind": "ConfigMap",
					"name": "test-configmap", "namespace": "default",
					"status": argoCDSyncStatusOutOfSync,
					"health": map[string]interface{}{"status": argoCDHealthStatusDegraded},
				},
			},
		}
	}).Once()

	// Mock managed resource Get — return modified ConfigMap
	mockClient.On("Get",
		mock.Anything,
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace != "argocd"
		}),
		mock.Anything,
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion("v1")
		obj.SetKind("ConfigMap")
		obj.SetName("test-configmap")
		obj.SetNamespace("default")
		obj.Object["data"] = map[string]interface{}{
			"key": "drifted-value",
		}
	}).Once()

	worker := &ArgoCDOCIWorker{}
	worker.SetApplierType(kubernetes.CLIUtilsSSA)

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.Refresh(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)

	// Verify drift detected — both sync and content drift
	assert.NotNil(t, capturedResult)
	assert.Contains(t, capturedResult.Message, "drift detected")
	assert.NotNil(t, capturedResult.Data, "result.Data should contain patched content when content drift detected")
	dataStr := string(capturedResult.Data)
	assert.Contains(t, dataStr, "drifted-value", "patched data should contain the drifted value")
}

func TestArgoCDOCIWorker_WatchForApply_Success(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	// 1. Initial waiting status
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for ArgoCD Application to sync and become healthy...")
	// 2. Progress status with resource info
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusProgressing && status.ResourceStatuses != nil
	})).Return(nil).Once()
	// 3. Completed status - capture LiveData for inventory verification
	var capturedLiveData []byte
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted &&
			status.Result == api.ActionResultApplyCompleted &&
			status.ResourceStatuses != nil
	})).Return(nil).Run(func(args mock.Arguments) {
		status := args.Get(0).(*api.ActionResult)
		capturedLiveData = status.LiveData
	}).Once()

	// Mock K8s client Get - return a synced+healthy Application
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "argocd"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion(argoCDAPIVersion)
		obj.SetKind(argoCDKindApplication)
		obj.SetName("test-app")
		obj.SetNamespace("argocd")
		obj.Object["status"] = map[string]interface{}{
			"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
			"sync":   map[string]interface{}{"status": argoCDSyncStatusSynced},
			"operationState": map[string]interface{}{
				"phase": argoCDOperationPhaseSucceeded,
			},
			"resources": []interface{}{
				map[string]interface{}{
					"group": "apps", "version": "v1", "kind": "Deployment",
					"name": "my-deploy", "namespace": "default",
					"status": argoCDSyncStatusSynced,
					"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
				},
			},
		}
	}).Once()

	// Mock K8s client Get - return a live managed Deployment
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "default" && key.Name == "my-deploy"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion("apps/v1")
		obj.SetKind("Deployment")
		obj.SetName("my-deploy")
		obj.SetNamespace("default")
		obj.Object["spec"] = map[string]interface{}{
			"replicas": int64(1),
		}
		obj.Object["status"] = map[string]interface{}{
			"readyReplicas":     int64(1),
			"availableReplicas": int64(1),
		}
	}).Once()

	worker := &ArgoCDOCIWorker{}
	worker.SetApplierType(kubernetes.CLIUtilsSSA)

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 3)

	// Verify LiveData contains managed resources (Deployment), not the Application CR
	assert.NotEmpty(t, capturedLiveData, "LiveData should not be empty")
	liveDataStr := string(capturedLiveData)
	assert.Contains(t, liveDataStr, "kind: Deployment", "LiveData should contain managed Deployment")
	assert.NotContains(t, liveDataStr, "kind: Application", "LiveData should not contain Application CR")
}

func TestArgoCDOCIWorker_WatchForApply_OperationFailed(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for ArgoCD Application to sync and become healthy...")
	// Progress status
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusProgressing && strings.Contains(status.Message, "ArgoCD Application")
	})).Return(nil).Once()
	// Failed status
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusFailed &&
			status.Result == api.ActionResultApplyWaitFailed &&
			strings.Contains(status.Message, "sync operation failed")
	})).Return(nil).Once()

	// Mock K8s client Get - return a failed Application
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool { return true }),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion(argoCDAPIVersion)
		obj.SetKind(argoCDKindApplication)
		obj.Object["status"] = map[string]interface{}{
			"health": map[string]interface{}{"status": argoCDHealthStatusDegraded},
			"sync":   map[string]interface{}{"status": argoCDSyncStatusOutOfSync},
			"operationState": map[string]interface{}{
				"phase": argoCDOperationPhaseFailed,
			},
		}
	}).Once()

	worker := &ArgoCDOCIWorker{}
	worker.SetApplierType(kubernetes.CLIUtilsSSA)

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sync operation Failed")
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 3)
}

func TestArgoCDOCIWorker_Refresh_UnknownStatus(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	// 1. Progress status
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Retrieving ArgoCD Application state...")
	// 2. Unknown sync status should report drift (Bug 5 fix)
	var capturedResult *api.ActionResult
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted &&
			status.Result == api.ActionResultRefreshAndDrifted
	})).Return(nil).Run(func(args mock.Arguments) {
		capturedResult = args.Get(0).(*api.ActionResult)
	}).Once()

	// Mock K8s client Get - return Application with Unknown sync status, no .status.resources[]
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "argocd"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion(argoCDAPIVersion)
		obj.SetKind(argoCDKindApplication)
		obj.SetName("test-app")
		obj.SetNamespace("argocd")
		// No status fields set - getArgoCDAppSyncStatus returns argoCDSyncStatusUnknown
		obj.Object["status"] = map[string]interface{}{}
	}).Once()

	worker := &ArgoCDOCIWorker{}
	worker.SetApplierType(kubernetes.CLIUtilsSSA)

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.Refresh(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)

	// Verify Unknown sync status is treated as drift
	assert.NotNil(t, capturedResult)
	assert.Contains(t, capturedResult.Message, "drift detected")
	assert.Contains(t, capturedResult.Message, "sync=Unknown")
}

func TestArgoCDOCIWorker_WatchForApply_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mockCtx := new(kubernetes.MockBridgeWorkerContext)
	mockCtx.On("Context").Return(ctx)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for ArgoCD Application to sync and become healthy...")

	// Cancel the context before the poll loop starts
	cancel()

	worker := &ArgoCDOCIWorker{}

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)
	assert.NoError(t, err) // Context cancellation returns nil
}

func TestNewArgoCDOCIWorker_ApplierTypeInitialized(t *testing.T) {
	worker := NewArgoCDOCIWorker("test-id", "test-secret")
	assert.Equal(t, kubernetes.CLIUtilsSSA, worker.GetApplierType(), "NewArgoCDOCIWorker should initialize applierType via NewKubernetesBridgeWorker")
	assert.Equal(t, "test-id", worker.workerID)
	assert.Equal(t, "test-secret", worker.workerSecret)
}

func TestArgoCDOCIWorker_WatchForApply_CompletionSendStatusError(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	// 1. Initial waiting status
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for ArgoCD Application to sync and become healthy...")
	// 2. Progress status
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusProgressing && status.ResourceStatuses != nil
	})).Return(nil).Once()
	// 3. Completion SendStatus returns an error
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted &&
			status.Result == api.ActionResultApplyCompleted
	})).Return(fmt.Errorf("send status failed")).Once()

	// Mock K8s client Get - return a synced+healthy Application
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "argocd"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion(argoCDAPIVersion)
		obj.SetKind(argoCDKindApplication)
		obj.SetName("test-app")
		obj.SetNamespace("argocd")
		obj.Object["status"] = map[string]interface{}{
			"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
			"sync":   map[string]interface{}{"status": argoCDSyncStatusSynced},
			"operationState": map[string]interface{}{
				"phase": argoCDOperationPhaseSucceeded,
			},
			"resources": []interface{}{
				map[string]interface{}{
					"group": "apps", "version": "v1", "kind": "Deployment",
					"name": "my-deploy", "namespace": "default",
					"status": argoCDSyncStatusSynced,
					"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
				},
			},
		}
	}).Once()

	// Mock K8s client Get - return a live managed Deployment
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "default" && key.Name == "my-deploy"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion("apps/v1")
		obj.SetKind("Deployment")
		obj.SetName("my-deploy")
		obj.SetNamespace("default")
		obj.Object["spec"] = map[string]interface{}{"replicas": int64(1)}
		obj.Object["status"] = map[string]interface{}{"readyReplicas": int64(1)}
	}).Once()

	worker := NewArgoCDOCIWorker("", "")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)
	// Should return nil even when completion SendStatus fails
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 3)
}

func TestArgoCDOCIWorker_WatchForApply_ContextCanceledDuringCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mockCtx := new(kubernetes.MockBridgeWorkerContext)
	mockCtx.On("Context").Return(ctx)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	// 1. Initial waiting status
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for ArgoCD Application to sync and become healthy...")
	// 2. Progress status - cancel context when this is sent (simulating cancel during poll)
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusProgressing && status.ResourceStatuses != nil
	})).Return(nil).Run(func(args mock.Arguments) {
		cancel() // Cancel context after progress but before completion
	}).Once()

	// Mock K8s client Get - return a synced+healthy Application
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "argocd"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion(argoCDAPIVersion)
		obj.SetKind(argoCDKindApplication)
		obj.SetName("test-app")
		obj.SetNamespace("argocd")
		obj.Object["status"] = map[string]interface{}{
			"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
			"sync":   map[string]interface{}{"status": argoCDSyncStatusSynced},
			"operationState": map[string]interface{}{
				"phase": argoCDOperationPhaseSucceeded,
			},
			"resources": []interface{}{
				map[string]interface{}{
					"group": "apps", "version": "v1", "kind": "Deployment",
					"name": "my-deploy", "namespace": "default",
					"status": argoCDSyncStatusSynced,
					"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
				},
			},
		}
	}).Once()

	// Mock K8s client Get - return a live managed Deployment
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "default" && key.Name == "my-deploy"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion("apps/v1")
		obj.SetKind("Deployment")
		obj.SetName("my-deploy")
		obj.SetNamespace("default")
		obj.Object["spec"] = map[string]interface{}{"replicas": int64(1)}
		obj.Object["status"] = map[string]interface{}{"readyReplicas": int64(1)}
	}).Maybe()

	worker := NewArgoCDOCIWorker("", "")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)
	// Should return nil without sending completion when context is cancelled
	assert.NoError(t, err)
	// Should only have 2 SendStatus calls (initial progress + resource progress), no completion
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
}

func TestGenerateArgoCDRepoCreds_HTTP(t *testing.T) {
	yamlBytes, err := generateArgoCDRepoCreds("localhost:9092", "argocd", "worker-123", "secret-456", true)
	assert.NoError(t, err)
	assert.NotEmpty(t, yamlBytes)

	yamlStr := string(yamlBytes)
	assert.Contains(t, yamlStr, "kind: Secret")
	assert.Contains(t, yamlStr, "name: confighub-oci-creds-localhost-9092")
	assert.Contains(t, yamlStr, "namespace: argocd")
	assert.Contains(t, yamlStr, "argocd.argoproj.io/secret-type: repo-creds")
	assert.Contains(t, yamlStr, k8skit.LabelManagedBy+": argocd-oci-bridge")
	assert.Contains(t, yamlStr, "type: oci")
	assert.Contains(t, yamlStr, "url: oci://localhost:9092")
	assert.Contains(t, yamlStr, "username: worker-123")
	assert.Contains(t, yamlStr, "password: secret-456")
	assert.Contains(t, yamlStr, "insecureOCIForceHttp: \"true\"")
	assert.Contains(t, yamlStr, "forceHttpBasicAuth: \"true\"")
}

func TestGenerateArgoCDRepoCreds_HTTPS(t *testing.T) {
	yamlBytes, err := generateArgoCDRepoCreds("oci.hub.confighub.com", "argocd", "worker-123", "secret-456", false)
	assert.NoError(t, err)
	assert.NotEmpty(t, yamlBytes)

	yamlStr := string(yamlBytes)
	assert.Contains(t, yamlStr, "kind: Secret")
	assert.Contains(t, yamlStr, "url: oci://oci.hub.confighub.com")
	assert.Contains(t, yamlStr, "username: worker-123")
	assert.Contains(t, yamlStr, "password: secret-456")
	// Should NOT contain HTTP-only fields
	assert.NotContains(t, yamlStr, "insecureOCIForceHttp")
	assert.NotContains(t, yamlStr, "forceHttpBasicAuth")
}

func TestProbeOCIProtocol_UnreachableHost(t *testing.T) {
	// An unreachable host should be detected as HTTP (connection fails → HTTP fallback)
	isHTTP := probeOCIProtocol("127.0.0.1:1") // port 1 is almost certainly unreachable
	assert.True(t, isHTTP, "unreachable host should be detected as HTTP")
}

func TestTransformToArgoCDOCIApplication_WithCredentials_MultiDoc(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"OCIHost": "127.0.0.1:1", "DisableRepoCreds": "false"},
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	worker := &ArgoCDOCIWorker{
		workerID:     "test-worker-id",
		workerSecret: "test-worker-secret",
	}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)

	yamlStr := string(payload.Data)
	// Should contain both Secret and Application
	assert.Contains(t, yamlStr, "kind: Secret")
	assert.Contains(t, yamlStr, "kind: Application")
	// Secret should come first (before the --- separator and Application)
	secretIdx := strings.Index(yamlStr, "kind: Secret")
	appIdx := strings.Index(yamlStr, "kind: Application")
	assert.True(t, secretIdx < appIdx, "Secret should appear before Application in multi-doc YAML")
	// Should contain the separator
	assert.Contains(t, yamlStr, "---")
	// Secret should have repo-creds labels
	assert.Contains(t, yamlStr, "argocd.argoproj.io/secret-type: repo-creds")
	assert.Contains(t, yamlStr, "username: test-worker-id")
	assert.Contains(t, yamlStr, "password: test-worker-secret")
}

func TestTransformToArgoCDOCIApplication_DisableRepoCreds(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"OCIHost": "127.0.0.1:1", "DisableRepoCreds": "true"},
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	worker := &ArgoCDOCIWorker{
		workerID:     "test-worker-id",
		workerSecret: "test-worker-secret",
	}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)

	yamlStr := string(payload.Data)
	// Should NOT contain Secret
	assert.NotContains(t, yamlStr, "kind: Secret")
	// Should still contain Application
	assert.Contains(t, yamlStr, "kind: Application")
}

func TestTransformToArgoCDOCIApplication_NoCredentials_NoSecret(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"OCIHost": "127.0.0.1:1"},
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	// Worker without credentials
	worker := &ArgoCDOCIWorker{}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)

	yamlStr := string(payload.Data)
	// Should NOT contain Secret (no credentials)
	assert.NotContains(t, yamlStr, "kind: Secret")
	// Should contain Application
	assert.Contains(t, yamlStr, "kind: Application")
}

func TestFindApplicationObject(t *testing.T) {
	secret := &unstructured.Unstructured{}
	secret.SetKind(k8sKindSecret)
	secret.SetName("my-secret")

	app := &unstructured.Unstructured{}
	app.SetKind(argoCDKindApplication)
	app.SetName("my-app")

	t.Run("finds application among multiple objects", func(t *testing.T) {
		result := findApplicationObject([]*unstructured.Unstructured{secret, app})
		assert.NotNil(t, result)
		assert.Equal(t, "my-app", result.GetName())
	})

	t.Run("returns nil when no application", func(t *testing.T) {
		result := findApplicationObject([]*unstructured.Unstructured{secret})
		assert.Nil(t, result)
	})

	t.Run("returns nil for empty list", func(t *testing.T) {
		result := findApplicationObject([]*unstructured.Unstructured{})
		assert.Nil(t, result)
	})
}

func TestFilterOutSecrets(t *testing.T) {
	multiDocYAML := []byte(`apiVersion: v1
kind: Secret
metadata:
  name: my-secret
  namespace: argocd
type: Opaque
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
  namespace: argocd
`)
	result := filterOutSecrets(multiDocYAML)
	resultStr := string(result)
	assert.NotContains(t, resultStr, "kind: Secret")
	assert.Contains(t, resultStr, "kind: Application")
}

func TestGenerateArgoCDApplication_NameNormalization(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("")

	// Test that names with invalid characters are normalized
	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "MY_APP.Test",
		SpaceSlug:    "My-Space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	worker := &ArgoCDOCIWorker{}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)

	yamlStr := string(payload.Data)
	// Name should be normalized (lowercase, valid K8s name)
	assert.Contains(t, yamlStr, "name:")

	// Extract the metadata.name field from the YAML
	// The name field in metadata is normalized, but the UnitSlug in labels is preserved as-is
	// Find the line that starts with "  name:" (indented, which is the metadata.name)
	lines := strings.Split(yamlStr, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "  name:") {
			// This is the metadata.name field - it should be normalized (lowercase)
			assert.NotContains(t, line, "MY_APP")
			assert.NotContains(t, line, ".Test")
			assert.NotContains(t, line, "My-Space")
			break
		}
	}
}

func TestArgoCDOCIWorker_WatchForApply_TransformError_IsPermanent(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockCtx.On("SendStatus", mock.Anything).Return(nil).Maybe()

	worker := &ArgoCDOCIWorker{}
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)
	assert.Error(t, err)

	// The error should be a backoff.PermanentError so it is not retried
	var permErr *backoff.PermanentError
	assert.True(t, errors.As(err, &permErr), "transform error should be wrapped in backoff.PermanentError")
}

func TestArgoCDOCIWorker_WatchForDestroy_TransformError_IsPermanent(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockCtx.On("SendStatus", mock.Anything).Return(nil).Maybe()

	worker := &ArgoCDOCIWorker{}
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForDestroy(mockCtx, payload)
	assert.Error(t, err)

	// The error should be a backoff.PermanentError so it is not retried
	var permErr *backoff.PermanentError
	assert.True(t, errors.As(err, &permErr), "transform error should be wrapped in backoff.PermanentError")
}

func TestArgoCDOCIWorker_Refresh_ContentDriftWithSyncedStatus(t *testing.T) {
	// When ArgoCD reports Synced but managed resource differs from original,
	// content drift should be detected via diff-patch. result.Data should contain
	// patched content with the modified value.
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	// 1. Progress status
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Retrieving ArgoCD Application state...")
	// 2. Completed with drift detected (content drift despite Synced)
	var capturedResult *api.ActionResult
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted &&
			status.Result == api.ActionResultRefreshAndDrifted
	})).Return(nil).Run(func(args mock.Arguments) {
		capturedResult = args.Get(0).(*api.ActionResult)
	}).Once()

	// Mock K8s client Get for Application CR - Synced+Healthy with .status.resources[]
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "argocd"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion(argoCDAPIVersion)
		obj.SetKind(argoCDKindApplication)
		obj.SetName("test-space-my-app")
		obj.SetNamespace("argocd")
		obj.Object["status"] = map[string]interface{}{
			"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
			"sync":   map[string]interface{}{"status": argoCDSyncStatusSynced},
			"resources": []interface{}{
				map[string]interface{}{
					"group": "", "version": "v1", "kind": "ConfigMap",
					"name": "test-configmap", "namespace": "default",
					"status": argoCDSyncStatusSynced,
					"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
				},
			},
		}
	}).Once()

	// Mock managed resource Get — returns modified ConfigMap (content drift)
	mockClient.On("Get",
		mock.Anything,
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace != "argocd"
		}),
		mock.Anything,
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion("v1")
		obj.SetKind("ConfigMap")
		obj.SetName("test-configmap")
		obj.SetNamespace("default")
		obj.Object["data"] = map[string]interface{}{
			"key": "modified-value",
		}
	}).Once()

	worker := &ArgoCDOCIWorker{}
	worker.SetApplierType(kubernetes.CLIUtilsSSA)

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.Refresh(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)

	// Content drift detected despite Synced; result.Data should contain patched content
	assert.NotNil(t, capturedResult)
	assert.Contains(t, capturedResult.Message, "drift detected")
	assert.NotEmpty(t, capturedResult.LiveData)
	assert.NotEmpty(t, capturedResult.LiveState)
	assert.NotNil(t, capturedResult.Data, "result.Data should contain patched content when content drift detected")
	assert.Contains(t, string(capturedResult.Data), "modified-value", "patched data should contain the modified value")
}

func TestArgoCDOCIWorker_Refresh_ManagedResourceNotFound(t *testing.T) {
	// When Application has no .status.resources[], no managed resource fetch occurs.
	// Only ArgoCD sync status determines drift.
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("").Maybe()
	mockClient := new(kubernetes.MockK8sClient)

	// Override kubernetes.KubernetesClientFactory
	originalFactory := kubernetes.KubernetesClientFactory
	kubernetes.KubernetesClientFactory = func(kubeContext string) (kubernetes.KubernetesClient, kubernetes.ResourceManager, error) {
		return mockClient, nil, nil
	}
	defer func() { kubernetes.KubernetesClientFactory = originalFactory }()

	// Setup mock expectations for SendStatus
	// 1. Progress status
	kubernetes.SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Retrieving ArgoCD Application state...")
	// 2. Completed with no drift (Synced + no .status.resources[])
	var capturedResult *api.ActionResult
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted &&
			status.Result == api.ActionResultRefreshAndNoDrift
	})).Return(nil).Run(func(args mock.Arguments) {
		capturedResult = args.Get(0).(*api.ActionResult)
	}).Once()

	// Mock K8s client Get for Application CR - Synced+Healthy, no .status.resources[]
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Namespace == "argocd"
		}),
		mock.MatchedBy(func(obj client.Object) bool { return true }),
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.SetAPIVersion(argoCDAPIVersion)
		obj.SetKind(argoCDKindApplication)
		obj.SetName("test-space-my-app")
		obj.SetNamespace("argocd")
		obj.Object["status"] = map[string]interface{}{
			"health": map[string]interface{}{"status": argoCDHealthStatusHealthy},
			"sync":   map[string]interface{}{"status": argoCDSyncStatusSynced},
		}
	}).Once()

	worker := &ArgoCDOCIWorker{}
	worker.SetApplierType(kubernetes.CLIUtilsSSA)

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	err := worker.Refresh(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)

	// Verify no drift when sync is Synced and no managed resources
	assert.NotNil(t, capturedResult)
	assert.Contains(t, capturedResult.Message, "no drift")
	assert.Nil(t, capturedResult.Data, "result.Data should be nil when no drift")
}

func TestGetLiveObjects_SkipNotFound(t *testing.T) {
	t.Run("fetches found resources", func(t *testing.T) {
		mockClient := new(kubernetes.MockK8sClient)
		mockClient.On("Get",
			mock.Anything,
			mock.MatchedBy(func(key client.ObjectKey) bool {
				return key.Name == "test-configmap"
			}),
			mock.Anything,
			mock.Anything,
		).Return(nil).Run(func(args mock.Arguments) {
			obj := args.Get(2).(*unstructured.Unstructured)
			obj.SetAPIVersion("v1")
			obj.SetKind("ConfigMap")
			obj.SetName("test-configmap")
			obj.SetNamespace("default")
			obj.Object["data"] = map[string]interface{}{"key": "value"}
		}).Once()

		expected := []*unstructured.Unstructured{testConfigMap.DeepCopy()}
		result, err := kubernetes.GetLiveObjects(context.Background(), mockClient, expected, false, true)
		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "test-configmap", result[0].GetName())
	})

	t.Run("skips not-found resources when skipNotFound=true", func(t *testing.T) {
		mockClient := new(kubernetes.MockK8sClient)
		mockClient.On("Get",
			mock.Anything,
			mock.Anything,
			mock.Anything,
			mock.Anything,
		).Return(&mockNotFoundError{}).Once()

		expected := []*unstructured.Unstructured{testConfigMap.DeepCopy()}
		result, err := kubernetes.GetLiveObjects(context.Background(), mockClient, expected, false, true)
		assert.NoError(t, err)
		assert.Len(t, result, 0)
	})

	t.Run("returns NotFound error when skipNotFound=false", func(t *testing.T) {
		mockClient := new(kubernetes.MockK8sClient)
		mockClient.On("Get",
			mock.Anything,
			mock.Anything,
			mock.Anything,
			mock.Anything,
		).Return(&mockNotFoundError{}).Once()

		expected := []*unstructured.Unstructured{testConfigMap.DeepCopy()}
		result, err := kubernetes.GetLiveObjects(context.Background(), mockClient, expected, false, false)
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("propagates permission error even when skipNotFound=true", func(t *testing.T) {
		mockClient := new(kubernetes.MockK8sClient)
		forbiddenErr := &mockForbiddenError{}
		mockClient.On("Get",
			mock.Anything,
			mock.Anything,
			mock.Anything,
			mock.Anything,
		).Return(forbiddenErr).Once()

		expected := []*unstructured.Unstructured{testConfigMap.DeepCopy()}
		result, err := kubernetes.GetLiveObjects(context.Background(), mockClient, expected, false, true)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "forbidden")
	})

	t.Run("empty expected objects", func(t *testing.T) {
		mockClient := new(kubernetes.MockK8sClient)
		result, err := kubernetes.GetLiveObjects(context.Background(), mockClient, nil, false, true)
		assert.NoError(t, err)
		assert.Nil(t, result)
	})
}

// Helm-specific Application CR tests

func TestGenerateArgoCDApplication_HelmSource(t *testing.T) {
	args := &argoCDApplicationArgs{
		Name:                 "test-helm-app",
		ArgoCDNamespace:      "argocd",
		UnitSlug:             "my-release",
		UnitID:               "unit-uuid-123",
		SpaceID:              "space-uuid-456",
		RevisionNum:          "1",
		Project:              "default",
		OCIRepoURL:           "oci://oci.hub.confighub.com/unit/production/my-release",
		OCIPath:              ".",
		TargetRevision:       "1.2.3",
		DestinationServer:    "https://kubernetes.default.svc",
		DestinationNamespace: "default",
		SyncPolicy:           "automated",
		PruneEnabled:         true,
		SelfHealEnabled:      true,
		IsHelm:               true,
		HelmReleaseName:      "my-release",
		HelmChartName:        "nginx",
	}

	yamlBytes, err := generateArgoCDApplication(args)
	assert.NoError(t, err)
	assert.NotEmpty(t, yamlBytes)

	yamlStr := string(yamlBytes)

	// Verify Helm source: chart set, no path, targetRevision is chart version, helm.releaseName set
	assert.Contains(t, yamlStr, "chart: nginx")
	assert.Contains(t, yamlStr, "repoURL: oci://oci.hub.confighub.com/unit/production/my-release")
	assert.Contains(t, yamlStr, "targetRevision: 1.2.3")
	assert.NotContains(t, yamlStr, "path:")
	assert.Contains(t, yamlStr, "releaseName: my-release")

	// Standard fields should still be present
	assert.Contains(t, yamlStr, "apiVersion: argoproj.io/v1alpha1")
	assert.Contains(t, yamlStr, "kind: Application")
	assert.Contains(t, yamlStr, "name: test-helm-app")
	assert.Contains(t, yamlStr, "prune: true")
	assert.Contains(t, yamlStr, "selfHeal: true")
}

func TestGenerateArgoCDApplication_NonHelmSourceHasPath(t *testing.T) {
	args := &argoCDApplicationArgs{
		Name:                 "test-plain-app",
		ArgoCDNamespace:      "argocd",
		UnitSlug:             "my-app",
		UnitID:               "unit-uuid-123",
		SpaceID:              "space-uuid-456",
		RevisionNum:          "1",
		Project:              "default",
		OCIRepoURL:           "oci://oci.hub.confighub.com/unit/production/my-app",
		OCIPath:              ".",
		TargetRevision:       "latest",
		DestinationServer:    "https://kubernetes.default.svc",
		DestinationNamespace: "default",
		SyncPolicy:           "manual",
		IsHelm:               false,
	}

	yamlBytes, err := generateArgoCDApplication(args)
	assert.NoError(t, err)

	yamlStr := string(yamlBytes)

	// Non-Helm source should have path
	assert.Contains(t, yamlStr, "path: .")
	assert.Contains(t, yamlStr, "targetRevision: latest")
}

func TestGenerateArgoCDApplication_HelmSource_EmptyReleaseName(t *testing.T) {
	args := &argoCDApplicationArgs{
		Name:                 "helm-app-no-release",
		ArgoCDNamespace:      "argocd",
		UnitSlug:             "my-chart",
		SpaceID:              "space-123",
		RevisionNum:          "1",
		Project:              "default",
		OCIRepoURL:           "oci://ghcr.io/myorg/charts",
		TargetRevision:       "1.0.0",
		DestinationServer:    "https://kubernetes.default.svc",
		DestinationNamespace: "default",
		SyncPolicy:           "manual",
		IsHelm:               true,
		HelmReleaseName:      "",
		HelmChartName:        "nginx",
	}

	yamlBytes, err := generateArgoCDApplication(args)
	assert.NoError(t, err)

	yamlStr := string(yamlBytes)

	// Should have chart but no releaseName or helm section when releaseName is empty
	assert.Contains(t, yamlStr, "chart: nginx")
	assert.NotContains(t, yamlStr, "releaseName:")
	assert.NotContains(t, yamlStr, "path:")
}

func TestTransformToArgoCDOCIApplication_HelmUnit(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("https://app.confighub.com")

	spaceID := uuid.New()
	unitID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-release",
		UnitID:       unitID,
		SpaceSlug:    "production",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
		UnitLabels: map[string]string{
			helmutils.HelmReleaseLabel:         "my-release",
			helmutils.HelmChartLabel:           "nginx",
			helmutils.HelmChartVersionLabel:    "1.2.3",
			helmutils.HelmChartAPIVersionLabel: "v2",
		},
	}

	worker := &ArgoCDOCIWorker{}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)

	yamlStr := string(payload.Data)

	// Should generate Helm-style source with chart name, chart version and release name
	assert.Contains(t, yamlStr, "chart: nginx")
	assert.Contains(t, yamlStr, "targetRevision: 1.2.3")
	assert.NotContains(t, yamlStr, "path:")
	assert.Contains(t, yamlStr, "releaseName: my-release")

	// Standard fields
	assert.Contains(t, yamlStr, "apiVersion: argoproj.io/v1alpha1")
	assert.Contains(t, yamlStr, "kind: Application")
}

func TestTransformToArgoCDOCIApplication_HelmUnit_InferredOCI(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("https://hub.confighub.com")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"KubeContext": "test-context"},
		UnitSlug:     "my-release",
		SpaceSlug:    "production",
		SpaceID:      spaceID,
		RevisionNum:  5,
		Data:         testConfigMapYAML,
		UnitLabels: map[string]string{
			helmutils.HelmReleaseLabel:         "my-release",
			helmutils.HelmChartLabel:           "nginx",
			helmutils.HelmChartVersionLabel:    "2.0.0",
			helmutils.HelmChartAPIVersionLabel: "v2",
		},
	}

	worker := &ArgoCDOCIWorker{}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)

	yamlStr := string(payload.Data)

	// Helm unit: targetRevision should be the chart version, not the OCI tag
	assert.Contains(t, yamlStr, "targetRevision: 2.0.0")
	assert.NotContains(t, yamlStr, "path:")
	// OCI URL should still be auto-constructed
	assert.Contains(t, yamlStr, "repoURL: oci://oci.hub.confighub.com/unit/production/my-release")
}

func TestTransformToArgoCDOCIApplication_NonHelmUnit_PreservesPath(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("https://app.confighub.com")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceSlug:    "production",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
		// No Helm labels
		UnitLabels: map[string]string{},
	}

	worker := &ArgoCDOCIWorker{}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)

	yamlStr := string(payload.Data)

	// Non-Helm: should have path and standard targetRevision
	assert.Contains(t, yamlStr, "path: apps/myapp")
	assert.Contains(t, yamlStr, "targetRevision: v1.0.0")
}

func TestTransformToArgoCDOCIApplication_PartialHelmLabels_NotHelm(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("https://app.confighub.com")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: testArgoCDOCITargetOptions,
		UnitSlug:     "my-app",
		SpaceSlug:    "production",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
		// Partial Helm labels (missing HelmChartAPIVersion)
		UnitLabels: map[string]string{
			helmutils.HelmReleaseLabel:      "my-release",
			helmutils.HelmChartLabel:        "nginx",
			helmutils.HelmChartVersionLabel: "1.0.0",
		},
	}

	worker := &ArgoCDOCIWorker{}
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, false)
	assert.NoError(t, err)

	yamlStr := string(payload.Data)

	// Partial labels should NOT trigger Helm mode
	assert.Contains(t, yamlStr, "path:")
	assert.Contains(t, yamlStr, "targetRevision: v1.0.0")
}

func TestTransformToArgoCDOCIApplication_SkipRepoCreds(t *testing.T) {
	mockCtx := kubernetes.SetupMockContext(t)
	mockCtx.On("GetServerURL").Return("")

	spaceID := uuid.New()
	payload := api.BridgeWorkerPayload{
		TargetOptions: map[string]string{"OCIHost": "127.0.0.1:1", "DisableRepoCreds": "false"},
		UnitSlug:     "my-app",
		SpaceSlug:    "test-space",
		SpaceID:      spaceID,
		RevisionNum:  1,
		Data:         testConfigMapYAML,
	}

	worker := &ArgoCDOCIWorker{
		workerID:     "test-worker-id",
		workerSecret: "test-worker-secret",
	}

	// With skipRepoCreds=true, even though credentials are available and DisableRepoCreds=false,
	// no Secret should be generated
	_, err := worker.transformToArgoCDOCIApplication(mockCtx, &payload, true)
	assert.NoError(t, err)

	yamlStr := string(payload.Data)
	assert.NotContains(t, yamlStr, "kind: Secret", "skipRepoCreds=true should suppress Secret generation")
	assert.Contains(t, yamlStr, "kind: Application")
}
