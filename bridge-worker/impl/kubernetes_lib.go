// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fluxcd/cli-utils/pkg/kstatus/polling"
	"github.com/fluxcd/pkg/ssa"
	ssautil "github.com/fluxcd/pkg/ssa/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/configkit/k8skit"
)

// parseTargetParams extracts and parses target parameters
func parseTargetParams(payload api.BridgeWorkerPayload) (KubernetesWorkerParams, string, error) {
	var params KubernetesWorkerParams
	if len(payload.TargetParams) > 0 {
		if err := json.Unmarshal(payload.TargetParams, &params); err != nil {
			return KubernetesWorkerParams{}, "", fmt.Errorf("failed to parse target params: %v (%s)", err, string(payload.TargetParams))
		}
	}

	return params, params.KubeContext, nil
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

// parseObjects parses YAML objects from payload data
func parseObjects(data []byte) ([]*unstructured.Unstructured, error) {
	objects, err := ssautil.ReadObjects(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML resources: %v", err)
	}
	log.Log.Info("✅ Parsed YAML resources", "count", len(objects))
	return objects, nil
}

func getLiveObjects(
	wctx api.BridgeWorkerContext,
	manager ResourceManager,
	objects []*unstructured.Unstructured,
	doCleanup bool,
) ([]*unstructured.Unstructured, error) {
	k8sCli := manager.Client() // Use the Client() method of the ResourceManager
	if k8sCli == nil {
		return nil, fmt.Errorf("resource manager client is nil")
	}

	liveObjects := make([]*unstructured.Unstructured, len(objects))
	for i, obj := range objects {
		key := client.ObjectKey{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}
		u := obj.DeepCopyObject().(*unstructured.Unstructured)
		if err := k8sCli.Get(wctx.Context(), key, u); err != nil {
			return nil, err
		}

		if doCleanup {
			cleanup(u)
		}
		liveObjects[i] = u
	}
	return liveObjects, nil
}

func cleanup(u *unstructured.Unstructured) {
	// Clean up fields we don't want to store

	// TODO: We don't want to copy these into config data, but they may be useful
	// as part of the live state. Consider moving them to extraCleanupObjects.
	u.SetManagedFields(nil)
	unstructured.RemoveNestedField(u.Object, "status")
	unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(u.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(u.Object, "metadata", "generation")
	unstructured.RemoveNestedField(u.Object, "metadata", "uid")
	unstructured.RemoveNestedField(u.Object, "metadata", "finalizers")
	unstructured.RemoveNestedField(u.Object, "metadata", "deletionGracePeriodSeconds")
	unstructured.RemoveNestedField(u.Object, "metadata", "deletionTimestamp")
	unstructured.RemoveNestedField(u.Object, "metadata", "ownerReferences")
	// Remove deprecated selfLink field
	unstructured.RemoveNestedField(u.Object, "metadata", "selfLink")
}

// extraCleanupObjects performs heuristic cleanup on imported objects to make them suitable for being unit.Data
func extraCleanupObjects(objects []*unstructured.Unstructured) []*unstructured.Unstructured {
	for _, obj := range objects {
		cleanup(obj)
		removeInternalAnnotations(obj)
		removeInternalLabels(obj)
	}
	return objects
}

// removeInternalAnnotations removes known autogenerated and cluster-internal annotations
func removeInternalAnnotations(obj *unstructured.Unstructured) {
	annotations := obj.GetAnnotations()
	for k := range annotations {
		// Remove known annotation prefixes
		// Check prefixes first - if found, skip specific key check
		foundPrefix := false
		for _, prefix := range k8skit.K8sInternalAnnotationPrefixes {
			if strings.HasPrefix(k, prefix) {
				delete(annotations, k)
				foundPrefix = true
				break // Break out of inner loop once found
			}
		}
		// Remove specific known annotation keys
		// Only check specific keys if not already deleted by prefix
		if !foundPrefix {
			for _, key := range k8skit.K8sInternalAnnotationKeys {
				if k == key {
					delete(annotations, k)
					break // Break out of inner loop once found
				}
			}
		}
	}
	obj.SetAnnotations(annotations)
}

// removeInternalLabels removes known autogenerated and cluster-internal labels
// These label prefixes are rare, but some controllers add these
func removeInternalLabels(obj *unstructured.Unstructured) {
	labels := obj.GetLabels()
	for k := range labels {
		for _, prefix := range k8skit.K8sInternalLabelPrefixes {
			if strings.HasPrefix(k, prefix) {
				delete(labels, k)
				break // Break out of inner loop once found
			}
		}
	}
	obj.SetLabels(labels)
}

// TODO: move to k8skit
func parseGroupVersionKind(s string) (schema.GroupVersionKind, error) {
	parts := strings.Split(s, "/")
	kind := parts[len(parts)-1]
	gvStr := strings.Join(parts[:len(parts)-1], "/")
	gv, err := schema.ParseGroupVersion(gvStr)
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	return schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    kind,
	}, nil
}

type ResourceManager interface {
	ApplyAllStaged(ctx context.Context, objects []*unstructured.Unstructured, opts ssa.ApplyOptions) (*ssa.ChangeSet, error)
	Wait(objects []*unstructured.Unstructured, opts ssa.WaitOptions) error
	WaitForTermination(objects []*unstructured.Unstructured, opts ssa.WaitOptions) error
	DeleteAll(ctx context.Context, objects []*unstructured.Unstructured, opts ssa.DeleteOptions) (*ssa.ChangeSet, error)
	Client() KubernetesClient // Updated to return KubernetesClient interface
}

type KubernetesClient interface {
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error
	Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
	DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error
	IsObjectNamespaced(obj runtime.Object) (bool, error)
	// Add other methods as needed
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

type WrappedResourceManager struct {
	*ssa.ResourceManager
	client KubernetesClient
}

func (w *WrappedResourceManager) Client() KubernetesClient {
	return w.client
}
