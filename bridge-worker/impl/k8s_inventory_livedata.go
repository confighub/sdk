package impl

import (
	"context"
	"fmt"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/third_party/gaby"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/yaml"
)

const (
	// InventoryConfigMapName is the standard name for the inventory ConfigMap
	InventoryConfigMapName = "inventory"
	// YAMLSeparator is the standard YAML document separator
	YAMLSeparator = "---"
	// InventoryIDLabel is the label key for inventory ID
	InventoryIDLabel = "cli-utils.sigs.k8s.io/inventory-id"
	// FunctionAnnotation is the annotation key for function
	FunctionAnnotation = "config.k8s.io/function"
	// InventoryDataKey is the key for inventory data in ConfigMap
	InventoryDataKey = "inventory"
)

// InventoryConfigMap represents the structure of the inventory ConfigMap
// that tracks applied resources for pruning and lifecycle management
type InventoryConfigMap struct {
	*unstructured.Unstructured
}

// InventoryOptions contains optional metadata for inventory ConfigMap
type InventoryOptions struct {
	Name     string
	SpaceID  string
	UnitSlug string
}

// NewInventoryConfigMap creates a new inventory ConfigMap for the given inventory info
func NewInventoryConfigMap(inv inventory.Info) *InventoryConfigMap {
	return NewInventoryConfigMapWithOptions(inv, InventoryOptions{})
}

// NewInventoryConfigMapWithOptions creates a new inventory ConfigMap with optional metadata
func NewInventoryConfigMapWithOptions(inv inventory.Info, opts InventoryOptions) *InventoryConfigMap {
	// Use provided name or default
	name := opts.Name
	if name == "" {
		name = InventoryConfigMapName
	}

	// Build annotations
	annotations := map[string]interface{}{
		FunctionAnnotation: "inventory",
	}
	if opts.SpaceID != "" {
		annotations["confighub.com/SpaceID"] = opts.SpaceID
	}
	if opts.UnitSlug != "" {
		annotations["confighub.com/UnitSlug"] = opts.UnitSlug
	}

	cm := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": inv.GetNamespace(),
				"labels": map[string]interface{}{
					InventoryIDLabel:      string(inv.GetID()),
					k8skit.LabelManagedBy: "confighub",
				},
				"annotations": annotations,
			},
			"data": map[string]interface{}{
				InventoryDataKey: fmt.Sprintf("# inventory id: %s\n", inv.GetID()),
			},
		},
	}
	return &InventoryConfigMap{Unstructured: cm}
}

// IsValid checks if the InventoryConfigMap is valid
func (i *InventoryConfigMap) IsValid() bool {
	return i != nil && i.Unstructured != nil
}

// GetInventoryID retrieves the inventory ID from the ConfigMap
func (i *InventoryConfigMap) GetInventoryID() string {
	if !i.IsValid() {
		return ""
	}
	labels := i.GetLabels()
	return labels[InventoryIDLabel]
}

// SplitInventoryFromLiveData splits the LiveData YAML into inventory ConfigMap and remaining resources
// The inventory ConfigMap is expected to be the first document in the YAML
func SplitInventoryFromLiveData(liveData []byte) (*InventoryConfigMap, []byte, error) {
	if len(liveData) == 0 {
		return nil, nil, nil
	}

	// Parse all YAML documents using gaby
	documents, err := gaby.ParseAll(liveData)
	if err != nil {
		return nil, liveData, fmt.Errorf("failed to parse YAML documents: %w", err)
	}
	if len(documents) == 0 {
		return nil, liveData, nil
	}

	// Extract first document as potential inventory
	inventoryCM, isInventory := extractInventoryFromDocument(documents[0])
	if !isInventory {
		return nil, liveData, nil
	}

	// Reconstruct remaining documents
	remainingDocs := reconstructRemainingDocuments(documents[1:])
	return inventoryCM, remainingDocs, nil
}

// extractInventoryFromDocument attempts to extract an inventory ConfigMap from a YAML document
func extractInventoryFromDocument(doc *gaby.YamlDoc) (*InventoryConfigMap, bool) {
	data := doc.Data()
	if data == nil {
		return nil, false
	}

	objMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, false
	}

	obj := &unstructured.Unstructured{Object: objMap}
	if !isInventoryConfigMap(obj) {
		return nil, false
	}

	return &InventoryConfigMap{Unstructured: obj}, true
}

// reconstructRemainingDocuments converts remaining YAML documents back to bytes
func reconstructRemainingDocuments(documents []*gaby.YamlDoc) []byte {
	if len(documents) == 0 {
		return nil
	}
	return []byte(gaby.Container(documents).String())
}

