// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	ssautil "github.com/fluxcd/pkg/ssa/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// LiveDataBuilder optimally builds LiveData from tracked events
type LiveDataBuilder struct {
	inventory     inventory.Client
	dynamicClient dynamic.Interface
	restMapper    meta.RESTMapper
	cache         *ResourceCache
	spaceID       string
	unitSlug      string
}

// ResourceCache provides LRU caching for unchanged resources
type ResourceCache struct {
	mu        sync.RWMutex
	resources map[string]CachedResource
	maxAge    time.Duration
	maxSize   int
}

// CachedResource represents a cached Kubernetes resource
type CachedResource struct {
	object          *unstructured.Unstructured
	fetchedAt       time.Time
	resourceVersion string
}

// FetchStrategy determines how to fetch resources
type FetchStrategy struct {
	mustFetch    []object.ObjMetadata // Recently changed, must fetch fresh
	canUseCached []object.ObjMetadata // Unchanged, can use cache
	skipFetch    []object.ObjMetadata // Deleted/pruned, should not exist
}

// NewLiveDataBuilder creates a new LiveDataBuilder
func NewLiveDataBuilder(
	inventory inventory.Client,
	dynamicClient dynamic.Interface,
	restMapper meta.RESTMapper,
	spaceID string,
	unitSlug string,
) *LiveDataBuilder {
	return &LiveDataBuilder{
		inventory:     inventory,
		dynamicClient: dynamicClient,
		restMapper:    restMapper,
		cache:         NewResourceCache(100, 5*time.Minute),
		spaceID:       spaceID,
		unitSlug:      unitSlug,
	}
}

// NewResourceCache creates a new resource cache
func NewResourceCache(maxSize int, maxAge time.Duration) *ResourceCache {
	return &ResourceCache{
		resources: make(map[string]CachedResource),
		maxAge:    maxAge,
		maxSize:   maxSize,
	}
}

// BuildLiveData builds optimal LiveData from tracked events
func (b *LiveDataBuilder) BuildLiveData(
	ctx context.Context,
	invInfo inventory.Info,
	processor *EventProcessor,
	previousLiveData []byte,
) (liveData []byte, resourceSet ResourceSet, err error) {
	log.Log.Info("🏗️ Building LiveData from events",
		"spaceID", b.spaceID,
		"unitSlug", b.unitSlug,
		"applied", len(processor.appliedObjects),
		"pruned", len(processor.prunedObjects),
		"deleted", len(processor.deletedObjects))

	// Step 1: Get current inventory (already updated by CLI-Utils)
	currentInventory, err := b.inventory.Get(ctx, invInfo, inventory.GetOptions{})
	if err != nil {
		// If inventory doesn't exist (after destroy), return empty
		if errors.IsNotFound(err) {
			log.Log.Info("📦 Inventory not found (likely after destroy), returning empty LiveData")
			return []byte{}, processor.buildResourceSet(), nil
		}
		return nil, nil, fmt.Errorf("failed to get inventory: %w", err)
	}

	// Step 2: Get current managed objects from inventory
	managedRefs := currentInventory.GetObjectRefs()
	log.Log.Info("📋 Current inventory state",
		"managedObjects", len(managedRefs),
		"inventoryID", invInfo.GetID())

	// Step 3: Build fetch strategy based on events
	fetchStrategy := b.buildFetchStrategy(managedRefs, processor)
	log.Log.Info("📊 Fetch strategy",
		"mustFetch", len(fetchStrategy.mustFetch),
		"canUseCached", len(fetchStrategy.canUseCached),
		"skipFetch", len(fetchStrategy.skipFetch))

	// Step 4: Execute parallel fetching with intelligent batching
	liveObjects := b.fetchResourcesIntelligently(ctx, fetchStrategy)
	log.Log.Info("✅ Fetched live objects",
		"count", len(liveObjects))

	// Step 5: Convert objects to YAML — inventory is stored in BridgeState, not LiveData
	yamlResources, err := ssautil.ObjectsToYAML(liveObjects)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert objects to YAML: %w", err)
	}
	liveData = []byte(yamlResources)

	// Step 8: Build ResourceSet from events
	resourceSet = processor.buildResourceSet()
	log.Log.Info("📝 Built ResourceSet",
		"entries", len(resourceSet.GetEntries()))

	// Step 9: Update cache for next operation
	b.updateCache(liveObjects, processor)

	log.Log.Info("✅ LiveData built successfully",
		"size", len(liveData),
		"objects", len(liveObjects),
		"changes", len(resourceSet.GetEntries()))

	return liveData, resourceSet, nil
}

