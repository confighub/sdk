// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

// Binding represents a single needs/provides binding between two units.
// It records which provided attribute satisfies which needed attribute,
// and captures the original value at the time the binding was first created.
type Binding struct {
	AttributeName AttributeName `json:",omitempty" swaggertype:"string" description:"Shared attribute name that matched the need to the provide"`

	DataType DataType `json:",omitempty" swaggertype:"string" description:"DataType of the bound value"`

	ProvidedResource ResourceInfo `description:"Resource in the upstream unit that provides the value"`

	ProvidedPath ResolvedPath `json:",omitempty" swaggertype:"string" description:"Resolved path within the provided resource"`

	InLiveState bool `json:",omitempty" description:"Whether the provided value comes from the upstream unit's LiveState rather than its Data"`

	NeededResource ResourceInfo `description:"Resource in the downstream unit that needs the value"`

	NeededPath ResolvedPath `json:",omitempty" swaggertype:"string" description:"Resolved path within the needed resource"`

	AutoUpdate bool `description:"Whether this binding should be automatically updated when the provided value changes; if false, the binding is manual and will not be modified by automatic resolution"`
}

// BindingList is a list of Binding entries stored on a Link.
type BindingList []Binding
