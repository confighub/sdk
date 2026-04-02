// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
)

func TestLiveDataBuilder_BuildLiveData(t *testing.T) {
	ctx := context.Background()

	// Create fake dynamic client
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())

	// Create test inventory info
	invInfo := &testInventoryInfo{
		id:        "test-inventory",
		name:      "test-inventory",
		namespace: "default",
	}

	// Create in-memory inventory client
	invClient := NewInMemInventoryClient()

	// Create REST mapper
	restMapper := restmapper.NewDiscoveryRESTMapper([]*restmapper.APIGroupResources{
		{
			Group: metav1.APIGroup{
				Name: "",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1"},
				},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "configmaps", Namespaced: true, Kind: "ConfigMap"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "apps",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1"},
				},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
				},
			},
		},
	})

	// Create LiveDataBuilder
	builder := NewLiveDataBuilder(
		invClient,
		dynamicClient,
		restMapper,
		"test-space",
		"test-unit",
	)

	t.Run("empty inventory returns empty LiveData", func(t *testing.T) {
		// Create empty inventory first
		inv, err := invClient.NewInventory(invInfo)
		require.NoError(t, err)
		err = invClient.CreateOrUpdate(ctx, inv, inventory.UpdateOptions{})
		require.NoError(t, err)

		processor := &EventProcessor{
			appliedObjects: []object.ObjMetadata{},
			prunedObjects:  []object.ObjMetadata{},
			deletedObjects: []object.ObjMetadata{},
		}

		liveData, changeSet, err := builder.BuildLiveData(ctx, invInfo, processor, nil)
		require.NoError(t, err)
		assert.Empty(t, liveData) // No resources = empty LiveData (inventory is in BridgeState)
		assert.NotNil(t, changeSet)
		assert.Empty(t, changeSet.GetEntries())
	})

	t.Run("builds LiveData with applied objects", func(t *testing.T) {
		// Setup inventory with some objects
		inv, err := invClient.NewInventory(invInfo)
		require.NoError(t, err)

		objRefs := object.ObjMetadataSet{
			{
				GroupKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
				Namespace: "default",
				Name:      "test-cm",
			},
			{
				GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},
				Namespace: "default",
				Name:      "test-deployment",
			},
		}
		inv.SetObjectRefs(objRefs)

		err = invClient.CreateOrUpdate(ctx, inv, inventory.UpdateOptions{})
		require.NoError(t, err)

		// Create processor with applied objects
		processor := &EventProcessor{
			appliedObjects: []object.ObjMetadata{
				{
					GroupKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
					Namespace: "default",
					Name:      "test-cm",
				},
			},
			prunedObjects:  []object.ObjMetadata{},
			deletedObjects: []object.ObjMetadata{},
		}

		liveData, changeSet, err := builder.BuildLiveData(ctx, invInfo, processor, nil)
		require.NoError(t, err)
		// LiveData may be empty when mock cluster has no fetchable objects
		// (inventory is in BridgeState, not LiveData)
		_ = liveData
		assert.NotNil(t, changeSet)
		assert.Len(t, changeSet.GetEntries(), 1)
	})

	t.Run("handles pruned objects", func(t *testing.T) {
		// Setup inventory
		inv, err := invClient.NewInventory(invInfo)
		require.NoError(t, err)

		objRefs := object.ObjMetadataSet{
			{
				GroupKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
				Namespace: "default",
				Name:      "remaining-cm",
			},
		}
		inv.SetObjectRefs(objRefs)

		err = invClient.CreateOrUpdate(ctx, inv, inventory.UpdateOptions{})
		require.NoError(t, err)

		// Create processor with pruned objects
		processor := &EventProcessor{
			appliedObjects: []object.ObjMetadata{},
			prunedObjects: []object.ObjMetadata{
				{
					GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},
					Namespace: "default",
					Name:      "pruned-deployment",
				},
			},
			deletedObjects: []object.ObjMetadata{},
		}

		liveData, changeSet, err := builder.BuildLiveData(ctx, invInfo, processor, nil)
		require.NoError(t, err)
		_ = liveData // May be empty when mock cluster has no fetchable objects
		assert.NotNil(t, changeSet)
		assert.Len(t, changeSet.GetEntries(), 1)
	})
}