// buildFetchStrategy determines what needs fetching and how
func (b *LiveDataBuilder) buildFetchStrategy(
	managedRefs object.ObjMetadataSet,
	processor *EventProcessor,
) FetchStrategy {
	strategy := FetchStrategy{
		mustFetch:    make([]object.ObjMetadata, 0),
		canUseCached: make([]object.ObjMetadata, 0),
		skipFetch:    make([]object.ObjMetadata, 0),
	}

	// Build lookup maps for O(1) access
	appliedMap := make(map[string]bool)
	for _, ref := range processor.appliedObjects {
		appliedMap[ref.String()] = true
	}

	prunedMap := make(map[string]bool)
	for _, ref := range processor.prunedObjects {
		prunedMap[ref.String()] = true
	}

	deletedMap := make(map[string]bool)
	for _, ref := range processor.deletedObjects {
		deletedMap[ref.String()] = true
	}

	// Categorize each managed resource
	for _, ref := range managedRefs {
		key := ref.String()

		switch {
		case appliedMap[key]:
			// Just applied/updated - MUST fetch fresh
			strategy.mustFetch = append(strategy.mustFetch, ref)

		case prunedMap[key] || deletedMap[key]:
			// Should not be in inventory anymore - skip
			// This is a consistency check - shouldn't happen
			log.Log.V(1).Info("⚠️ Found pruned/deleted object still in inventory", "ref", ref)
			strategy.skipFetch = append(strategy.skipFetch, ref)

		default:
			// Unchanged - can use cache if available
			if b.cache.IsValid(key) {
				strategy.canUseCached = append(strategy.canUseCached, ref)
			} else {
				strategy.mustFetch = append(strategy.mustFetch, ref)
			}
		}
	}

	return strategy
}

// fetchResourcesIntelligently performs parallel fetching with error handling
func (b *LiveDataBuilder) fetchResourcesIntelligently(
	ctx context.Context,
	strategy FetchStrategy,
) []*unstructured.Unstructured {
	results := make([]*unstructured.Unstructured, 0)
	resultsMu := sync.Mutex{}

	// Use worker pool for parallel fetching
	workerCount := min(10, max(1, len(strategy.mustFetch)))
	if len(strategy.mustFetch) == 0 {
		workerCount = 0
	}

	if workerCount > 0 {
		work := make(chan object.ObjMetadata, len(strategy.mustFetch))
		var wg sync.WaitGroup

		// Start workers
		for i := 0; i < workerCount; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ref := range work {
					obj := b.fetchSingleResource(ctx, ref)
					if obj != nil {
						resultsMu.Lock()
						results = append(results, obj)
						resultsMu.Unlock()
					}
				}
			}()
		}

		// Queue work
		for _, ref := range strategy.mustFetch {
			work <- ref
		}
		close(work)
		wg.Wait()
	}

	// Add cached resources
	for _, ref := range strategy.canUseCached {
		if cached := b.cache.Get(ref.String()); cached != nil {
			results = append(results, cached)
		} else {
			// Cache miss - fetch it
			log.Log.V(1).Info("Cache miss, fetching", "ref", ref)
			if obj := b.fetchSingleResource(ctx, ref); obj != nil {
				results = append(results, obj)
			}
		}
	}

	// Sort by kind, namespace, name for consistent output
	sort.Slice(results, func(i, j int) bool {
		if results[i].GetKind() != results[j].GetKind() {
			return results[i].GetKind() < results[j].GetKind()
		}
		if results[i].GetNamespace() != results[j].GetNamespace() {
			return results[i].GetNamespace() < results[j].GetNamespace()
		}
		return results[i].GetName() < results[j].GetName()
	})

	return results
}

