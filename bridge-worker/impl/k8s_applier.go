// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/confighub/sdk/bridge-worker/api"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ErrOperationInterrupted is returned when an operation is cancelled or overridden.
// Status is already sent via sendOverriddenStatus/sendCancelledStatus, so no "Failed" status needed.
var ErrOperationInterrupted = errors.New("operation interrupted")

// ResourceSet represents a generic resource set interface
// This allows different applier implementations to provide their own ResourceSet types
type ResourceSet interface {
	// Entries returns a slice that can be used to get the count of changes
	GetEntries() []ResourceSetEntry
}

// ResourceSetEntry represents a single change in the resource set
type ResourceSetEntry interface {
	// Minimal interface for change set entries
	String() string
}

// SimpleResourceSet is a basic implementation of ResourceSet
type SimpleResourceSet struct {
	Entries []SimpleResourceSetEntry
}

func (s *SimpleResourceSet) GetEntries() []ResourceSetEntry {
	entries := make([]ResourceSetEntry, len(s.Entries))
	for i, entry := range s.Entries {
		entries[i] = entry
	}
	return entries
}

// SimpleResourceSetEntry is a basic implementation of ResourceSetEntry
type SimpleResourceSetEntry struct {
	Name      string
	Namespace string
	Kind      string
	Action    string
}

func (s SimpleResourceSetEntry) String() string {
	return fmt.Sprintf("%s/%s (%s): %s", s.Kind, s.Name, s.Namespace, s.Action)
}

// NewSimpleResourceSet creates a new SimpleResourceSet
func NewSimpleResourceSet() *SimpleResourceSet {
	return &SimpleResourceSet{
		Entries: make([]SimpleResourceSetEntry, 0),
	}
}

// Add adds an entry to the SimpleResourceSet
func (s *SimpleResourceSet) Add(entry SimpleResourceSetEntry) {
	s.Entries = append(s.Entries, entry)
}

// ApplyResult contains the result of an apply operation
type ApplyResult struct {
	ResourceSet      ResourceSet
	LiveObjects      []*unstructured.Unstructured
	LiveData         []byte // Updated LiveData including inventory
	LiveState        []byte
	ResourceStatuses api.ResourceStatusMap // Per-resource sync and readiness status
	Error            error
}

// WaitResult contains the result of a wait operation
type WaitResult struct {
	LiveObjects      []*unstructured.Unstructured
	ResourceSet      ResourceSet
	ResourceStatuses api.ResourceStatusMap // Per-resource sync and readiness status
	Error            error
}

// DestroyResult contains the result of a destroy operation
type DestroyResult struct {
	ResourceSet ResourceSet
	LiveData    []byte // Updated LiveData including inventory after destroy
	Error       error
}

// K8sApplier defines the interface for Kubernetes resource operations
// Note: Import is not part of this interface because it's a higher-level operation
// that reads live objects from the cluster based on discovery/filtering logic.
// Import belongs to the BridgeWorker interface as it operates at the orchestration
// level, while K8sApplier focuses on direct resource manipulation (apply/destroy/refresh).
type K8sApplier interface {
	// Apply applies the given objects to the cluster
	Apply(ctx context.Context, objects []*unstructured.Unstructured) ApplyResult

	// WaitForApply waits for applied resources to be ready
	WaitForApply(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult

	// Refresh retrieves the current live state of objects
	Refresh(ctx context.Context, objects []*unstructured.Unstructured) ([]*unstructured.Unstructured, error)

	// Destroy deletes the given objects from the cluster
	Destroy(ctx context.Context, objects []*unstructured.Unstructured) DestroyResult

	// WaitForDestroy waits for resources to be terminated and returns the final state
	WaitForDestroy(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult
}

// ApplierConfig contains configuration for creating an applier
type ApplierConfig struct {
	KubeContext string
	LiveData    []byte // LiveData containing inventory and resources
	SpaceID     string // SpaceID for inventory identification
	UnitSlug    string // UnitSlug for inventory identification
	RevisionNum int64  // RevisionNum for the revision being applied
	WaitTimeout string // WaitTimeout duration string for resource readiness
}

type ApplierName string

const (
	FluxSSA     ApplierName = "FluxSSA"
	CLIUtilsSSA ApplierName = "CLIUtilsSSA"
)

// k8sApplierFactory is a variable that can be replaced in tests
var k8sApplierFactory = defaultK8sApplierFactory

func defaultK8sApplierFactory(name ApplierName, config ApplierConfig) (K8sApplier, error) {
	switch name {
	case FluxSSA:
		return NewFluxSSAApplier(config)
	case CLIUtilsSSA:
		return NewCLIUtilsApplier(config)
	}
	return nil, fmt.Errorf("unknown applier %s", name)
}

func NewK8sApplier(name ApplierName, config ApplierConfig) (K8sApplier, error) {
	return k8sApplierFactory(name, config)
}
