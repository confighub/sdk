// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fluxcd/pkg/ssa"
	ssautil "github.com/fluxcd/pkg/ssa/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	funcapi "github.com/confighub/sdk/core/function/api"
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
// on the given Kubernetes object. These annotations correspond to the paths returned by
// K8sResourceProvider.ContextPath() for each field.
func EnsureConfigHubContext(obj *unstructured.Unstructured, unitSlug, spaceID string, revisionNum int64) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[k8skit.UnitSlugAnnotation] = unitSlug
	annotations[k8skit.SpaceIDAnnotation] = spaceID
	annotations[k8skit.RevisionNumAnnotation] = fmt.Sprintf("%d", revisionNum)
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

// ResolveNamespace determines the namespace for resources that lack an explicit namespace.
// Precedence:
//  1. TargetOptions["Namespace"] — explicit user override via BridgeOptions
//  2. Pod namespace (in-cluster) or kubeconfig context.namespace (out-of-cluster)
//  3. "default" — hardcoded fallback
func ResolveNamespace(payload api.BridgeWorkerPayload) string {
	// 1. Explicit override from BridgeOptions
	if v, ok := payload.TargetOptions["Namespace"]; ok && v != "" {
		return v
	}

	// 2. Resolve from context — use BridgeHandle (not the normalized kubeContext)
	// because resolveContextNamespace needs "cluster" to trigger in-cluster detection.
	// Fall back to TargetParams KubeContext for old targets.
	handle := payload.BridgeHandle
	if handle == "" {
		var params KubernetesWorkerParams
		if len(payload.TargetParams) > 0 {
			json.Unmarshal(payload.TargetParams, &params)
		}
		handle = params.KubeContext
	}
	ns := resolveContextNamespace(handle)
	if ns != "" {
		return ns
	}

	// 3. Fallback
	return "default"
}

// resolveContextNamespace reads the namespace from a kubeconfig context.
// For in-cluster pods, reads the pod's namespace from the service account mount.
// Returns empty string if no namespace can be determined.
func resolveContextNamespace(kubeContext string) string {
	// In-cluster: read the pod's own namespace from the service account mount.
	// This is the namespace the pod is running in, which is often the intended
	// deployment target in multi-tenant setups.
	if kubeContext == "" || kubeContext == "cluster" {
		if ns, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil && len(ns) > 0 {
			return strings.TrimSpace(string(ns))
		}
		return ""
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{
		CurrentContext: kubeContext,
	}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	ns, _, err := kubeConfig.Namespace()
	if err != nil {
		return ""
	}
	// clientcmd.Namespace() returns "default" when no namespace is configured;
	// return empty so the caller's fallback logic handles it uniformly.
	if ns == "default" {
		return ""
	}
	return ns
}

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
			Cleanup(u)
		}
		liveObjects = append(liveObjects, u)
	}
	return liveObjects, nil
}

// CleanupObjects applies Cleanup() to all objects and returns the cleaned copies.
// This is used to prepare objects for LiveData (config storage) while preserving
// the original objects for LiveState (status tracking).
func CleanupObjects(objects []*unstructured.Unstructured) []*unstructured.Unstructured {
	cleaned := make([]*unstructured.Unstructured, len(objects))
	for i, obj := range objects {
		u := obj.DeepCopy()
		Cleanup(u)
		cleaned[i] = u
	}
	return cleaned
}

