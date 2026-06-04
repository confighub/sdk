// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	ssautil "github.com/fluxcd/pkg/ssa/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/bridge-impl/kubernetes/cleanup"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/worker/api"
)

// LargeWaitTimeout is effectively infinite (~10 years) - disabled per #3220
// The Bridge should report what Kubernetes reports, not have its own timeout-based failure.
const LargeWaitTimeout = 87600 * time.Hour

type KubernetesWorkerParams struct {
	KubeContext string `json:",omitempty"`
	WaitTimeout string `json:",omitempty"` // Duration string like "5m0s", "10h5m"

	// Retry configuration
	RetryInitialInterval string  `json:",omitempty"` // Initial retry interval (e.g., "10s")
	RetryMultiplier      float64 `json:",omitempty"` // Backoff multiplier (e.g., 2.0)
	RetryMaxInterval     string  `json:",omitempty"` // Max retry interval (e.g., "5m")
	RetryMaxElapsedTime  string  `json:",omitempty"` // Max total time for retries (e.g., "30m")
}

func (p KubernetesWorkerParams) ToMap() map[string]interface{} {
	var result map[string]interface{}
	data, _ := json.Marshal(p)
	_ = json.Unmarshal(data, &result)
	return result
}

// EnsureConfigHubContext sets ConfigHub context annotations (UnitSlug, SpaceID, RevisionNum)
// on a parsed Kubernetes object. Used by the K8s bridge which already has parsed objects.
// See also k8skit.EnsureConfigHubContextOnData for a text-based variant that preserves
// YAML comments and key ordering.
func EnsureConfigHubContext(obj *unstructured.Unstructured, unitSlug, spaceID string, revisionNum int64) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[k8skit.UnitSlugAnnotation] = unitSlug
	annotations[k8skit.SpaceIDAnnotation] = spaceID
	annotations[k8skit.RevisionNumAnnotation] = strconv.FormatInt(revisionNum, 10)
	obj.SetAnnotations(annotations)
}

// ParseTargetParams extracts and parses target parameters.
// KubeContext is resolved from BridgeHandle first (the new model), falling back
// to the deprecated KubeContext field in TargetParams JSON for backward compatibility.
func ParseTargetParams(payload api.BridgeWorkerPayload) (KubernetesWorkerParams, string, error) {
	var params KubernetesWorkerParams
	if len(payload.TargetParams) > 0 {
		if err := json.Unmarshal(payload.TargetParams, &params); err != nil {
			return KubernetesWorkerParams{}, "", fmt.Errorf("failed to parse target params: %v (%s)", err, string(payload.TargetParams))
		}
	}

	// Override WaitTimeout to LargeWaitTimeout per #3220
	// The Bridge should not have its own timeout-based failure
	params.WaitTimeout = LargeWaitTimeout.String()

	// Prefer BridgeHandle over deprecated params.KubeContext.
	// BridgeHandle is the canonical source for connection identity;
	// params.KubeContext is kept only for backward compatibility with old targets.
	kubeContext := payload.BridgeHandle
	if kubeContext == "" {
		kubeContext = params.KubeContext
	}

	// Normalize: "cluster" is the BridgeHandle for in-cluster targets, but
	// KubernetesConfigFactory expects "" to trigger in-cluster config detection.
	// Old targets had KubeContext="" for in-cluster; new targets have BridgeHandle="cluster".
	if kubeContext == "cluster" {
		kubeContext = ""
	}

	return params, kubeContext, nil
}

// ResolveNamespace determines the namespace to enforce on resources as their
// metadata.namespace, when enforcement is desired.
// Precedence:
//  1. TargetOptions["Namespace"] — explicit user override via BridgeOptions
//  2. Pod namespace (in-cluster) or kubeconfig context.namespace (out-of-cluster)
//  3. "" — no namespace enforcement
func ResolveNamespace(payload api.BridgeWorkerPayload) string {
	// 1. Explicit override from BridgeOptions
	if v, ok := payload.TargetOptions["Namespace"]; ok && v != "" {
		return v
	}

	// This is believed to not be useful.
	// 2. Resolve from context — use BridgeHandle (not the normalized kubeContext)
	// because resolveContextNamespace needs "cluster" to trigger in-cluster detection.
	// Fall back to TargetParams KubeContext for old targets.
	// handle := payload.BridgeHandle
	// if handle == "" {
	// 	var params KubernetesWorkerParams
	// 	if len(payload.TargetParams) > 0 {
	// 		json.Unmarshal(payload.TargetParams, &params)
	// 	}
	// 	handle = params.KubeContext
	// }
	// if ns := resolveContextNamespace(handle); ns != "" {
	// 	return ns
	// }

	// 3. No enforcement
	return ""
}

// We treat the worker as potentially multi-tenant and constrain the namespaces it can deploy to by:
// (a) in-cluster permissions
// (b) the Namespace bridge option set on the Target (or Unit) - see validateNamespaces
// (c) a Trigger associated with the Target can set the namespace