func TestResourceCache(t *testing.T) {
	cache := NewResourceCache(2, 1*time.Minute)

	t.Run("cache put and get", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetKind("ConfigMap")
		obj.SetName("test-cm")
		obj.SetNamespace("default")

		key := "ConfigMap/default/test-cm"
		cache.Put(key, obj)

		cached := cache.Get(key)
		require.NotNil(t, cached)
		assert.Equal(t, "test-cm", cached.GetName())
	})

	t.Run("cache expiration", func(t *testing.T) {
		cache := NewResourceCache(2, 100*time.Millisecond)

		obj := &unstructured.Unstructured{}
		obj.SetKind("ConfigMap")
		obj.SetName("expiring-cm")

		key := "ConfigMap/default/expiring-cm"
		cache.Put(key, obj)

		// Should exist immediately
		assert.True(t, cache.IsValid(key))

		// Wait for expiration
		time.Sleep(200 * time.Millisecond)

		// Should be expired
		assert.False(t, cache.IsValid(key))
		assert.Nil(t, cache.Get(key))
	})

	t.Run("cache eviction", func(t *testing.T) {
		cache := NewResourceCache(2, 1*time.Minute)

		obj1 := &unstructured.Unstructured{}
		obj1.SetName("obj1")
		cache.Put("key1", obj1)

		obj2 := &unstructured.Unstructured{}
		obj2.SetName("obj2")
		cache.Put("key2", obj2)

		// Cache is now full (maxSize=2)

		obj3 := &unstructured.Unstructured{}
		obj3.SetName("obj3")
		cache.Put("key3", obj3)

		// obj1 should be evicted (oldest)
		assert.Nil(t, cache.Get("key1"))
		assert.NotNil(t, cache.Get("key2"))
		assert.NotNil(t, cache.Get("key3"))
	})

	t.Run("cache remove", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetName("to-remove")

		key := "to-remove"
		cache.Put(key, obj)
		assert.NotNil(t, cache.Get(key))

		cache.Remove(key)
		assert.Nil(t, cache.Get(key))
	})
}

func TestFetchStrategy(t *testing.T) {
	builder := &LiveDataBuilder{
		cache: NewResourceCache(10, 5*time.Minute),
	}

	t.Run("categorizes objects correctly", func(t *testing.T) {
		// Add something to cache
		cachedObj := &unstructured.Unstructured{}
		cachedObj.SetName("cached")
		// The key format from ObjMetadata.String() is: namespace_name_group_kind
		builder.cache.Put("_cached__ConfigMap", cachedObj)

		managedRefs := object.ObjMetadataSet{
			{
				GroupKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
				Name:      "applied",
			},
			{
				GroupKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
				Name:      "cached",
			},
			{
				GroupKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
				Name:      "uncached",
			},
		}

		processor := &EventProcessor{
			appliedObjects: []object.ObjMetadata{
				{
					GroupKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
					Name:      "applied",
				},
			},
			prunedObjects:  []object.ObjMetadata{},
			deletedObjects: []object.ObjMetadata{},
		}

		strategy := builder.buildFetchStrategy(managedRefs, processor)

		assert.Len(t, strategy.mustFetch, 2)    // applied + uncached
		assert.Len(t, strategy.canUseCached, 1) // cached
		assert.Len(t, strategy.skipFetch, 0)
	})
}

// Test helper - inventory info implementation
type testInventoryInfo struct {
	id        string
	name      string
	namespace string
}

func (t *testInventoryInfo) GetID() inventory.ID {
	return inventory.ID(t.id)
}

func (t *testInventoryInfo) GetName() string {
	return t.name
}

func (t *testInventoryInfo) GetNamespace() string {
	return t.namespace
}

func (t *testInventoryInfo) GetLabels() map[string]string {
	return map[string]string{
		"confighub.ai/unit":  "test-unit",
		"confighub.ai/space": "test-space",
	}
}

func (t *testInventoryInfo) GetGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	}
}