func Cleanup(u *unstructured.Unstructured) {
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

func GvkToResourceType(gvk *schema.GroupVersionKind) funcapi.ResourceType {
	var resourceTypeString string
	if gvk.Group != "" {
		resourceTypeString = gvk.Group + "/"
	}
	resourceTypeString += gvk.Version + "/" + gvk.Kind
	return funcapi.ResourceType(resourceTypeString)
}

func IsService(gvk *schema.GroupVersionKind) bool {
	return gvk.Kind == "Service" && gvk.Group == "" && gvk.Version == "v1"
}

func IsPersistentVolume(gvk *schema.GroupVersionKind) bool {
	return gvk.Kind == "PersistentVolume" && gvk.Group == "" && gvk.Version == "v1"
}

func IsStandardWorkload(gvk *schema.GroupVersionKind) bool {
	resourceType := GvkToResourceType(gvk)
	_, present := k8skit.K8sWorkloadResourceTypes[resourceType]
	// TODO: Handle CronJob
	return present && resourceType != funcapi.ResourceType("batch/v1/CronJob")
}

// ignoredFieldManagers lists field managers whose managed fields should be removed
// during import/refresh. These are controllers that set default values or dynamically
// modify fields that we don't want to capture as part of the unit's configuration.
var ignoredFieldManagers = map[string]bool{
	// Horizontal/Vertical scaling controllers
	"horizontal-pod-autoscaler-controller": true, // HPA controller
	"vpa-recommender":                      true, // VPA recommender
	"vpa-updater":                          true, // VPA updater
	"vpa-admission-controller":             true, // VPA admission

	// Most of the controllers below are unnecessary because we remove status fields,
	// but they are included for completeness.

	// Kubernetes core controllers that dynamically modify resources
	"endpoint-controller":         true, // Endpoints controller
	"endpointslice-controller":    true, // EndpointSlice controller
	"service-controller":          true, // Service controller (cloud load balancers)
	"deployment-controller":       true, // Deployment controller
	"replicaset-controller":       true, // ReplicaSet controller
	"daemonset-controller":        true, // DaemonSet controller
	"statefulset-controller":      true, // StatefulSet controller
	"job-controller":              true, // Job controller
	"cronjob-controller":          true, // CronJob controller
	"pv-protection-controller":    true, // PV protection finalizer
	"pvc-protection-controller":   true, // PVC protection finalizer
	"attach-detach-controller":    true, // Volume attach/detach
	"persistentvolume-controller": true, // PV controller

	// Node and scheduling controllers
	"node-controller":           true, // Node controller
	"taint-controller":          true, // Taint controller
	"scheduler":                 true, // Kubernetes scheduler
	"kube-scheduler":            true, // Kubernetes scheduler (alternate name)
	"cluster-autoscaler":        true, // Cluster autoscaler
	"descheduler":               true, // Descheduler
	"node-problem-detector":     true, // Node problem detector
	"node-local-dns-sidecar":    true, // Node local DNS
	"node-lifecycle-controller": true, // Node lifecycle

	// Service mesh and ingress controllers
	"istio-pilot":              true, // Istio pilot
	"istiod":                   true, // Istio daemon
	"istio-galley":             true, // Istio galley
	"linkerd-proxy-injector":   true, // Linkerd proxy injector
	"linkerd-destination":      true, // Linkerd destination
	"ingress-nginx-controller": true, // NGINX ingress controller
	"traefik":                  true, // Traefik ingress controller

	// Certificate and secrets controllers
	"cert-manager-certificates-trigger":         true, // cert-manager
	"cert-manager-certificates-issuing":         true, // cert-manager
	"cert-manager-certificates-key-manager":     true, // cert-manager
	"cert-manager-certificates-request-manager": true, // cert-manager
	"cert-manager-certificates-readiness":       true, // cert-manager
	"cert-manager-orders":                       true, // cert-manager
	"cert-manager-challenges":                   true, // cert-manager
	"cert-manager-ingress-shim":                 true, // cert-manager
	"cert-manager-controller":                   true, // cert-manager (generic)
	"external-secrets":                          true, // External Secrets Operator
	"sealed-secrets-controller":                 true, // Sealed Secrets

	// Operators and CRD controllers
	"operator-sdk":            true, // Operator SDK based operators
	"kopf":                    true, // Kopf Python operator framework
	"kube-controller-manager": true, // Generic KCM
}

// RemoveIgnoredManagedFields removes fields from an object that are managed by
// ignored field managers (e.g., defaults, HPA, VPA). This helps clean up
// dynamically-set values during import/refresh.
func RemoveIgnoredManagedFields(obj *unstructured.Unstructured) {
	managedFields := obj.GetManagedFields()
	if len(managedFields) == 0 {
		return
	}

	for _, mf := range managedFields {
		if !ignoredFieldManagers[mf.Manager] {
			continue
		}

		if mf.FieldsV1 == nil || len(mf.FieldsV1.Raw) == 0 {
			continue
		}

		// Parse the FieldsV1 JSON structure
		var fieldsMap map[string]interface{}
		if err := json.Unmarshal(mf.FieldsV1.Raw, &fieldsMap); err != nil {
			log.Log.V(2).Info("Failed to parse FieldsV1", "manager", mf.Manager, "error", err)
			continue
		}

		// Remove the fields from the object
		removeFieldsFromObject(obj.Object, fieldsMap, []string{})
	}
}

// RemoveUnmanagedFields removes fields from an object that aren't present in any
// managedFields entry. These are typically default values set by the API server
// that weren't explicitly specified by any controller or user.
// Fields like restartPolicy, dnsPolicy, schedulerName, terminationMessagePath, etc.
func RemoveUnmanagedFields(obj *unstructured.Unstructured) {
	managedFields := obj.GetManagedFields()
	if len(managedFields) == 0 {
		return
	}

	// Build a combined set of all managed fields from all non-ignored managers
	allManagedFields := make(map[string]interface{})
	for _, mf := range managedFields {
		// Skip ignored managers - their fields should be removed, not kept
		if ignoredFieldManagers[mf.Manager] {
			continue
		}

		// Skip status subresource - we remove status separately
		if mf.Subresource == "status" {
			continue
		}

		if mf.FieldsV1 == nil || len(mf.FieldsV1.Raw) == 0 {
			continue
		}

		var fieldsMap map[string]interface{}
		if err := json.Unmarshal(mf.FieldsV1.Raw, &fieldsMap); err != nil {
			log.Log.V(2).Info("Failed to parse FieldsV1", "manager", mf.Manager, "error", err)
			continue
		}

		// Merge this manager's fields into the combined set
		mergeFieldsV1(allManagedFields, fieldsMap)
	}

	if len(allManagedFields) == 0 {
		return
	}

	// Now remove fields from the object that aren't in allManagedFields
	// We need to preserve certain essential fields
	keepOnlyManagedFields(obj.Object, allManagedFields, []string{})
}

// mergeFieldsV1 merges source FieldsV1 structure into dest
func mergeFieldsV1(dest, source map[string]interface{}) {
	for key, value := range source {
		if existing, ok := dest[key]; ok {
			// Key exists in both - if both are maps, merge recursively
			existingMap, existingIsMap := existing.(map[string]interface{})
			valueMap, valueIsMap := value.(map[string]interface{})
			if existingIsMap && valueIsMap {
				mergeFieldsV1(existingMap, valueMap)
			}
			// If not both maps, the existing value takes precedence (already managed)
		} else {
			// Key doesn't exist in dest - add it
			dest[key] = value
		}
	}
}

// essentialFields are fields that should never be removed even if not managed
var essentialFields = map[string]bool{
	"apiVersion": true,
	"kind":       true,
	"metadata":   true,
	// Note: spec is NOT essential - it should only be kept if managed
}

// essentialMetadataFields are metadata fields that should always be kept
var essentialMetadataFields = map[string]bool{
	"name":      true,
	"namespace": true,
	// Note: annotations and labels are containers that should be kept but pruned
}

// keepOnlyManagedFields removes fields from obj that aren't in managedFields
func keepOnlyManagedFields(obj map[string]interface{}, managedFields map[string]interface{}, path []string) {
	// Build a set of managed field names at this level
	// The value indicates whether there are nested field specifications:
	// - nil or empty map means the field is managed atomically (keep entire value)
	// - non-empty map means there are nested fields to filter
	managedAtThisLevel := make(map[string]map[string]interface{})
	for key, value := range managedFields {
		if strings.HasPrefix(key, "f:") {
			fieldName := strings.TrimPrefix(key, "f:")
			if nestedFields, ok := value.(map[string]interface{}); ok && len(nestedFields) > 0 {
				// Has nested field specifications - will recurse
				managedAtThisLevel[fieldName] = nestedFields
			} else {
				// Empty map {} or not a map - field is managed atomically
				managedAtThisLevel[fieldName] = nil
			}
		} else if key == "." {
			// The "." key means this object is managed
			// If the value is empty {}, the entire object is managed atomically - don't filter
			// If the value has nested fields, recurse to filter them
			if nestedFields, ok := value.(map[string]interface{}); ok && len(nestedFields) > 0 {
				keepOnlyManagedFields(obj, nestedFields, path)
			}
			// If empty map, don't recurse - object is managed atomically
		}
		// We handle k: and v: in list handling below
	}

	// Determine if we're at a special level
	isRoot := len(path) == 0
	isMetadata := len(path) == 1 && path[0] == "metadata"

	// Iterate over object fields and remove unmanaged ones
	for fieldName := range obj {
		// Check if this is an essential field that should always be kept
		if isRoot && essentialFields[fieldName] {
			// Essential root field - recurse if it has nested managed fields
			if nestedManaged, ok := managedAtThisLevel[fieldName]; ok && nestedManaged != nil {
				if nestedObj, ok := obj[fieldName].(map[string]interface{}); ok {
					keepOnlyManagedFields(nestedObj, nestedManaged, append(path, fieldName))
				}
			} else if fieldName == "metadata" {
				// Special handling for metadata - always keep essential subfields
				if metaObj, ok := obj["metadata"].(map[string]interface{}); ok {
					keepOnlyManagedFieldsInMetadata(metaObj, managedAtThisLevel["metadata"])
				}
			}
			continue
		}

		if isMetadata && essentialMetadataFields[fieldName] {
			// Essential metadata field - always keep
			continue
		}

		// Check if this field is managed
		nestedManaged, isManaged := managedAtThisLevel[fieldName]
		if !isManaged {
			// Field is not managed - remove it
			delete(obj, fieldName)
			continue
		}

		// Field is managed
		// If nestedManaged is nil, the field is managed atomically - keep entire value
		// If nestedManaged is non-nil, there are nested fields to filter
		if nestedManaged != nil {
			if nestedObj, ok := obj[fieldName].(map[string]interface{}); ok {
				keepOnlyManagedFields(nestedObj, nestedManaged, append(path, fieldName))
			} else if nestedList, ok := obj[fieldName].([]interface{}); ok {
				// Handle lists with keyed items
				keepOnlyManagedListItems(nestedList, nestedManaged, fieldName, obj)
			}
		}
		// If nestedManaged is nil, keep the field as-is (atomic ownership)
	}
}

// keepOnlyManagedFieldsInMetadata handles metadata specially to preserve essential fields
func keepOnlyManagedFieldsInMetadata(meta map[string]interface{}, managedFields map[string]interface{}) {
	if managedFields == nil {
		// No managed fields info for metadata - only keep essential fields
		for fieldName := range meta {
			if !essentialMetadataFields[fieldName] && fieldName != "annotations" && fieldName != "labels" {
				delete(meta, fieldName)
			}
		}
		return
	}

	// Build managed set for metadata
	managedAtThisLevel := make(map[string]map[string]interface{})
	for key, value := range managedFields {
		if strings.HasPrefix(key, "f:") {
			fieldName := strings.TrimPrefix(key, "f:")
			if nestedFields, ok := value.(map[string]interface{}); ok {
				managedAtThisLevel[fieldName] = nestedFields
			} else {
				managedAtThisLevel[fieldName] = nil
			}
		}
	}

	for fieldName := range meta {
		if essentialMetadataFields[fieldName] {
			continue
		}

		nestedManaged, isManaged := managedAtThisLevel[fieldName]
		if !isManaged {
			delete(meta, fieldName)
			continue
		}

		// For annotations and labels, prune unmanaged entries
		if (fieldName == "annotations" || fieldName == "labels") && nestedManaged != nil {
			if mapField, ok := meta[fieldName].(map[string]interface{}); ok {
				keepOnlyManagedMapEntries(mapField, nestedManaged)
			}
		}
	}
}

// keepOnlyManagedMapEntries keeps only entries in a map that are managed
func keepOnlyManagedMapEntries(mapField map[string]interface{}, managedFields map[string]interface{}) {
	managedKeys := make(map[string]bool)
	for key := range managedFields {
		if strings.HasPrefix(key, "f:") {
			managedKeys[strings.TrimPrefix(key, "f:")] = true
		}
	}

	for key := range mapField {
		if !managedKeys[key] {
			delete(mapField, key)
		}
	}
}

// keepOnlyManagedListItems handles lists with keyed items (k:) in managedFields
func keepOnlyManagedListItems(list []interface{}, managedFields map[string]interface{}, fieldName string, parentObj map[string]interface{}) {
	// Collect all managed keys and their nested fields
	type managedItem struct {
		keyMap       map[string]interface{}
		nestedFields map[string]interface{}
	}
	var managedItems []managedItem

	for key, value := range managedFields {
		if strings.HasPrefix(key, "k:") {
			keyJSON := strings.TrimPrefix(key, "k:")
			var keyMap map[string]interface{}
			if err := json.Unmarshal([]byte(keyJSON), &keyMap); err != nil {
				continue
			}
			nestedFields, _ := value.(map[string]interface{})
			managedItems = append(managedItems, managedItem{keyMap: keyMap, nestedFields: nestedFields})
		}
	}

	// Filter the list to only keep managed items
	var filteredList []interface{}
	for _, item := range list {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// Check if this item matches any managed key
		for _, mi := range managedItems {
			matches := true
			for k, v := range mi.keyMap {
				if !valuesEqual(itemMap[k], v) {
					matches = false
					break
				}
			}
			if matches {
				// Item is managed - prune its fields if we have nested field info
				if mi.nestedFields != nil {
					keepOnlyManagedFields(itemMap, mi.nestedFields, nil)
				}
				filteredList = append(filteredList, item)
				break
			}
		}
	}

	parentObj[fieldName] = filteredList
}

// valuesEqual compares two values for equality, handling numeric type differences
// between JSON (float64) and YAML (int/int64).
func valuesEqual(a, b interface{}) bool {
	// Direct equality check
	if a == b {
		return true
	}

	// Handle numeric type differences (JSON uses float64, YAML uses int/int64)
	switch av := a.(type) {
	case int:
		if bv, ok := b.(float64); ok {
			return float64(av) == bv
		}
		if bv, ok := b.(int64); ok {
			return int64(av) == bv
		}
	case int64:
		if bv, ok := b.(float64); ok {
			return float64(av) == bv
		}
		if bv, ok := b.(int); ok {
			return av == int64(bv)
		}
	case float64:
		if bv, ok := b.(int); ok {
			return av == float64(bv)
		}
		if bv, ok := b.(int64); ok {
			return av == float64(bv)
		}
	}

	return false
}

// removeFieldsFromObject recursively removes fields specified in fieldsMap from the object.
// The fieldsMap follows the FieldsV1 format where:
// - "f:fieldName" represents a field
// - "k:{...}" represents a keyed list item (e.g., containers by name)
// - "v:value" represents a list item by value
func removeFieldsFromObject(obj map[string]interface{}, fieldsMap map[string]interface{}, path []string) {
	for key, value := range fieldsMap {
		if strings.HasPrefix(key, "f:") {
			// Regular field: "f:fieldName"
			fieldName := strings.TrimPrefix(key, "f:")

			// Check if value is empty (leaf node) or has nested fields
			nestedFields, isMap := value.(map[string]interface{})
			if !isMap || len(nestedFields) == 0 {
				// Leaf node - remove this field
				delete(obj, fieldName)
			} else {
				// Has nested fields - recurse into the object
				if nested, ok := obj[fieldName].(map[string]interface{}); ok {
					removeFieldsFromObject(nested, nestedFields, append(path, fieldName))
					// If the nested object is now empty, remove it
					if len(nested) == 0 {
						delete(obj, fieldName)
					}
				}
			}
		} else if strings.HasPrefix(key, "k:") {
			// Keyed list item: "k:{\"name\":\"value\"}"
			// This identifies a list item by a key field (e.g., container by name)
			keyJSON := strings.TrimPrefix(key, "k:")
			nestedFields, isMap := value.(map[string]interface{})

			// Find the parent field name (should be the last regular field in path)
			// Look for the list in the object at the current level
			for fieldName, fieldValue := range obj {
				if list, ok := fieldValue.([]interface{}); ok {
					removeKeyedListItem(list, keyJSON, nestedFields, isMap, fieldName, obj)
				}
			}
		} else if strings.HasPrefix(key, "v:") {
			// Value-based list item: "v:value"
			// This is less common - items identified by their value
			// For now, we don't handle this case as it's rare in practice
			log.Log.V(2).Info("Skipping value-based list item removal", "key", key)
		} else if key == "." {
			// The "." key represents the object itself - recurse into nested fields
			if nestedFields, isMap := value.(map[string]interface{}); isMap {
				removeFieldsFromObject(obj, nestedFields, path)
			}
		}
	}
}

// removeKeyedListItem removes or modifies a list item identified by a key JSON.
// keyJSON is like {"name":"container-name"} which identifies the list item.
func removeKeyedListItem(list []interface{}, keyJSON string, nestedFields map[string]interface{}, hasNestedFields bool, fieldName string, parentObj map[string]interface{}) {
	// Parse the key JSON to get the identifying fields
	var keyMap map[string]interface{}
	if err := json.Unmarshal([]byte(keyJSON), &keyMap); err != nil {
		log.Log.V(2).Info("Failed to parse key JSON", "keyJSON", keyJSON, "error", err)
		return
	}

	// Find the matching item in the list
	for i, item := range list {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// Check if this item matches the key
		matches := true
		for k, v := range keyMap {
			if !valuesEqual(itemMap[k], v) {
				matches = false
				break
			}
		}

		if matches {
			if !hasNestedFields || len(nestedFields) == 0 {
				// Remove the entire list item
				newList := append(list[:i], list[i+1:]...)
				parentObj[fieldName] = newList
			} else {
				// Remove specific fields from this list item
				removeFieldsFromObject(itemMap, nestedFields, nil)
			}
			return
		}
	}
}

// ExtraCleanupObjects performs heuristic cleanup on imported objects to make them suitable for being unit.Data
func ExtraCleanupObjects(objects []*unstructured.Unstructured) []*unstructured.Unstructured {
	for _, obj := range objects {
		// Remove fields that aren't managed by any non-ignored field manager.
		// This removes:
		// 1. Default values set by the API server (not in any managedFields)
		// 2. Fields only managed by ignored managers (HPA, VPA, defaults, etc.)
		// Must be called before Cleanup() removes the managedFields metadata.
		RemoveUnmanagedFields(obj)
		Cleanup(obj)
		RemoveInternalAnnotations(obj)
		removeInternalLabels(obj)
		NormalizeResourceQuantities(obj)

		gvk := obj.GroupVersionKind()
		if IsService(&gvk) {
			// These are allocated
			unstructured.RemoveNestedField(obj.Object, "spec", "clusterIP")
			unstructured.RemoveNestedField(obj.Object, "spec", "clusterIPs")
			// TODO: spec.ports.*.nodePort? nodePort is more often set by the user than clusterIP.
			// We only want to refresh it if it was in the original Data.
			// https://github.com/kubernetes/kubernetes/issues/28551
		} else if IsStandardWorkload(&gvk) {
			// This doesn't really hurt anything, but is ugly and in the most common resources
			unstructured.RemoveNestedField(obj.Object, "spec", "template", "metadata", "creationTimestamp")
		} else if IsPersistentVolume(&gvk) {
			unstructured.RemoveNestedField(obj.Object, "spec", "claimRef", "uid")
			unstructured.RemoveNestedField(obj.Object, "spec", "claimRef", "resourceVersion")
		}
	}
	return objects
}

// RemoveInternalAnnotations removes known autogenerated and cluster-internal annotations
func RemoveInternalAnnotations(obj *unstructured.Unstructured) {
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
	if len(annotations) == 0 {
		unstructured.RemoveNestedField(obj.Object, "metadata", "annotations")
	} else {
		obj.SetAnnotations(annotations)
	}
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
	if len(labels) == 0 {
		// Remove the labels field entirely to avoid "labels: {}" in YAML output,
		// which would cause false drift against original data with no labels.
		unstructured.RemoveNestedField(obj.Object, "metadata", "labels")
	} else {
		obj.SetLabels(labels)
	}
}

// NormalizeResourceQuantities walks the object tree and normalizes Kubernetes
// resource quantity values (CPU, memory, storage, etc.) to their canonical form.
//
// Why this is needed:
// The Kubernetes API server normalizes resource quantities when storing objects.
// For example, "2000m" (millicores) becomes "2" (cores), and "0.5" becomes "500m".
// The user's manifest may specify "cpu: 2000m" but the live object returns "cpu: 2".
// These are semantically equal but textually different, causing false drift when
// comparing the original data against LiveData during Refresh.
//
// This function must be applied symmetrically to BOTH LiveData (via ExtraCleanupObjects)
// AND base data (via cleanBaseDataForDrift) so that both sides use the same canonical
// representation before the diff comparison.
//
// Scope: normalizes string values under "limits" or "requests" maps that are children
// of a "resources" key, covering containers, init containers, PVCs, and any other
// Kubernetes resource specification pattern.
func NormalizeResourceQuantities(obj *unstructured.Unstructured) {
	normalizeQuantitiesRecursive(obj.Object, "")
}

func normalizeQuantitiesRecursive(val interface{}, parentKey string) {
	switch v := val.(type) {
	case map[string]interface{}:
		for key, child := range v {
			if (key == "limits" || key == "requests") && parentKey == "resources" {
				// Normalize all values in resources.limits / resources.requests
				// using Kubernetes' resource.Quantity parser, which produces the
				// same canonical string the API server would store.
				if resMap, ok := child.(map[string]interface{}); ok {
					for rk, rv := range resMap {
						if strVal, ok := rv.(string); ok {
							if q, err := resource.ParseQuantity(strVal); err == nil {
								resMap[rk] = q.String()
							}
						}
					}
				}
			} else {
				normalizeQuantitiesRecursive(child, key)
			}
		}
	case []interface{}:
		for _, item := range v {
			normalizeQuantitiesRecursive(item, parentKey)
		}
	}
}

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
		RemoveInternalAnnotations(obj)
		NormalizeResourceQuantities(obj)
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
	Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error
	Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
	DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error
	IsObjectNamespaced(obj runtime.Object) (bool, error)
	// Add other methods as needed
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

type WrappedResourceManager struct {
	*ssa.ResourceManager
	Client_ KubernetesClient
}

func (w *WrappedResourceManager) Client() KubernetesClient {
	return w.Client_
}
