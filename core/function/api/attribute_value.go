// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import "github.com/google/uuid"

// ResolvedPath represents a concrete specific dot-separated path within a structured document (JSON, YAML document, etc.).
// Array indices are represented as integers within the path. A Kubernetes example is:
// "spec.template.spec.containers.0.image".
type ResolvedPath string

// UnresolvedPath represents a dot-separated path pattern within a structured document (JSON, YAML document, etc.).
// In addition to literal map keys and array indices, the following segment patterns are supported:
//
//   - .?mapKey:parameterName=value. represents an associative match within an array based on the value of the `mapKey`
//     attribute of a child of the array. This is common, for instance, in Kubernetes. It is replaced with the
//     matched array index. The `parameterName` is the name of the getter or setter parameter that corresponds
//     to the segment.
//
//   - .*?mapKey:parameterName. represents a wildcard match within an array or map where the value of the `mapKey`
//     attribute of a child is recorded for the named parameter of the getter or setter for the attribute.
//
//   - .*@:parameterName. represents a wildcard match within a map where the value of the map key for that
//     segment is recorded for the named parameter of the getter or setter for the attribute.
//
//   - .*. represents a wildcard for an array or map without recording any values for getter or setter parameters.
type UnresolvedPath string

// AttributeSelector identifies a value to extract from configuration data.
type AttributeSelector struct {
	WhereResource string         `json:",omitempty"`                      // where expression to select resource(s)
	Path          UnresolvedPath `json:",omitempty" swaggertype:"string"` // path expression
}

// AttributeName represents the category name of an attribute used for getter and setter functions, and for
// matching Provided values to Needed values. There are some well known attribute names that are used across
// resource/configuration providers, and some that are specific to each provider. The well known ones are
// specified here. For functions that use the path registry, they identify sets of related resource paths.
type AttributeName string

// Valid attribute names. By convention we use kabob-case to match cub's convention.
const AttributeNamePrefixRegexpString = "^[A-Za-z0-9]([\\-_A-Za-z0-9]{0,127})?"

const MaxAttributeNameLength = 128

const (
	// This is used to indicate that no path is registered in the path registry
	AttributeNameNone = AttributeName("none")

	// TODO: Convert these to attribute groups once they are registered under more specific attributes.
	AttributeNameDetail      = AttributeName("detail")
	AttributeNameDefaultName = AttributeName("default-name")

	// Attributes with registered paths
	AttributeNameResourceName            = AttributeName("resource-name")
	AttributeNameContainerName           = AttributeName("container-name")
	AttributeNameContainerImage          = AttributeName("container-image")
	AttributeNameContainerRepositoryURI  = AttributeName("container-repository-uri")
	AttributeNameContainerImageReference = AttributeName("container-image-reference")
	AttributeNameContainerFlag           = AttributeName("container-flag")
	AttributeNameHostname                = AttributeName("hostname")
	AttributeNameDomain                  = AttributeName("domain")
	AttributeNameSubdomain               = AttributeName("subdomain")
)

func AttributeNameForResourceType(resourceType ResourceType) AttributeName {
	return AttributeName(string(AttributeNameResourceName) + "/" + string(resourceType))
}

// AttributeVisitorDetails extends AttributeDetails with registration-time metadata
// used by visitor functions.
type AttributeVisitorDetails struct {
	AttributeDetails
	// TODO: expression fields are reserved for future use.
	// ExtractProvidedPropertiesExpression string `json:",omitempty" description:"Expression to extract ProvidedProperties from resource context; CEL expressions are prefixed with 'cel:'"`
	// ExtractNeededCriteriaExpression     string `json:",omitempty" description:"Expression to extract NeededRequired and NeededPreferred from resource context; CEL expressions are prefixed with 'cel:'"`
}

// AttributeDescriptor contains details about an attribute that is valid for all paths
// of the attribute. In the case of an attribute group, it contains the list of attributes
// the group expands to.
type AttributeDescriptor struct {
	AttributeName  AttributeName
	AttributeGroup []AttributeName // List of attributes to expand to, if non-empty
	AttributeVisitorDetails
}

type AttributeNameToAttributeDescriptor map[AttributeName]*AttributeDescriptor

// EmbeddedAccessorType specifies the type of format the embedded accessor can marshal
// and unmarshal in order to access embedded attributes.
type EmbeddedAccessorType string

const (
	EmbeddedAccessorRegexp = "Regexp"
	EmbeddedAccessorLine   = "Line"
	EmbeddedAccessorJSON   = "JSON"
	EmbeddedAccessorYAML   = "YAML"
)

// An AttributeIdentifier identifies the resource type and name and resolved path of the
// resource attribute.
type AttributeIdentifier struct {
	ResourceInfo
	Path        ResolvedPath `swaggertype:"string" description:"Path of the attribute"`
	InLiveState bool         `json:",omitempty" description:"True if a path in the live state, false if a path in the configuration data"`
}

// AttributeMetadata specifies the AttributeName, DataType, and other details, such as corresponding
// getter and setter functions for the attribute.
type AttributeMetadata struct {
	AttributeName AttributeName     `json:",omitempty" swaggertype:"string" description:"Name of the registered attribute"`
	DataType      DataType          `swaggertype:"string" description:"Data type if the attribute value."`
	Details       *AttributeDetails `json:",omitempty" description:"Additional attribute details"`
}