// resolveContextNamespace read the namespace from a kubeconfig context.
// For in-cluster pods, reads the pod's namespace from the service account mount.
// Returns empty string if no namespace can be determined.
// func resolveContextNamespace(kubeContext string) string {
// In-cluster:
// The pod's own namespace is in the service account mount.

// This is probably NOT what the user wants.

// if kubeContext == "" || kubeContext == "cluster" {
// 	if ns, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil && len(ns) > 0 {
// 		return strings.TrimSpace(string(ns))
// 	}
// 	return ""
// }

// The namespace last set in the context is potentially arbitrary based on what the kubecontext
// was last used for, and is not necessarily what the user wants to use.

// loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
// configOverrides := &clientcmd.ConfigOverrides{
// 	CurrentContext: kubeContext,
// }
// kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
// ns, _, err := kubeConfig.Namespace()
// if err != nil {
// 	return ""
// }
// // clientcmd.Namespace() returns "default" when no namespace is configured;
// // return empty so the caller's fallback logic handles it uniformly.
// if ns == "default" {
// 	return ""
// }
// return ns
// }

// ParseObjects parses YAML objects from payload data
func ParseObjects(data []byte) ([]*unstructured.Unstructured, error) {
	objects, err := ssautil.ReadObjects(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML resources: %v", err)
	}
	log.Log.Info("✅ Parsed YAML resources", "count", len(objects))
	return objects, nil
}

// GetLiveObjects fetches live versions of expected objects from the cluster.
// When skipNotFound is true, resources that return a NotFound error are silently
// skipped (e.g., not yet created during ArgoCD sync). All other errors (permission
// denied, network timeouts, RBAC failures) are always returned immediately.
func GetLiveObjects(
	ctx context.Context,
	k8sClient KubernetesClient,
	objects []*unstructured.Unstructured,
	doCleanup bool,
	skipNotFound bool,
) ([]*unstructured.Unstructured, error) {
	var liveObjects []*unstructured.Unstructured
	for _, obj := range objects {
		u := obj.DeepCopyObject().(*unstructured.Unstructured)
		if err := k8sClient.Get(ctx, client.ObjectKey{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}, u); err != nil {
			if skipNotFound && apierrors.IsNotFound(err) {
				log.Log.Info("Resource not found in cluster, skipping",
					"name", obj.GetName(), "namespace", obj.GetNamespace(),
					"kind", obj.GetKind())
				continue
			}
			return nil, err
		}
		if doCleanup {
			cleanup.Cleanup(u)
		}
		liveObjects = append(liveObjects, u)
	}
	return liveObjects, nil
}

// CleanupObjects applies Cleanup() to all objects and returns the cleaned copies.
// This is used to prepare objects for LiveData (config storage) while preserving
// the original objects for LiveState (status tracking).

// BuildOriginalNamespaceMap parses the original unit data and returns a map of
// "Group/Kind/Name" -> namespace for resources that have an explicit namespace set.
// Used by OCI bridges to determine which resources should retain their namespace
// in LiveData — resources not in this map have their namespace cleared.
func BuildOriginalNamespaceMap(data []byte) map[string]string {
	nsMap := make(map[string]string)
	if len(data) == 0 {
		return nsMap
	}
	objects, err := ParseObjects(data)
	if err != nil {
		return nsMap
	}
	for _, obj := range objects {
		if ns := obj.GetNamespace(); ns != "" {
			key := obj.GroupVersionKind().Group + "/" + obj.GetKind() + "/" + obj.GetName()
			nsMap[key] = ns
		}
	}
	return nsMap
}

// OriginalNamespaceKey returns the map key used by BuildOriginalNamespaceMap.
func OriginalNamespaceKey(obj *unstructured.Unstructured) string {
	return obj.GroupVersionKind().Group + "/" + obj.GetKind() + "/" + obj.GetName()
}

// CleanBaseDataForDrift prepares "base data" (the last-applied revision YAML)
// for drift comparison by stripping comments, internal annotations, and
// normalizing resource quantities — the same cleanup rules applied to LiveData.
// This ensures textual differences that are not real drift (e.g. comments,
// controller-injected annotations, "2000m" vs "2") do not trigger false positives.
func CleanBaseDataForDrift(baseData []byte) ([]byte, error) {
	stripped, err := yamlkit.StripComments(baseData)
	if err != nil {
		log.Log.Error(err, "Failed to strip comments from baseData, continuing without stripping")
		stripped = baseData
	}

	objects, parseErr := ParseObjects(stripped)
	if parseErr != nil || len(objects) == 0 {
		return stripped, parseErr
	}
	for _, obj := range objects {
		cleanup.RemoveInternalAnnotations(obj)
		cleanup.NormalizeResourceQuantities(obj)
	}
	cleaned, marshalErr := ObjectsToYAML(objects)
	if marshalErr != nil {
		return stripped, marshalErr
	}
	return []byte(cleaned), nil
}

// TODO: move to k8skit
func ParseGroupVersionKind(s string) (schema.GroupVersionKind, error) {
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

type KubernetesClient interface {
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error
	Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error
	Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
	DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error
	IsObjectNamespaced(obj runtime.Object) (bool, error)
	// Add other methods as needed
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

