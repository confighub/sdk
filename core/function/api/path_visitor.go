// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

// PathVisitorInfo specifies the information needed by a visitor function to traverse the
// specified attributes within the registered resource types. The type is serializable as JSON
// for dynamic configuration and discovery.
type PathVisitorInfo struct {
	Path                   UnresolvedPath            `swaggertype:"string" description:"Unresolved path pattern"`
	ResolvedPath           ResolvedPath              `json:",omitempty" swaggertype:"string" description:"Specific resolved path"`
	AttributeName          AttributeName             `swaggertype:"string" description:"AttributeName for the path"`
	DataType               DataType                  `swaggertype:"string" description:"DataType of the attribute at the path"`
	Details                *AttributeDetails         `json:",omitempty" description:"Additional attribute details"`
	TypeExceptions         map[ResourceType]struct{} `json:",omitempty" description:"Resource types to skip"`
	EmbeddedAccessorType   EmbeddedAccessorType      `json:",omitempty" swaggertype:"string" description:"Embedded accessor to use, if any"`
	EmbeddedAccessorConfig string                    `json:",omitempty" description:"Configuration of the embedded accessor, if any"`
	Enricher               any                       `json:"-" description:"AttributeEnricher function pointer for populating properties; not serialized"`
	// TypeExceptionFunc is a TypeExceptionPredicate deciding, per resource type, whether to skip
	// the path. It complements TypeExceptions for exceptions that are open-ended rather than a
	// fixed set — a rule over the API group, say, rather than an enumeration of every type it
	// matches. Enumeration does not scale to CRD families that ship thousands of types and add
	// more each release.
	//
	// Held as any and not serialized, following Enricher: it is only set by in-process path
	// registrations, so paths that arrive over the API (from an Attribute) carry TypeExceptions
	// alone.
	TypeExceptionFunc any `json:"-" description:"TypeExceptionPredicate deciding additional resource types to skip; not serialized"`
}

// TypeExceptionPredicate reports whether a path registered for ResourceTypeAny should be skipped
// for the given resource type. See PathVisitorInfo.TypeExceptionFunc.
type TypeExceptionPredicate func(ResourceType) bool

// SkipsResourceType reports whether the path is excepted for the given resource type, by either
// the fixed TypeExceptions set or the open-ended TypeExceptionFunc.
func (p *PathVisitorInfo) SkipsResourceType(resourceType ResourceType) bool {
	if p == nil {
		return false
	}
	if p.TypeExceptions != nil {
		if _, excepted := p.TypeExceptions[resourceType]; excepted {
			return true
		}
	}
	if fn, ok := p.TypeExceptionFunc.(TypeExceptionPredicate); ok && fn != nil {
		return fn(resourceType)
	}
	// Also accept a bare func literal, so callers need not convert explicitly.
	if fn, ok := p.TypeExceptionFunc.(func(ResourceType) bool); ok && fn != nil {
		return fn(resourceType)
	}
	return false
}

// PathToVisitorInfoType associates attribute metadata with a resource path.
type PathToVisitorInfoType map[UnresolvedPath]*PathVisitorInfo

// ResourceTypeToPathToVisitorInfoType associates attribute path information with applicable
// resource types.
type ResourceTypeToPathToVisitorInfoType map[ResourceType]PathToVisitorInfoType

// AttributeNameToResourceTypeToPathToVisitorInfoType associates paths of resource types with an attribute
// attribute class for traversal/visitation by functions.
type AttributeNameToResourceTypeToPathToVisitorInfoType map[AttributeName]ResourceTypeToPathToVisitorInfoType
