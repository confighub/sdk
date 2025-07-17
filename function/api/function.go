// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package api implements the data types and messages exchanged by the ConfigHub
// function executor and its clients, in Go.
package api

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"strings"

	"github.com/confighub/sdk/third_party/yamlpatch"
	"github.com/confighub/sdk/workerapi"

	"github.com/google/uuid"
)

// RevisionHash represents a crc32.ChecksumIEEE of configuration data.
// In Go, conversion of uint32 to int32 doesn't lose information. The
// 32 bits are retained. We use int32 because a number of languages and
// systems don't handle unsigned integers.
type RevisionHash int32

func HashConfigData(data []byte) RevisionHash {
	//nolint:gosec // negative numbers are fine, they just need to be unique
	return RevisionHash(crc32.ChecksumIEEE(data))
}

// The worker API ToolchainType identifies the configuration serialization format
// (YAML, TOML, HCL, etc.) and family of configuration entities and their schemas.
// Kubernetes is ToolchainKubernetesYAML.

var SupportedToolchains = map[workerapi.ToolchainType]string{
	workerapi.ToolchainKubernetesYAML:      "/kubernetes",
	workerapi.ToolchainAppConfigProperties: "/properties",
	workerapi.ToolchainOpenTofuHCL:         "/opentofu",
}

// TODO: Unify DataType and OutputType.

// DataType represents the data type of a function parameter or configuration attribute.
// The data type can be a scalar type (string, int, bool), a structured format (JSON, YAML),
// or a well known structured data type (e.g., AttributeValueList).
type DataType string

const (
	// Basic scalar types
	DataTypeNone   = DataType("")
	DataTypeString = DataType("string")
	DataTypeInt    = DataType("int")
	DataTypeBool   = DataType("bool")
	DataTypeEnum   = DataType("enum")

	// Additional Storage types
	DataTypeUUID          = DataType("uuid")
	DataTypeTime          = DataType("time")
	DataTypeStringMap     = DataType("map[string]string")
	DataTypeStringBoolMap = DataType("map[string]bool")
	DataTypeUUIDArray     = DataType("[]uuid")

	// Structured data types
	DataTypeAttributeValueList   = DataType("AttributeValueList")
	DataTypePatchMap             = DataType("PatchMap")
	DataTypeResourceMutationList = DataType("ResourceMutationList")
	DataTypeResourceList         = DataType("ResourceList")

	// Configuration format types
	DataTypeJSON       = DataType("JSON")
	DataTypeYAML       = DataType("YAML")
	DataTypeProperties = DataType("Properties")
	DataTypeTOML       = DataType("TOML")
	DataTypeINI        = DataType("INI")
	DataTypeEnv        = DataType("Env")
	DataTypeHCL        = DataType("HCL")
	DataTypeCEL        = DataType("CEL")
)

// OutputType represents the type of output produced by a function. It is either a well
// known structured type (e.g., AttributeValueList), a structured format (JSON), or
// Opaque (unparsed).
type OutputType string

const (
	OutputTypeValidationResult     = OutputType("ValidationResult")
	OutputTypeValidationResultList = OutputType("ValidationResultList")
	OutputTypeAttributeValueList   = OutputType("AttributeValueList")
	OutputTypeResourceInfoList     = OutputType("ResourceInfoList")
	OutputTypeResourceList         = OutputType("ResourceList")
	OutputTypePatchMap             = OutputType("PatchMap")
	OutputTypeCustomJSON           = OutputType("JSON")
	OutputTypeYAML                 = OutputType("YAML")
	OutputTypeOpaque               = OutputType("Opaque")
	OutputTypeResourceMutationList = OutputType("ResourceMutationList")
)

// ResourceCategory represents the category of syntactic construct represented.
// Each configuration toolchain defines its own set of valid resource categories,
// but there are some common categories, such as Resource and AppConfig.
type ResourceCategory string

const (
	ResourceCategoryInvalid     = ResourceCategory("Invalid")
	ResourceCategoryResource    = ResourceCategory("Resource")
	ResourceCategoryDyanmicData = ResourceCategory("DynamicData")
	ResourceCategoryAppConfig   = ResourceCategory("AppConfig")
)

// ResourceType represents a fully qualified resource type of a resource provider.
// Each resource provider defines its own set of valid resource types.
type ResourceType string

