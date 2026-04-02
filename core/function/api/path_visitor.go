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
}

// PathToVisitorInfoType associates attribute metadata with a resource path.
type PathToVisitorInfoType map[UnresolvedPath]*PathVisitorInfo

// ResourceTypeToPathToVisitorInfoType associates attribute path information with applicable
// resource types.
type ResourceTypeToPathToVisitorInfoType map[ResourceType]PathToVisitorInfoType

// AttributeNameToResourceTypeToPathToVisitorInfoType associates paths of resource types with an attribute
// attribute class for traversal/visitation by functions.
type AttributeNameToResourceTypeToPathToVisitorInfoType map[AttributeName]ResourceTypeToPathToVisitorInfoType
