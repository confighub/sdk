// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// Verify InMemInventoryClient implements the inventory.Client interface
var _ inventory.Client = &InMemInventoryClient{}

// InMemInventory represents an in-memory inventory
type InMemInventory struct {
	info           inventory.Info
	objRefs        object.ObjMetadataSet
	objectStatuses object.ObjectStatusSet
}

// Info returns the inventory info
func (i *InMemInventory) Info() inventory.Info {
	return i.info
}

// GetObjectRefs returns the list of object references tracked in the inventory
func (i *InMemInventory) GetObjectRefs() object.ObjMetadataSet {
	return i.objRefs
}

// GetObjectStatuses returns the list of statuses for each object reference
func (i *InMemInventory) GetObjectStatuses() object.ObjectStatusSet {
	return i.objectStatuses
}

// SetObjectRefs updates the local cache of object references
func (i *InMemInventory) SetObjectRefs(refs object.ObjMetadataSet) {
	i.objRefs = refs
}

// SetObjectStatuses updates the local cache of object statuses
func (i *InMemInventory) SetObjectStatuses(statuses object.ObjectStatusSet) {
	i.objectStatuses = statuses
}

// InMemInventoryClient is an inventory client that stores everything in memory
type InMemInventoryClient struct {
	inventories map[inventory.ID]*InMemInventory
}

// NewInMemInventoryClient creates a new in-memory inventory client
func NewInMemInventoryClient() *InMemInventoryClient {
	return &InMemInventoryClient{
		inventories: make(map[inventory.ID]*InMemInventory),
	}
}

// Get retrieves an inventory object from memory
func (c *InMemInventoryClient) Get(ctx context.Context, info inventory.Info, opts inventory.GetOptions) (inventory.Inventory, error) {
	inv := c.getInventory(info.GetID())
	if inv == nil {
		// Return a proper K8s NotFound error that can be detected by errors.IsNotFound
		return nil, errors.NewNotFound(
			schema.GroupResource{Group: "", Resource: "configmaps"},
			string(info.GetID()),
		)
	}
	return inv, nil
}

// List returns all inventory objects from memory
func (c *InMemInventoryClient) List(ctx context.Context, opts inventory.ListOptions) ([]inventory.Inventory, error) {
	result := make([]inventory.Inventory, 0, len(c.inventories))
	for _, inv := range c.inventories {
		result = append(result, inv)
	}
	return result, nil
}

// CreateOrUpdate creates or updates an inventory object in memory
func (c *InMemInventoryClient) CreateOrUpdate(ctx context.Context, inv inventory.Inventory, opts inventory.UpdateOptions) error {
	if inv == nil {
		return fmt.Errorf("inventory cannot be nil")
	}

	info := inv.Info()
	if info == nil {
		return fmt.Errorf("inventory info cannot be nil")
	}

	// Store the inventory
	c.setInventory(info.GetID(), &InMemInventory{
		info:           info,
		objRefs:        inv.GetObjectRefs(),
		objectStatuses: inv.GetObjectStatuses(),
	})

	return nil
}

// Delete removes an inventory object from memory
func (c *InMemInventoryClient) Delete(ctx context.Context, info inventory.Info, opts inventory.DeleteOptions) error {
	if info == nil {
		return fmt.Errorf("inventory info cannot be nil")
	}
	delete(c.inventories, info.GetID())
	return nil
}

// NewInventory returns an empty initialized inventory object
func (c *InMemInventoryClient) NewInventory(info inventory.Info) (inventory.Inventory, error) {
	if info == nil {
		return nil, fmt.Errorf("inventory info cannot be nil")
	}
	return &InMemInventory{
		info:           info,
		objRefs:        object.ObjMetadataSet{},
		objectStatuses: object.ObjectStatusSet{},
	}, nil
}

// Helper methods to reduce code duplication

// getInventory retrieves an inventory by ID
func (c *InMemInventoryClient) getInventory(id inventory.ID) *InMemInventory {
	return c.inventories[id]
}

// setInventory stores an inventory by ID
func (c *InMemInventoryClient) setInventory(id inventory.ID, inv *InMemInventory) {
	c.inventories[id] = inv
}