const (
	ResourceTypeAny = ResourceType("*")
)

// ResourceCategoryType is a tuple containing the ResourceCategory and ResourceType.
type ResourceCategoryType struct {
	ResourceCategory ResourceCategory
	ResourceType     ResourceType
}

// ResourceName represents a fully qualified resource name of a resource for a specific resource provider.
type ResourceName string

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

// AttributeName represents the category name of an attribute used for getter and setter functions, and for
// matching Provided values to Needed values. There are some well known attribute names that are used across
// resource/configuration providers, and some that are specific to each provider. The well known ones are
// specified here.
type AttributeName string

const (
	AttributeNameNone                    = AttributeName("")
	AttributeNameGeneral                 = AttributeName("attribute-value")
	AttributeNameNeededValue             = AttributeName("needed-value")
	AttributeNameProvidedValue           = AttributeName("provided-value")
	AttributeNameResourceName            = AttributeName("resource-name")
	AttributeNameContainerName           = AttributeName("container-name")
	AttributeNameContainerImage          = AttributeName("container-image")
	AttributeNameContainerImages         = AttributeName("container-images")
	AttributeNameContainerRepositoryURI  = AttributeName("container-repository-uri")
	AttributeNameContainerImageReference = AttributeName("container-image-reference")
	AttributeNameHostname                = AttributeName("hostname")
	AttributeNameDomain                  = AttributeName("domain")
	AttributeNameSubdomain               = AttributeName("subdomain")
	AttributeNameDetail                  = AttributeName("detail")
	AttributeNameDefaultName             = AttributeName("default-name")
)

// EmbeddedAccessorType specifies the type of format the embedded accessor can marshal
// and unmarshal in order to access embedded attributes.
type EmbeddedAccessorType string

const (
	EmbeddedAccessorRegexp = "Regexp"
	// EmbeddedAccessorJSON = "JSON"
	// EmbeddedAccessorYAML = "YAML"
)

// FunctionType represents the function's implementation pattern, if a common pattern.
// The type is Custom if it doesn't fit a common pattern.
type FunctionType string

const (
	FunctionTypePathVisitor = FunctionType("PathVisitor")
	FunctionTypeCustom      = FunctionType("Custom")
)

// Fields are documented using https://github.com/swaggest/jsonschema-go#field-tags

// The FunctionContext contains metadata about the configuration Unit provided as input to a
// function invocation sequence.
type FunctionContext struct {
	// ToolchainType is the ToolchainType of the configuration data and function handlers.
	ToolchainType workerapi.ToolchainType

	// UnitDisplayName is the display name of the configuration Unit.
	UnitDisplayName string

	// UnitSlug is the Slug of the configuration Unit.
	UnitSlug string

	// UnitID is the unique id of the configuration Unit.
	UnitID uuid.UUID

	// UnitLabels contains the labels of the configuration Unit.
	UnitLabels map[string]string

	// UnitAnnotations contains the annotations of the configuration Unit.
	UnitAnnotations map[string]string

	// SpaceID is the id of the Space of the configuration Unit.
	SpaceID uuid.UUID

	// SpaceSlug is the Slug of the Space of the configuration Unit.
	SpaceSlug string

	// OrganizationID is the id of the Organization of the configuration Unit.
	OrganizationID uuid.UUID

	// TargetID of the configuration Unit to determine the target where the function is executed.
	// This field is optional.
	TargetID uuid.UUID

	// BridgeWorkerID is the id of the BridgeWorker that executes the function.
	// This field is optional.
	// If not present, the function is executed by the Internal Function Executor.
	BridgeWorkerID uuid.UUID

	// RevisionID is the unique id of the configuration Revision.
	RevisionID uuid.UUID

	// RevisionNum is the current/previous HeadRevisionNum of the configuration Unit.
	RevisionNum int64

	// New is true if the configuration has never been applied (or has been destroyed).
	New bool

	// PreviousContentHash contains the crc32.ChecksumIEEE of the previous copy of the data,
	// for determining whether it has been changed since it was last written.
	PreviousContentHash RevisionHash

	// Usernames of users that have approved this revision of the configuration data.
	ApprovedBy []string
}

// InstanceString returns a string that uniquely identifies the configuration Unit and,
// if present, the RevisionID.
func (fc *FunctionContext) InstanceString() string {
	if fc.RevisionID == uuid.Nil {
		return fc.UnitID.String()
	}
	return strings.Join([]string{fc.UnitID.String(), fc.RevisionID.String()}, ": ")
}

