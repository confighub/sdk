// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"fmt"
	"github.com/fluxcd/cli-utils/pkg/kstatus/polling"
	"github.com/fluxcd/pkg/ssa"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"
)

// SSAApplier implements K8sApplier using FluxCD Server-Side Apply
type SSAApplier struct {
	manager          ResourceManager
	kubernetesClient KubernetesClient
}

// Apply implements K8sApplier.Apply
func (a *SSAApplier) Apply(ctx context.Context, objects []*unstructured.Unstructured) ApplyResult {
	if a.manager == nil || a.kubernetesClient == nil {
		return ApplyResult{Error: fmt.Errorf("resource manager or kubernetes client is not initialized")}
	}
	// Set default namespace if not declared on namespaced resources
	a.setDefaultNamespaceIfNotDeclared(objects)

	changeSet, err := a.manager.ApplyAllStaged(ctx, objects, ssa.DefaultApplyOptions())
	if err != nil {
		log.Log.Error(err, "Failed to apply resources")
		return ApplyResult{Error: fmt.Errorf("failed to apply resources: %w", err)}
	}

	log.Log.Info("✅ Successfully initiated applying resources", "changeset_entries", len(changeSet.Entries))
	return ApplyResult{ResourceSet: NewSSAResourceSetWrapper(changeSet)}
}

