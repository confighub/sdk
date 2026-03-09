// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"github.com/fluxcd/pkg/ssa"
)

// SSAResourceSetWrapper wraps an SSA ChangeSet to implement our generic ResourceSet interface
type SSAResourceSetWrapper struct {
	changeSet *ssa.ChangeSet
}

// NewSSAResourceSetWrapper creates a wrapper for SSA ChangeSet
func NewSSAResourceSetWrapper(changeSet *ssa.ChangeSet) *SSAResourceSetWrapper {
	return &SSAResourceSetWrapper{changeSet: changeSet}
}

// GetEntries implements the ResourceSet interface for SSA ChangeSet
func (w *SSAResourceSetWrapper) GetEntries() []ResourceSetEntry {
	if w.changeSet == nil {
		return make([]ResourceSetEntry, 0)
	}

	entries := make([]ResourceSetEntry, len(w.changeSet.Entries))
	for i, entry := range w.changeSet.Entries {
		entries[i] = &SSAResourceSetEntryWrapper{entry: entry}
	}
	return entries
}

// SSAResourceSetEntryWrapper wraps an SSA ChangeSetEntry
type SSAResourceSetEntryWrapper struct {
	entry ssa.ChangeSetEntry
}

// String implements the ResourceSetEntry interface
func (w *SSAResourceSetEntryWrapper) String() string {
	return w.entry.String()
}