type Scalar interface {
	~string | ~int | ~bool
}

// FunctionArgument specifies the value of an argument in a function invocation and, optionally,
// its corresponding parameter name. If the parameter name is not specified for any argument,
// all of the arguments are expected to be passed in the same order as in the parameter list.
type FunctionArgument struct {
	ParameterName string `json:",omitempty" description:"Name of parameter corresponding to this argument; optional: if not specified, expected to be in order"`
	Value         any    `description:"Argument value; must be a Scalar type, currently string, int, or bool"`
	// DataType is not needed here because it's in the function signature
}

// FunctionInvocation specifies the name of the function to invoke and the arguments to pass
// to the function. The function name must uniquely identify the function within its resource/configuration
// provider on its executor instance.
type FunctionInvocation struct {
	FunctionName string             `description:"Function name"`
	Arguments    []FunctionArgument `description:"Function arguments"`
}

type FunctionInvocationList []FunctionInvocation

// A FunctionInvocationRequest contains the configuration data of a configuration Unit, the function context
// for that configuration Unit, a sequence of functions to invoke and their arguments, and various
// options for the invocation.
type FunctionInvocationRequest struct {
	FunctionContext
	ConfigData               []byte                 `swaggertype:"string" format:"byte" description:"Configuration data of the Unit to operate on"`
	LiveState                []byte                 `swaggertype:"string" format:"byte" description:"The most recent live state of the Unit as reported by the bridge worker associated with the Target attached to the Unit."`
	CastStringArgsToScalars  bool                   `description:"If true, expect integer and boolean arguments to be passed as strings"`
	NumFilters               int                    `description:"Number of validating functions to treat as filters: stop, but don't report errors"`
	StopOnError              bool                   `description:"If true, stop executing functions on the first error"`
	CombineValidationResults bool                   `description:"If true, return a single ValidationResult for validating functions rather than a ValidationResultList"`
	FunctionInvocations      FunctionInvocationList `description:"List of functions to invoke and their arguments"`
}

// A FunctionInvocationResponse is returned by the function executor in response to a
// FunctionInvocationRequest. It contains the potentially modified configuration data,
// any output produced by read-only and/or validation functions, whether the function
// sequence executed successfully, and any error messages returned.
// Output of compatible OutputTypes is combined, and otherwise the first output is
// returned. For instance, a sequence of validation functions will have their outputs
// combined into a single ValidationResult, multiple AttributeValueLists will be appended
// together, and multiple ResourceInfoLists will be appended together.
type FunctionInvocationResponse struct {
	OrganizationID uuid.UUID            `description:"ID of the Unit's Organization"`
	SpaceID        uuid.UUID            `description:"ID of the Unit's Space"`
	UnitID         uuid.UUID            `description:"ID of the Unit the configuration data is associated with"`
	RevisionID     uuid.UUID            `description:"ID of the Revision the configuration data is associated with"`
	ConfigData     []byte               `swaggertype:"string" format:"byte" description:"The resulting configuration data, potentially mutated"`
	Output         []byte               `swaggertype:"string" format:"byte" description:"Output other than config data, as embedded JSON"`
	OutputType     OutputType           `swaggertype:"string" description:"Type of structured function output, if any"`
	Success        bool                 `description:"True if all functions executed successfully"`
	Mutations      ResourceMutationList `description:"List of mutations in the same order as the resources in ConfigData"`
	Mutators       []int                `description:"List of function invocation indices that resulted in mutations"`
	ErrorMessages  []string             `description:"Error messages from function execution; will be empty if Success is true"`
}

// ResourceInfo contains the ResourceName, ResourceNameWithoutScope, ResourceType, and ResourceCategory for a configuration Element within a configuration Unit.
type ResourceInfo struct {
	ResourceName             ResourceName     `swaggertype:"string" description:"Name of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <metadata.namespace>/<metadata.name>; not all ToolchainTypes necessarily use '/' as a separator between any scope(s) and name or other client-chosen ID"`
	ResourceNameWithoutScope ResourceName     `swaggertype:"string" description:"Name of a resource in the system under management represented in the configuration data, without any uniquifying scope, such as Namespace, Project, Account, Region, etc.; Kubernetes resources are represented in the form <metadata.name>"`
	ResourceType             ResourceType     `swaggertype:"string" description:"Type of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <apiVersion>/<kind> (aka group-version-kind)"`
	ResourceCategory         ResourceCategory `json:",omitempty" swaggertype:"string" description:"Category of configuration element represented in the configuration data; Kubernetes and OpenTofu resources are of category Resource, and application configuration files are of category AppConfig"`
}
type ResourceInfoList []ResourceInfo