// CombineInventoryWithResources combines the inventory ConfigMap with resource YAML
// The inventory ConfigMap is placed as the first document
func CombineInventoryWithResources(inventoryCM *InventoryConfigMap, resources []byte) ([]byte, error) {
	if !inventoryCM.IsValid() {
		return resources, nil
	}

	// Marshal inventory ConfigMap to YAML
	inventoryYAML, err := yaml.Marshal(inventoryCM.Unstructured.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal inventory ConfigMap: %w", err)
	}

	// Create a container for all documents
	var documents []*gaby.YamlDoc

	// Parse inventory YAML and wrap it using gaby
	inventoryDoc, err := gaby.ParseYAML(inventoryYAML)
	if err != nil {
		return nil, fmt.Errorf("failed to parse inventory YAML: %w", err)
	}
	documents = append(documents, inventoryDoc)

	// Parse and add resource documents if they exist
	if len(resources) > 0 {
		resourceDocs, err := gaby.ParseAll(resources)
		if err != nil {
			return nil, fmt.Errorf("failed to parse resource YAML: %w", err)
		}
		documents = append(documents, resourceDocs...)
	}

	// Combine all documents using gaby.Container
	return []byte(gaby.Container(documents).String()), nil
}

// UpdateInventoryConfigMap updates the inventory ConfigMap with new object metadata
func UpdateInventoryConfigMap(inventoryCM *InventoryConfigMap, objRefs object.ObjMetadataSet) error {
	if !inventoryCM.IsValid() {
		return fmt.Errorf("invalid inventory ConfigMap")
	}

	// Create inventory entries in the data section
	data := make(map[string]string)
	data[InventoryDataKey] = fmt.Sprintf("# inventory id: %s\n", inventoryCM.GetInventoryID())
	for _, objRef := range objRefs {
		key := objRef.String()
		data[key] = ""
	}

	// Update the ConfigMap data
	if err := unstructured.SetNestedStringMap(inventoryCM.Object, data, "data"); err != nil {
		return fmt.Errorf("failed to update inventory data: %w", err)
	}

	return nil
}

// GetObjectRefsFromInventory extracts object references from an inventory ConfigMap
func GetObjectRefsFromInventory(inventoryCM *InventoryConfigMap) (object.ObjMetadataSet, error) {
	if !inventoryCM.IsValid() {
		return nil, nil
	}

	data, found, err := unstructured.NestedStringMap(inventoryCM.Object, "data")
	if err != nil {
		return nil, fmt.Errorf("failed to get inventory data: %w", err)
	}
	if !found {
		return object.ObjMetadataSet{}, nil
	}

	return parseObjectRefs(data), nil
}

// parseObjectRefs parses object references from ConfigMap data
func parseObjectRefs(data map[string]string) object.ObjMetadataSet {
	var objRefs object.ObjMetadataSet

	for key := range data {
		if key == InventoryDataKey {
			continue
		}

		if objMeta, err := object.ParseObjMetadata(key); err == nil {
			objRefs = append(objRefs, objMeta)
		}
	}

	return objRefs
}

// isInventoryConfigMap checks if an unstructured object is an inventory ConfigMap
func isInventoryConfigMap(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}

	// Check if it's a ConfigMap
	if obj.GetKind() != "ConfigMap" || obj.GetAPIVersion() != "v1" {
		return false
	}

	// Check by name
	if obj.GetName() == InventoryConfigMapName {
		return true
	}

	// Check for inventory label
	if labels := obj.GetLabels(); labels != nil {
		if _, hasLabel := labels[InventoryIDLabel]; hasLabel {
			return true
		}
	}

	// Check for inventory annotation
	if annotations := obj.GetAnnotations(); annotations != nil {
		if function, hasFunction := annotations[FunctionAnnotation]; hasFunction && function == "inventory" {
			return true
		}
	}

	return false
}

// CreateInventoryFromLiveData creates an InMemInventoryClient initialized with LiveData data
// This is a convenience wrapper for backward compatibility
func CreateInventoryFromLiveData(ctx context.Context, liveData []byte, inv inventory.Info) (*InMemInventoryClient, *InventoryConfigMap, []byte, error) {
	invClient := NewInMemInventoryClient()
	inventoryCM, liveDataWithoutInventoryCM, err := invClient.CreateFromLiveData(ctx, liveData, inv)
	return invClient, inventoryCM, liveDataWithoutInventoryCM, err
}

// SaveInventoryToLiveData updates the LiveData with the current inventory state
// This is a convenience wrapper for backward compatibility
func SaveInventoryToLiveData(invClient *InMemInventoryClient, inventoryCM *InventoryConfigMap, inventoryInfo inventory.Info, resources []byte) ([]byte, error) {
	if invClient == nil {
		return resources, nil
	}
	return invClient.SaveToLiveData(inventoryCM, inventoryInfo, resources)
}
