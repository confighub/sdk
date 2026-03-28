// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

// Binding represents a single needs/provides binding between two units.
// It records which provided attribute satisfies which needed attribute,
// and captures the original value at the time the binding was first created.
type Binding struct {
	// AttributeName is the shared attribute name that matched the need to the provide.
	AttributeName AttributeName `json:",omitempty" swaggertype:"string"`

	// DataType of the bound value.
	DataType DataType `json:",omitempty" swaggertype:"string"`

	// ProvidedResource identifies the resource in the upstream unit that provides the value.
	ProvidedResource ResourceInfo

	// ProvidedPath is the resolved path within the provided resource.
	ProvidedPath ResolvedPath `json:",omitempty" swaggertype:"string"`

	// InLiveState indicates whether the provided value comes from the upstream unit's LiveState
	// rather than its Data.
	InLiveState bool `json:",omitempty"`

	// NeededResource identifies the resource in the downstream unit that needs the value.
	NeededResource ResourceInfo

	// NeededPath is the resolved path within the needed resource.
	NeededPath ResolvedPath `json:",omitempty" swaggertype:"string"`

	// Expression is reserved for future use (e.g., CEL transformation expressions).
	Expression string `json:",omitempty"`

	// AutoUpdate indicates whether this binding should be automatically updated
	// when the provided value changes. If false, the binding is manual and will
	// not be modified by automatic resolution.
	AutoUpdate bool
}

// BindingList is a list of Binding entries stored on a Link.
type BindingList []Binding
