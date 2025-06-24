// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/fluxcd/cli-utils/pkg/kstatus/polling"
	"github.com/fluxcd/pkg/ssa"

	ssautil "github.com/fluxcd/pkg/ssa/utils"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
var kubernetesClientFactory = setupKubernetesClient

// setupKubernetesClient creates a Kubernetes client and resource manager
// use the kubernetesClientFactory variable instead of calling this function directly
func setupKubernetesClient(kubeContext string) (KubernetesClient, ResourceManager, error) {
	cfg, err := config.GetConfigWithContext(kubeContext)
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

	return wrappedManager.client, wrappedManager, nil
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
}

func extraCleanupObjects(objects []*unstructured.Unstructured) []*unstructured.Unstructured {
	cleanedObjects := make([]*unstructured.Unstructured, 0, len(objects))
	for _, obj := range objects {
		cleaned := obj.DeepCopy()
		cleanup(cleaned)
		// Remove status field as it's managed by the cluster
		unstructured.RemoveNestedField(cleaned.Object, "status")
		// Remove metadata fields that are managed by the cluster
		metadata, found, err := unstructured.NestedMap(cleaned.Object, "metadata")
		if found && err == nil {
			// Keep only essential annotations
			if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
				essentialAnnotations := make(map[string]interface{})
				// Keep only annotations that are not managed by the cluster
				for k, v := range annotations {
					if !strings.HasPrefix(k, "kubectl.kubernetes.io/") &&
						!strings.HasPrefix(k, "kubernetes.io/") &&
						!strings.HasPrefix(k, "k8s.io/") {
						essentialAnnotations[k] = v
					}
				}
				if len(essentialAnnotations) > 0 {
					metadata["annotations"] = essentialAnnotations
				} else {
					delete(metadata, "annotations")
				}
			}

			// Keep only essential labels
			if labels, ok := metadata["labels"].(map[string]interface{}); ok {
				essentialLabels := make(map[string]interface{})
				// Keep only labels that are not managed by the cluster
				for k, v := range labels {
					if !strings.HasPrefix(k, "kubernetes.io/") &&
						!strings.HasPrefix(k, "k8s.io/") {
						essentialLabels[k] = v
					}
				}
				if len(essentialLabels) > 0 {
					metadata["labels"] = essentialLabels
				} else {
					delete(metadata, "labels")
				}
			}

			// Update the cleaned object with the filtered metadata
			unstructured.SetNestedMap(cleaned.Object, metadata, "metadata")
		}

		// Handle specific resource types
		gvk := cleaned.GetObjectKind().GroupVersionKind()
		switch gvk.Kind {
		case "Service":
			// Remove cluster IP and other managed fields
			unstructured.RemoveNestedField(cleaned.Object, "spec", "clusterIP")
			unstructured.RemoveNestedField(cleaned.Object, "spec", "clusterIPs")
			unstructured.RemoveNestedField(cleaned.Object, "spec", "externalTrafficPolicy")
			unstructured.RemoveNestedField(cleaned.Object, "spec", "healthCheckNodePort")
			unstructured.RemoveNestedField(cleaned.Object, "spec", "sessionAffinityConfig")

		case "Deployment", "StatefulSet", "DaemonSet":
			// Remove managed fields from workload spec
			if spec, found, err := unstructured.NestedMap(cleaned.Object, "spec", "template", "spec"); found && err == nil {
				delete(spec, "dnsPolicy")
				delete(spec, "restartPolicy")
				delete(spec, "schedulerName")
				delete(spec, "securityContext")
				delete(spec, "terminationGracePeriodSeconds")
				unstructured.SetNestedMap(cleaned.Object, spec, "spec", "template", "spec")
			}
		}

		cleanedObjects = append(cleanedObjects, cleaned)
	}
	return cleanedObjects
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