// fetchSingleResource fetches one resource with error handling
func (b *LiveDataBuilder) fetchSingleResource(
	ctx context.Context,
	ref object.ObjMetadata,
) *unstructured.Unstructured {
	// Get GVR from REST mapper
	gk := schema.GroupKind{
		Group: ref.GroupKind.Group,
		Kind:  ref.GroupKind.Kind,
	}
	mapping, err := b.restMapper.RESTMapping(gk)
	if err != nil {
		log.Log.V(1).Info("Cannot map resource", "ref", ref, "error", err)
		return nil
	}

	var obj *unstructured.Unstructured
	var fetchErr error

	// Fetch with timeout
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		obj, fetchErr = b.dynamicClient.Resource(mapping.Resource).
			Namespace(ref.Namespace).
			Get(fetchCtx, ref.Name, metav1.GetOptions{})
	} else {
		obj, fetchErr = b.dynamicClient.Resource(mapping.Resource).
			Get(fetchCtx, ref.Name, metav1.GetOptions{})
	}

	if fetchErr != nil {
		if !errors.IsNotFound(fetchErr) {
			log.Log.Error(fetchErr, "Failed to fetch resource", "ref", ref)
		}
		return nil
	}

	// Clean the object (remove managed fields, status, etc.)
	Cleanup(obj)

	return obj
}

// updateCache updates cache with fetched resources
func (b *LiveDataBuilder) updateCache(
	liveObjects []*unstructured.Unstructured,
	processor *EventProcessor,
) {
	// Build set of recently changed objects
	recentlyChanged := make(map[string]bool)
	for _, ref := range processor.appliedObjects {
		recentlyChanged[ref.String()] = true
	}

	// Remove pruned/deleted from cache
	for _, ref := range processor.prunedObjects {
		b.cache.Remove(ref.String())
	}
	for _, ref := range processor.deletedObjects {
		b.cache.Remove(ref.String())
	}

	// Update cache with fetched objects (except recently changed ones)
	for _, obj := range liveObjects {
		gvk := obj.GroupVersionKind()
		ref := object.ObjMetadata{
			GroupKind: schema.GroupKind{
				Group: gvk.Group,
				Kind:  gvk.Kind,
			},
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}

		// Only cache if not recently changed
		if !recentlyChanged[ref.String()] {
			b.cache.Put(ref.String(), obj)
		}
	}
}

// ResourceCache methods

// IsValid checks if a cached resource is still valid
func (c *ResourceCache) IsValid(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if cached, exists := c.resources[key]; exists {
		return time.Since(cached.fetchedAt) < c.maxAge
	}
	return false
}

// Get retrieves a cached resource
func (c *ResourceCache) Get(key string) *unstructured.Unstructured {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if cached, exists := c.resources[key]; exists {
		if time.Since(cached.fetchedAt) < c.maxAge {
			return cached.object.DeepCopy()
		}
		// Expired - will be removed on next cleanup
	}
	return nil
}

// Put stores a resource in cache
func (c *ResourceCache) Put(key string, obj *unstructured.Unstructured) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Simple LRU: if at max size, remove oldest
	if len(c.resources) >= c.maxSize {
		c.evictOldest()
	}

	c.resources[key] = CachedResource{
		object:          obj.DeepCopy(),
		fetchedAt:       time.Now(),
		resourceVersion: obj.GetResourceVersion(),
	}
}

// Remove deletes a resource from cache
func (c *ResourceCache) Remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.resources, key)
}

// evictOldest removes the oldest cached resource
func (c *ResourceCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, cached := range c.resources {
		if oldestKey == "" || cached.fetchedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = cached.fetchedAt
		}
	}

	if oldestKey != "" {
		delete(c.resources, oldestKey)
	}
}
