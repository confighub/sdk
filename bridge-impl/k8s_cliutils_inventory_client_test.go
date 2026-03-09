// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
)

func TestPopulateFromObjects_EmptyInventory(t *testing.T) {
	ctx := context.Background()
	invClient := NewInMemInventoryClient()

	invInfo := &testInventoryInfo{
		id:        "test-space-test-unit",
		name:      "inventory",
		namespace: "default",
	}

	// Create test objects
	objects := []*unstructured.Unstructured{
		{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "test-cm",
					"namespace": "default",
				},
			},
		},
		{
			Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "test-deploy",
					"namespace": "default",
				},
			},
		},
	}

	// Populate inventory from objects
	err := invClient.PopulateFromObjects(ctx, invInfo, objects)
	require.NoError(t, err)

	// Verify inventory was populated
	inv, err := invClient.Get(ctx, invInfo, inventory.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, inv)

	objRefs := inv.GetObjectRefs()
	assert.Len(t, objRefs, 2)
}

func TestPopulateFromObjects_ExistingInventory(t *testing.T) {
	ctx := context.Background()
	invClient := NewInMemInventoryClient()

	invInfo := &testInventoryInfo{
		id:        "test-space-test-unit",
		name:      "inventory",
		namespace: "default",
	}

	// Create existing inventory with one object
	existingInv, err := invClient.NewInventory(invInfo)
	require.NoError(t, err)
	existingInv.SetObjectRefs(object.ObjMetadataSet{
		{
			GroupKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
			Namespace: "default",
			Name:      "existing-cm",
		},
	})
	err = invClient.CreateOrUpdate(ctx, existingInv, inventory.UpdateOptions{})
	require.NoError(t, err)

	// Create new objects to add
	objects := []*unstructured.Unstructured{
		{
			Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "new-deploy",
					"namespace": "default",
				},
			},
		},
	}

	// Populate inventory from objects (should merge)
	err = invClient.PopulateFromObjects(ctx, invInfo, objects)
	require.NoError(t, err)

	// Verify inventory was merged
	inv, err := invClient.Get(ctx, invInfo, inventory.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, inv)

	objRefs := inv.GetObjectRefs()
	assert.Len(t, objRefs, 2) // existing-cm + new-deploy
}

func TestPopulateFromObjects_EmptyObjects(t *testing.T) {
	ctx := context.Background()
	invClient := NewInMemInventoryClient()

	invInfo := &testInventoryInfo{
		id:        "test-space-test-unit",
		name:      "inventory",
		namespace: "default",
	}

	// Populate with empty objects slice
	err := invClient.PopulateFromObjects(ctx, invInfo, []*unstructured.Unstructured{})
	require.NoError(t, err)

	// Verify inventory was not created (no-op)
	_, err = invClient.Get(ctx, invInfo, inventory.GetOptions{})
	assert.Error(t, err) // Should be NotFound error
}

func TestPopulateFromObjects_Duplicates(t *testing.T) {
	ctx := context.Background()
	invClient := NewInMemInventoryClient()

	invInfo := &testInventoryInfo{
		id:        "test-space-test-unit",
		name:      "inventory",
		namespace: "default",
	}

	// Create existing inventory with an object
	existingInv, err := invClient.NewInventory(invInfo)
	require.NoError(t, err)
	existingInv.SetObjectRefs(object.ObjMetadataSet{
		{
			GroupKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
			Namespace: "default",
			Name:      "same-cm",
		},
	})
	err = invClient.CreateOrUpdate(ctx, existingInv, inventory.UpdateOptions{})
	require.NoError(t, err)

	// Create objects with the same ConfigMap (duplicate)
	objects := []*unstructured.Unstructured{
		{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "same-cm",
					"namespace": "default",
				},
			},
		},
		{
			Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "new-deploy",
					"namespace": "default",
				},
			},
		},
	}

	// Populate inventory from objects (should deduplicate)
	err = invClient.PopulateFromObjects(ctx, invInfo, objects)
	require.NoError(t, err)

	// Verify duplicates were removed
	inv, err := invClient.Get(ctx, invInfo, inventory.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, inv)

	objRefs := inv.GetObjectRefs()
	assert.Len(t, objRefs, 2) // same-cm (deduplicated) + new-deploy
}

func TestMergeObjMetadataSets(t *testing.T) {
	t.Run("merges two disjoint sets", func(t *testing.T) {
		set1 := object.ObjMetadataSet{
			{GroupKind: schema.GroupKind{Kind: "ConfigMap"}, Namespace: "default", Name: "cm1"},
		}
		set2 := object.ObjMetadataSet{
			{GroupKind: schema.GroupKind{Kind: "ConfigMap"}, Namespace: "default", Name: "cm2"},
		}

		result := mergeObjMetadataSets(set1, set2)
		assert.Len(t, result, 2)
	})

	t.Run("removes duplicates", func(t *testing.T) {
		set1 := object.ObjMetadataSet{
			{GroupKind: schema.GroupKind{Kind: "ConfigMap"}, Namespace: "default", Name: "cm1"},
		}
		set2 := object.ObjMetadataSet{
			{GroupKind: schema.GroupKind{Kind: "ConfigMap"}, Namespace: "default", Name: "cm1"},
			{GroupKind: schema.GroupKind{Kind: "ConfigMap"}, Namespace: "default", Name: "cm2"},
		}

		result := mergeObjMetadataSets(set1, set2)
		assert.Len(t, result, 2) // cm1 should only appear once
	})

	t.Run("handles empty sets", func(t *testing.T) {
		set1 := object.ObjMetadataSet{}
		set2 := object.ObjMetadataSet{
			{GroupKind: schema.GroupKind{Kind: "ConfigMap"}, Namespace: "default", Name: "cm1"},
		}

		result := mergeObjMetadataSets(set1, set2)
		assert.Len(t, result, 1)

		result = mergeObjMetadataSets(set2, set1)
		assert.Len(t, result, 1)
	})
}