// AttributeInfo conveys both the identifying information about a resource attribute and its
// metadata.
type AttributeInfo struct {
	AttributeIdentifier
	AttributeMetadata
}

// Attribute details used for needs/provides matching.
type AttributeNeedsProvidesDetails struct {
	ProvidedProperties      map[string]string `json:",omitempty" description:"Key/value properties describing what this provided value offers, for matching"`
	NeededRequired          map[string]string `json:",omitempty" description:"Required properties that a provided value must have in order to match"`
	NeededPreferred         map[string]string `json:",omitempty" description:"Preferred properties for matching; more matches produce a stronger match preference"`
	BoundLinkID             uuid.UUID         `json:",omitempty,omitzero" description:"ID of the Link that bound this needed path to a provided value"`
	BoundProvidedProperties map[string]string `json:",omitempty" description:"ProvidedProperties of the provided path that was bound to this needed path, for cross-link score comparison"`
	IsNeeded                bool              `json:",omitempty" description:"Whether this attribute is a needed value"`
	IsProvided              bool              `json:",omitempty" description:"Whether this attribute is a provided value"`
}

// AttributeDetails provides the getter and (potentially multiple) setter functions for the
// resource attribute, and other information.
type AttributeDetails struct {
	GetterInvocation  *FunctionInvocation  `json:",omitempty" description:"Function invocation used to get the attribute, if any"`                        // used for matching
	SetterInvocations []FunctionInvocation `json:",omitempty" description:"Function invocation used to set the attribute (except for the value), if any"` // used for matching
	Description       string               `json:",omitempty" description:"Description of the attribute"`
	AttributeNeedsProvidesDetails
}

// DeepCopyStringMap returns a deep copy of a map[string]string, or nil if the input is nil.
func DeepCopyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// DeepCopyFunctionArguments returns a deep copy of a slice of FunctionArgument.
func DeepCopyFunctionArguments(args []FunctionArgument) []FunctionArgument {
	if args == nil {
		return nil
	}
	result := make([]FunctionArgument, len(args))
	copy(result, args)
	return result
}

// DeepCopyFunctionInvocation returns a deep copy of a FunctionInvocation.
func DeepCopyFunctionInvocation(fi FunctionInvocation) FunctionInvocation {
	return FunctionInvocation{
		FunctionName: fi.FunctionName,
		Arguments:    DeepCopyFunctionArguments(fi.Arguments),
	}
}

// DeepCopyAttributeNeedsProvidesDetails returns a deep copy of AttributeNeedsProvidesDetails.
func DeepCopyAttributeNeedsProvidesDetails(d AttributeNeedsProvidesDetails) AttributeNeedsProvidesDetails {
	return AttributeNeedsProvidesDetails{
		ProvidedProperties:      DeepCopyStringMap(d.ProvidedProperties),
		NeededRequired:          DeepCopyStringMap(d.NeededRequired),
		NeededPreferred:         DeepCopyStringMap(d.NeededPreferred),
		BoundLinkID:             d.BoundLinkID,
		BoundProvidedProperties: DeepCopyStringMap(d.BoundProvidedProperties),
		IsNeeded:                d.IsNeeded,
		IsProvided:              d.IsProvided,
	}
}

// DeepCopyAttributeDetails returns a deep copy of an AttributeDetails, or nil if the input is nil.
func DeepCopyAttributeDetails(d *AttributeDetails) *AttributeDetails {
	if d == nil {
		return nil
	}
	result := &AttributeDetails{
		Description:                   d.Description,
		AttributeNeedsProvidesDetails: DeepCopyAttributeNeedsProvidesDetails(d.AttributeNeedsProvidesDetails),
	}
	if d.GetterInvocation != nil {
		gi := DeepCopyFunctionInvocation(*d.GetterInvocation)
		result.GetterInvocation = &gi
	}
	if d.SetterInvocations != nil {
		result.SetterInvocations = make([]FunctionInvocation, len(d.SetterInvocations))
		for i, si := range d.SetterInvocations {
			result.SetterInvocations[i] = DeepCopyFunctionInvocation(si)
		}
	}
	return result
}

// DeepCopyAttributeInfo returns a deep copy of an AttributeInfo, including its Details.
func DeepCopyAttributeInfo(ai AttributeInfo) AttributeInfo {
	result := ai
	result.Details = DeepCopyAttributeDetails(ai.Details)
	return result
}

// Issue describes an issue found with a configuration attribute/path, such as a
// validation failure, policy violation, recommendation, etc.
type Issue struct {
	Identifier string `description:"Identifier for the kind of issue found, such as a number, alphanumeric code, or policy/rule name. Use especially with validation functions that check multiple policies/rules."`
	Message    string `description:"A more detailed and specific explanation of the issue found"`
}

// AttributeValue provides the value of an attribute in addition to information about the attribute.
type AttributeValue struct {
	AttributeInfo
	Value        any     `description:"Value of the attribute at the specified Path"`
	Comment      string  `json:",omitempty" description:"Line comment on the attribute at the specified Path"`
	Index        int     `description:"Index of the function invocation corresponding to the output. Useful in the case that multiple function invocations in the same executor call return AttributeValueList output."`
	FunctionName string  `json:",omitempty" description:"Name of the function invocation corresponding to the output"`
	Score        Score   `json:",omitempty" description:"Score of finding attributed to this Path"`
	Issues       []Issue `json:",omitempty" description:"Issues found with the attribute"`
}
type AttributeValueList []AttributeValue