// Resource contains the ResourceName, ResourceType, ResourceCategory, and Body for a configuration Element within a configuration Unit.
type Resource struct {
	ResourceInfo
	ResourceBody string `json:",omitempty" description:"Full configuration data of the resource"`
}
type ResourceList []Resource

type ResourceTypeAndName string

// PatchList is a list of patches applied to specified resources.
type PatchMap map[ResourceTypeAndName]ResourcePatch

type ResourcePatch struct {
	Patches yamlpatch.Patch
}

// An AttributeIdentifier identifies the resource type and name and resolved path of the
// resource attribute.
type AttributeIdentifier struct {
	ResourceInfo
	Path        ResolvedPath `swaggertype:"string"`
	InLiveState bool         `json:",omitempty"`
}

// AttributeMetadata specifies the AttributeName, DataType, and other details, such as corresponding
// getter and setter functions for the attribute.
type AttributeMetadata struct {
	AttributeName AttributeName     `json:",omitempty" swaggertype:"string"`
	DataType      DataType          `swaggertype:"string"`
	Info          *AttributeDetails `json:",omitempty"`
}

// AttributeInfo conveys both the identifying information about a resource attribute and its
// metadata.
type AttributeInfo struct {
	AttributeIdentifier
	AttributeMetadata
}

// AttributeDetails provides the getter and (potentially multiple) setter functions for the
// resource attribute, and other information.
type AttributeDetails struct {
	GetterInvocation   *FunctionInvocation  `json:",omitempty"` // used for matching
	SetterInvocations  []FunctionInvocation `json:",omitempty"` // used for matching
	GenerationTemplate string               `json:",omitempty"` // used for set-default-names
	Description        string               `json:",omitempty"` // used by UI
	// ValidationRegexp   string              `json:",omitempty"`   // not used yet
	// ExtractionRegexp   string              `json:",omitempty"`   // not used yet
	// PartitionRegexp    string              `json:",omitempty"`    // not used yet
}

// AttributeValue provides the value of an attribute in addition to information about the attribute.
type AttributeValue struct {
	AttributeInfo
	Value   any
	Comment string `json:",omitempty"`
}
type AttributeValueList []AttributeValue

// ValidationResult specifies whether a single validation function or sequence of validation
// functions passed for the given configuration Unit.
type ValidationResult struct {
	Passed           bool               // true if valid, false otherwise
	Index            int                // index of the function invocation corresponding to the result
	Details          []string           `json:",omitempty"` // optional list of failure details
	FailedAttributes AttributeValueList `json:",omitempty"` // optional list of failed attributes; preferred over Details
}

type ValidationResultList []ValidationResult

var (
	ValidationResultTrue  = ValidationResult{Passed: true}
	ValidationResultFalse = ValidationResult{Passed: false}
)

type YAMLPayload struct {
	Payload string
}

// FunctionSignature specifies the parameter names and values, required and optional parameters,
// OutputType, kind of function (mutating/readonly or validating), and description of the function.
type FunctionSignature struct {
	FunctionName          string              `description:"Name of the function in kabob-case"`
	Parameters            []FunctionParameter `description:"Function parameters, in order"`
	RequiredParameters    int                 `description:"Number of required parameters"`
	VarArgs               bool                `description:"Last parameter may be repeated"`
	OutputInfo            *FunctionOutput     `description:"Output description"`
	Mutating              bool                `description:"May change the configuration data"`
	Validating            bool                `description:"Returns ValidationResult"`
	Hermetic              bool                `description:"Does not call other systems"`
	Idempotent            bool                `description:"Will return the same result if invoked again"`
	Description           string              `description:"Description of the function"`
	FunctionType          FunctionType        `swaggertype:"string" description:"Implementation pattern of the function: PathVisitor or Custom"`
	AttributeName         AttributeName       `json:",omitempty" swaggertype:"string" description:"Attribute corresponding to registered paths, if a path visitor; optional"`
	AffectedResourceTypes []ResourceType      `json:",omitempty" description:"Resource types the function applies to; * if all"`
}

