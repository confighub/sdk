// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
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

// CreateFromLiveState creates an InMemInventoryClient initialized with LiveState data
func (c *InMemInventoryClient) CreateFromLiveState(ctx context.Context, liveState []byte, inv inventory.Info) (*InventoryConfigMap, []byte, error) {
	// Split inventory from LiveState
	inventoryCM, remainingResources, err := SplitInventoryFromLiveState(liveState)
	if err != nil {
		return nil, liveState, fmt.Errorf("failed to split inventory from LiveState: %w", err)
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

// SaveToLiveState updates the LiveState with the current inventory state
func (c *InMemInventoryClient) SaveToLiveState(inventoryCM *InventoryConfigMap, inventoryInfo inventory.Info, resources []byte) ([]byte, error) {
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
	liveState, err := CombineInventoryWithResources(inventoryCM, resources)
	if err != nil {
		return nil, fmt.Errorf("failed to combine inventory with resources: %w", err)
	}

	return liveState, nil
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