// CreateFromLiveData creates an InMemInventoryClient initialized with LiveData data
func (c *InMemInventoryClient) CreateFromLiveData(ctx context.Context, liveData []byte, inv inventory.Info) (*InventoryConfigMap, []byte, error) {
	// Split inventory from LiveData
	inventoryCM, remainingResources, err := SplitInventoryFromLiveData(liveData)
	if err != nil {
		return nil, liveData, fmt.Errorf("failed to split inventory from LiveData: %w", err)
	}

	// Process existing inventory or create new one
	if inventoryCM != nil {
		if err := c.initializeFromConfigMap(ctx, inventoryCM, inv); err != nil {
			return inventoryCM, remainingResources, err
		}
	} else {
		inventoryCM = NewInventoryConfigMap(inv)
	}

	return inventoryCM, remainingResources, nil
}

// initializeFromConfigMap initializes the inventory client with existing data from ConfigMap
func (c *InMemInventoryClient) initializeFromConfigMap(ctx context.Context, inventoryCM *InventoryConfigMap, inv inventory.Info) error {
	objRefs, err := GetObjectRefsFromInventory(inventoryCM)
	if err != nil {
		return fmt.Errorf("failed to extract object refs: %w", err)
	}

	if len(objRefs) == 0 {
		return nil
	}

	newInv, err := c.NewInventory(inv)
	if err != nil {
		return fmt.Errorf("failed to create new inventory: %w", err)
	}

	newInv.SetObjectRefs(objRefs)

	if err := c.CreateOrUpdate(ctx, newInv, inventory.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to initialize inventory: %w", err)
	}

	return nil
}

// SaveToLiveData updates the LiveData with the current inventory state
func (c *InMemInventoryClient) SaveToLiveData(inventoryCM *InventoryConfigMap, inventoryInfo inventory.Info, resources []byte) ([]byte, error) {
	if !inventoryCM.IsValid() {
		return resources, nil
	}

	// Get or create inventory
	inv := c.getOrCreateInventory(inventoryInfo)
	if inv == nil {
		return nil, fmt.Errorf("failed to get or create inventory")
	}

	// Update the inventory ConfigMap with current state
	if err := UpdateInventoryConfigMap(inventoryCM, inv.GetObjectRefs()); err != nil {
		return nil, fmt.Errorf("failed to update inventory ConfigMap: %w", err)
	}

	// Combine inventory with resources
	liveData, err := CombineInventoryWithResources(inventoryCM, resources)
	if err != nil {
		return nil, fmt.Errorf("failed to combine inventory with resources: %w", err)
	}

	return liveData, nil
}

// getOrCreateInventory retrieves existing inventory or creates a new one
func (c *InMemInventoryClient) getOrCreateInventory(inv inventory.Info) inventory.Inventory {
	ctx := context.TODO()

	if existing, err := c.Get(ctx, inv, inventory.GetOptions{}); err == nil {
		return existing
	}

	if newInv, err := c.NewInventory(inv); err == nil {
		return newInv
	}

	return nil
}

// PopulateFromObjects populates the inventory with object metadata from unstructured objects.
// This is used for backward compatibility when inventory is empty but objects need to be tracked
// (e.g., for units created before inventory tracking was added).
func (c *InMemInventoryClient) PopulateFromObjects(ctx context.Context, inv inventory.Info, objects []*unstructured.Unstructured) error {
	if len(objects) == 0 {
		return nil
	}

	// Convert unstructured objects to ObjMetadataSet
	objRefs := object.UnstructuredSetToObjMetadataSet(objects)
	if len(objRefs) == 0 {
		return nil
	}

	// Get existing inventory or create new one
	existingInv, err := c.Get(ctx, inv, inventory.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get existing inventory: %w", err)
	}

	var targetInv inventory.Inventory
	if existingInv != nil {
		targetInv = existingInv
		// Merge with existing refs to avoid losing any tracked objects
		existingRefs := targetInv.GetObjectRefs()
		objRefs = mergeObjMetadataSets(existingRefs, objRefs)
	} else {
		targetInv, err = c.NewInventory(inv)
		if err != nil {
			return fmt.Errorf("failed to create new inventory: %w", err)
		}
	}

	targetInv.SetObjectRefs(objRefs)
	return c.CreateOrUpdate(ctx, targetInv, inventory.UpdateOptions{})
}

// mergeObjMetadataSets merges two ObjMetadataSets, removing duplicates
func mergeObjMetadataSets(set1, set2 object.ObjMetadataSet) object.ObjMetadataSet {
	seen := make(map[string]bool)
	result := make(object.ObjMetadataSet, 0, len(set1)+len(set2))

	for _, ref := range set1 {
		key := ref.String()
		if !seen[key] {
			seen[key] = true
			result = append(result, ref)
		}
	}

	for _, ref := range set2 {
		key := ref.String()
		if !seen[key] {
			seen[key] = true
			result = append(result, ref)
		}
	}

	return result
}