// FunctionParameter organizing metadata
// NOTE: I am aware of the similarity to OpenAPI and JSONSchema.

// FunctionParameter specifies the parameter name, description, required vs optional, data type, and example.
type FunctionParameter struct {
	ParameterName string   `description:"Name of the parameter in kabob-case"`
	Description   string   `description:"Description of the parameter"`
	Required      bool     `description:"Whether the parameter is required"`
	DataType      DataType `swaggertype:"string" description:"Data type of the parameter"`
	Example       string   `json:",omitempty" description:"Example value"`
	ValueConstraints
}

// ValueConstraints specifies constraints on a parameter's value.
type ValueConstraints struct {
	Regexp     string   `json:",omitempty" description:"Regular expression matching valid values; applies to string parameters"`
	Min        *int     `json:",omitempty" description:"Minimum allowed value; applies to int parameters"`
	Max        *int     `json:",omitempty" description:"Maximum allowed value; applies to int parameters"`
	EnumValues []string `json:",omitempty" description:"List of valid enum values; applies to enum parameters"`
}

// FunctionOutput specifies the name and description of the result and its OutputType.
type FunctionOutput struct {
	ResultName  string     `description:"Name of the result in kabob-case"`
	Description string     `description:"Description of the result"`
	OutputType  OutputType `swaggertype:"string" description:"Data type of the JSON embedded in the output"`
}

// PathVisitorInfo specifies the information needed by a visitor function to traverse the
// specified attributes within the registered resource types. The type is serializable as JSON
// for dynamic configuration and discovery.
type PathVisitorInfo struct {
	Path                   UnresolvedPath            `swaggertype:"string"`                   // unresolved path pattern
	ResolvedPath           ResolvedPath              `json:",omitempty" swaggertype:"string"` // specific resolved path
	AttributeName          AttributeName             `swaggertype:"string"`                   // AttributeName for the path
	DataType               DataType                  `swaggertype:"string"`                   // DataType of the attribute at the path
	Info                   *AttributeDetails         `json:",omitempty"`                      // additional attribute details
	TypeExceptions         map[ResourceType]struct{} `json:",omitempty"`                      // resource types to skip
	EmbeddedAccessorType   EmbeddedAccessorType      `json:",omitempty" swaggertype:"string"` // embedded accessor to use, if any
	EmbeddedAccessorConfig string                    `json:",omitempty"`                      // configuration of the embedded accessor, if any
}

// PathToVisitorInfoType associates attribute metadata with a resource path.
type PathToVisitorInfoType map[UnresolvedPath]*PathVisitorInfo

// ResourceTypeToPathToVisitorInfoType associates attribute path information with applicable
// resource types.
type ResourceTypeToPathToVisitorInfoType map[ResourceType]PathToVisitorInfoType

// AttributeNameToResourceTypeToPathToVisitorInfoType associates paths of resource types with an attribute
// attribute class for traversal/visitation by functions.
type AttributeNameToResourceTypeToPathToVisitorInfoType map[AttributeName]ResourceTypeToPathToVisitorInfoType

// TODO: Add ResourceCategory
func ResourceTypeAndNameFromResourceInfo(resourceInfo ResourceInfo) ResourceTypeAndName {
	return ResourceTypeAndName(string(resourceInfo.ResourceType) + "#" + string(resourceInfo.ResourceNameWithoutScope))
}

func AttributeNameForResourceType(resourceType ResourceType) AttributeName {
	return AttributeName(string(AttributeNameResourceName) + "/" + string(resourceType))
}

func DataTypeIsSerializedAsString(dataType DataType) bool {
	switch dataType {
	case DataTypeString,
		DataTypeEnum,
		DataTypeAttributeValueList,
		DataTypePatchMap,
		DataTypeResourceMutationList,
		DataTypeResourceList,
		DataTypeJSON, DataTypeYAML, DataTypeProperties, DataTypeTOML, DataTypeINI, DataTypeEnv, DataTypeHCL,
		DataTypeCEL:
		return true
	}
	return false
}

