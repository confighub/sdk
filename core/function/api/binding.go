// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

// Binding represents a single needs/provides binding between two units.
// It records which provided attribute satisfies which needed attribute,
// and captures the original value at the time the binding was first created.
//
// A Link keeps the bindings someone stated, in ManualBindings, apart from the ones resolution
// finds and maintains, in Bindings, so the list a Binding is in says which kind it is.
type Binding struct {
	Key string `json:",omitempty" description:"Identifies a binding within its Link's ManualBindings, so that a merge of two versions of the Link matches bindings by Key rather than by position. Optional, and unique within the list when present. Letters, digits, '-' and '_', starting with a letter or digit; at most 128 characters."`

	AttributeName AttributeName `json:",omitempty" swaggertype:"string" description:"Shared attribute name that matched the need to the provide"`

	DataType DataType `json:",omitempty" swaggertype:"string" description:"DataType of the bound value"`

	ProvidedResource ResourceInfo `description:"Resource in the upstream unit that provides the value"`

	ProvidedPath ResolvedPath `json:",omitempty" swaggertype:"string" description:"Resolved path within the provided resource"`

	NeededResource ResourceInfo `description:"Resource in the downstream unit that needs the value"`

	NeededPath ResolvedPath `json:",omitempty" swaggertype:"string" description:"Resolved path within the needed resource"`
}

// BindingList is a list of Binding entries stored on a Link.
type BindingList []Binding