// WaitForApply implements K8sApplier.WaitForApply
func (a *SSAApplier) WaitForApply(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult {
	if a.manager == nil || a.kubernetesClient == nil {
		return WaitResult{Error: fmt.Errorf("resource manager or kubernetes client is not initialized")}
	}
	// Set default namespace if not declared on namespaced resources
	a.setDefaultNamespaceIfNotDeclared(objects)

	// Set up wait options with timeout
	waitOpts := ssa.DefaultWaitOptions()
	if timeout > 0 {
		waitOpts.Timeout = timeout
		log.Log.Info("Using custom wait timeout", "timeout", timeout.String())
	}

	if err := a.manager.Wait(objects, waitOpts); err != nil {
		log.Log.Error(err, "Failed to wait for resources")
		return WaitResult{Error: fmt.Errorf("failed to wait for resources: %w", err)}
	}

	log.Log.Info("✅ All resources are ready")

	// Get live objects after successful wait
	// Use a fresh context with timeout since the parent context may have expired during wait
	// Use the WaitTimeout value if set, otherwise default to 30 seconds
	getLiveTimeout := 30 * time.Second
	if timeout > 0 {
		getLiveTimeout = timeout
	}
	getLiveCtx, cancel := context.WithTimeout(context.Background(), getLiveTimeout)
	defer cancel()

	// Return uncleaned live objects - caller will cleanup for LiveData, keep uncleaned for LiveState
	liveObjects, err := a.getLiveObjects(getLiveCtx, objects, false)
	if err != nil {
		return WaitResult{Error: fmt.Errorf("failed to get live objects: %w", err)}
	}

	return WaitResult{LiveObjects: liveObjects}
}

// Refresh implements K8sApplier.Refresh
// Returns uncleaned live objects - caller will cleanup for LiveData, keep uncleaned for LiveState
func (a *SSAApplier) Refresh(ctx context.Context, objects []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	if a.kubernetesClient == nil {
		return nil, fmt.Errorf("kubernetes client is not initialized")
	}
	a.setDefaultNamespaceIfNotDeclared(objects)

	// Return uncleaned live objects - caller will cleanup for LiveData, keep uncleaned for LiveState
	retrievedObjects, err := a.getLiveObjects(ctx, objects, false)
	if err != nil {
		log.Log.Error(err, "Failed to retrieve live objects")
		return nil, fmt.Errorf("failed to retrieve live objects: %v", err)
	}

	log.Log.Info("✅ Successfully retrieved resources", "count", len(retrievedObjects))
	return retrievedObjects, nil
}

// Destroy implements K8sApplier.Destroy
func (a *SSAApplier) Destroy(ctx context.Context, objects []*unstructured.Unstructured) DestroyResult {
	if a.manager == nil || a.kubernetesClient == nil {
		return DestroyResult{Error: fmt.Errorf("resource manager or kubernetes client is not initialized")}
	}
	a.setDefaultNamespaceIfNotDeclared(objects)

	log.Log.Info("🔄 Starting resource destruction...")
	changeSet, err := a.manager.DeleteAll(ctx, objects, ssa.DefaultDeleteOptions())
	if err != nil {
		log.Log.Error(err, "Failed to delete resources")
		return DestroyResult{Error: fmt.Errorf("failed to delete resources: %w", err)}
	}

	log.Log.Info("✅ Successfully initiated destruction of resources", "changeset_entries", len(changeSet.Entries))
	return DestroyResult{ResourceSet: NewSSAResourceSetWrapper(changeSet)}
}

// WaitForDestroy implements K8sApplier.WaitForDestroy
func (a *SSAApplier) WaitForDestroy(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult {
	if a.manager == nil || a.kubernetesClient == nil {
		return WaitResult{Error: fmt.Errorf("resource manager or kubernetes client is not initialized")}
	}
	a.setDefaultNamespaceIfNotDeclared(objects)

	// Set up wait options with timeout
	waitOpts := ssa.DefaultWaitOptions()
	if timeout > 0 {
		waitOpts.Timeout = timeout
		log.Log.Info("Using custom wait timeout", "timeout", timeout.String())
	}

	if err := a.manager.WaitForTermination(objects, waitOpts); err != nil {
		log.Log.Error(err, "Failed to wait for resource termination")
		return WaitResult{Error: fmt.Errorf("failed to wait for resource termination: %w", err)}
	}

	log.Log.Info("✅ All resources terminated successfully")
	// Return empty LiveObjects since everything was destroyed
	return WaitResult{
		LiveObjects: []*unstructured.Unstructured{},
		ResourceSet: NewSimpleResourceSet(),
		Error:       nil,
	}
}

// setDefaultNamespaceIfNotDeclared sets default namespace on namespaced objects where namespace is not set
func (a *SSAApplier) setDefaultNamespaceIfNotDeclared(objects []*unstructured.Unstructured) {
	// Check if kubernetesClient is nil to prevent panic
	if a.kubernetesClient == nil {
		log.Log.Info("Kubernetes client is nil, skipping namespace assignment")
		return
	}

	for _, obj := range objects {
		// obj.GetNamespace() returns empty string for cluster scoped objects
		// and namespaced objects where namespace is not set
		if obj.GetNamespace() == "" {
			// check if it is a namespaced object so we don't set namespace on cluster scoped objects
			isObjectNamespaced, err := a.kubernetesClient.IsObjectNamespaced(obj)
			if err == nil && isObjectNamespaced {
				log.Log.Info("🔄 Setting namespace to default on ", "name", obj.GetName())
				// This is currently not configurable.
				obj.SetNamespace("default")
			}
		}
	}
}

// getLiveObjects retrieves live objects from the cluster
func (a *SSAApplier) getLiveObjects(ctx context.Context, objects []*unstructured.Unstructured, doCleanup bool) ([]*unstructured.Unstructured, error) {
	liveObjects := make([]*unstructured.Unstructured, len(objects))
	for i, obj := range objects {
		key := client.ObjectKey{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}
		u := obj.DeepCopyObject().(*unstructured.Unstructured)
		if err := a.kubernetesClient.Get(ctx, key, u); err != nil {
			return nil, err
		}

		if doCleanup {
			cleanup(u)
		}
		liveObjects[i] = u
	}
	return liveObjects, nil
}

// kubernetesClientFactory is a function type that creates a Kubernetes client and resource manager
// It is used for dependency injection in tests.

var kubernetesConfigFactory = setupKubernetesConfig

func setupKubernetesConfig(kubeContext string) (*rest.Config, error) {
	return config.GetConfigWithContext(kubeContext)
}

var kubernetesClientFactory = setupKubernetesClient

// setupKubernetesClient creates a Kubernetes client and resource manager
// use the kubernetesClientFactory variable instead of calling this function directly
func setupKubernetesClient(kubeContext string) (KubernetesClient, ResourceManager, error) {
	cfg, err := kubernetesConfigFactory(kubeContext)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get Kubernetes config: %v", err)
	}
	log.Log.Info("✅ Got Kubernetes config")

	k8sClient, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}
	log.Log.Info("✅ Created Kubernetes client")

	kubePoller := polling.NewStatusPoller(k8sClient, k8sClient.RESTMapper(), polling.Options{})
	man := ssa.NewResourceManager(k8sClient, kubePoller, ssa.Owner{
		Field: "confighub",
		Group: "confighub.com",
	})
	log.Log.Info("✅ Created resource manager")

	// Wrap the resource manager
	wrappedManager := &WrappedResourceManager{
		ResourceManager: man,
		client:          k8sClient,
	}

	return k8sClient, wrappedManager, nil
}

// NewFluxSSAApplier creates a new K8sApplier instance using the SSA implementation
func NewFluxSSAApplier(config ApplierConfig) (K8sApplier, error) {
	k8sClient, manager, err := kubernetesClientFactory(config.KubeContext)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client and manager: %v", err)
	}

	return &SSAApplier{
		manager:          manager,
		kubernetesClient: k8sClient,
	}, nil
}