func UnmarshalOutput(outputBytes []byte, outputType OutputType) (any, error) {
	switch outputType {
	case OutputTypeValidationResult:
		var output ValidationResult
		err := json.Unmarshal(outputBytes, &output)
		return output, err
	case OutputTypeAttributeValueList:
		var output AttributeValueList
		err := json.Unmarshal(outputBytes, &output)
		return output, err
	case OutputTypeResourceInfoList:
		var output ResourceInfoList
		err := json.Unmarshal(outputBytes, &output)
		return output, err
	case OutputTypeResourceList:
		var output ResourceList
		err := json.Unmarshal(outputBytes, &output)
		return output, err
	default:
		return nil, fmt.Errorf("output type %s cannot be unmarshaled", string(outputType))
	}
}

func CombineOutputs(
	functionName string,
	instance string,
	outputType OutputType,
	newOutputType OutputType,
	output any,
	newOutput any,
	combineValidationResults bool,
	functionInvocationIndex int,
	messages []string,
) (any, []string) {
	if output == nil {
		outputType = newOutputType
		switch outputType {
		case OutputTypeValidationResult:
			if combineValidationResults {
				output = ValidationResult{
					Passed: true,
				}
			} else {
				output = ValidationResultList{}
			}
		case OutputTypeAttributeValueList:
			output = AttributeValueList{}
		case OutputTypeResourceInfoList:
			output = ResourceInfoList{}
		case OutputTypeResourceList:
			output = ResourceList{}
		default:
			output = newOutput
			return output, messages
		}
	}
	if outputType == newOutputType {
		switch outputType {
		case OutputTypeValidationResult:
			newResult, newExpectedType := newOutput.(ValidationResult)
			if !newExpectedType {
				messages = append(messages, "couldn't convert new result to ValidationResult")
				return output, messages
			}
			if !newResult.Passed {
				messages = append(messages, fmt.Sprintf("function failed: %s at %d on %s",
					functionName, functionInvocationIndex, instance))
			}
			if combineValidationResults {
				previousResult, previousExpectedType := output.(ValidationResult)
				if !previousExpectedType {
					messages = append(messages, "couldn't convert previous result to ValidationResult")
					return output, messages
				}
				newResult.Passed = newResult.Passed && previousResult.Passed
				newResult.Details = append(newResult.Details, previousResult.Details...)
				// Index is not set
				output = newResult
			} else {
				previousResults, previousExpectedType := output.(ValidationResultList)
				if !previousExpectedType {
					messages = append(messages, "couldn't convert previous result to ValidationResultList")
					return output, messages
				}
				newResult.Index = functionInvocationIndex
				previousResults = append(previousResults, newResult)
				output = previousResults
			}

		case OutputTypeAttributeValueList:
			previousOutput, previousExpectedType := output.(AttributeValueList)
			if !previousExpectedType {
				messages = append(messages, "couldn't convert previous result to AttributeValueList")
				return output, messages
			}
			newOutput, newExpectedType := newOutput.(AttributeValueList)
			if !newExpectedType {
				messages = append(messages, "couldn't convert new result to AttributeValueList")
				return output, messages
			}
			previousOutput = append(previousOutput, newOutput...)
			output = previousOutput

		case OutputTypeResourceInfoList:
			previousOutput, previousExpectedType := output.(ResourceInfoList)
			if !previousExpectedType {
				messages = append(messages, "couldn't convert previous result to ResourceInfoList")
				return output, messages
			}
			newOutput, newExpectedType := newOutput.(ResourceInfoList)
			if !newExpectedType {
				messages = append(messages, "couldn't convert new result to ResourceInfoList")
				return output, messages
			}
			previousOutput = append(previousOutput, newOutput...)
			output = previousOutput

		case OutputTypeResourceList:
			previousOutput, previousExpectedType := output.(ResourceList)
			if !previousExpectedType {
				messages = append(messages, "couldn't convert previous result to ResourceList")
				return output, messages
			}
			newOutput, newExpectedType := newOutput.(ResourceList)
			if !newExpectedType {
				messages = append(messages, "couldn't convert new result to ResourceList")
				return output, messages
			}
			previousOutput = append(previousOutput, newOutput...)
			output = previousOutput

		default:
			messages = append(messages, fmt.Sprintf("functions with unmergeable output types; output of %s discarded", instance))
		}
	} else {
		messages = append(messages, fmt.Sprintf("functions with incompatible output types; output of %s discarded", instance))
	}
	return output, messages
}
