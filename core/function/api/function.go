// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package api implements the data types and messages exchanged by the ConfigHub
// function executor and its clients, in Go.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"strings"

	"github.com/confighub/sdk/core/third_party/yamlpatch"
	"github.com/confighub/sdk/core/workerapi"

	jsonschema "github.com/swaggest/jsonschema-go"

	"github.com/google/uuid"
)

// RevisionHash represents a crc32.ChecksumIEEE of configuration data.
// Deprecated: Use DataHash (SHA256) instead for new code.
// In Go, conversion of uint32 to int32 doesn't lose information. The
// 32 bits are retained. We use int32 because a number of languages and
// systems don't handle unsigned integers.
type RevisionHash int32

func HashConfigData(data []byte) RevisionHash {
	//nolint:gosec // negative numbers are fine, they just need to be unique
	return RevisionHash(crc32.ChecksumIEEE(data))
}

// DataHash represents a SHA256 hash of configuration data, encoded as a hexadecimal string.
// This is the same hash algorithm used by git and container images.
type DataHash string

// HashConfigDataSHA256 computes the SHA256 hash of configuration data and returns it as a hex string.
func HashConfigDataSHA256(data []byte) DataHash {
	hash := sha256.Sum256(data)
	return DataHash(hex.EncodeToString(hash[:]))
}

// EmptyDataHash is the SHA256 hash of an empty byte slice.
var EmptyDataHash = HashConfigDataSHA256(nil)

// IsEmptyDataHash returns true if the given hash represents empty configuration data.
func IsEmptyDataHash(hash DataHash) bool {
	return hash == "" || hash == EmptyDataHash
}

// ConfigDataHasChanged returns true if the given data differs from the previous data
// identified by the hashes in the FunctionContext. It prefers PreviousDataHash (SHA256)
// when available, falling back to PreviousContentHash (CRC32) for legacy units.
func ConfigDataHasChanged(functionContext *FunctionContext, data []byte) bool {
	if functionContext.PreviousDataHash != "" {
		return HashConfigDataSHA256(data) != functionContext.PreviousDataHash
	}
	return HashConfigData(data) != functionContext.PreviousContentHash
}

// The worker API ToolchainType identifies the configuration serialization format
// (YAML, TOML, HCL, etc.) and family of configuration entities and their schemas.
// Kubernetes is ToolchainKubernetesYAML.

var SupportedToolchains = map[workerapi.ToolchainType]string{
	workerapi.ToolchainConfigHubYAML:       "/confighub",
	workerapi.ToolchainKubernetesYAML:      "/kubernetes",
	workerapi.ToolchainAppConfigProperties: "/properties",
	workerapi.ToolchainAppConfigYAML:       "/yaml",
	workerapi.ToolchainAppConfigTOML:       "/toml",
	workerapi.ToolchainAppConfigINI:        "/ini",
	workerapi.ToolchainAppConfigJSON:       "/json",
	workerapi.ToolchainAppConfigEnv:        "/env",
	workerapi.ToolchainAppConfigText:       "/text",
	workerapi.ToolchainOpenTofuHCL:         "/opentofu",
}

// ToolchainTypeToDataType maps each supported ToolchainType to the DataType
// of its serialization format.
var ToolchainTypeToDataType = map[workerapi.ToolchainType]DataType{
	workerapi.ToolchainConfigHubYAML:       DataTypeYAML,
	workerapi.ToolchainKubernetesYAML:      DataTypeYAML,
	workerapi.ToolchainAppConfigProperties: DataTypeProperties,
	workerapi.ToolchainAppConfigYAML:       DataTypeYAML,
	workerapi.ToolchainAppConfigTOML:       DataTypeTOML,
	workerapi.ToolchainAppConfigINI:        DataTypeINI,
	workerapi.ToolchainAppConfigJSON:       DataTypeJSON,
	workerapi.ToolchainAppConfigEnv:        DataTypeEnv,
	workerapi.ToolchainAppConfigText:       DataTypeText,
	workerapi.ToolchainOpenTofuHCL:         DataTypeHCL,
}

func IsSupportedToolchain(toolchain workerapi.ToolchainType) bool {
	_, supported := SupportedToolchains[toolchain]
	return supported
}

func GetToolchainPath(toolchain workerapi.ToolchainType) string {
	return SupportedToolchains[toolchain]
}

func SupportedToolchainsToString() string {
	s := ""
	for toolchain := range SupportedToolchains {
		s += ", " + string(toolchain)
	}
	return strings.TrimPrefix(s, ", ")
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
	DataTypeUUID                    = DataType("uuid")
	DataTypeTime                    = DataType("time")
	DataTypeStringMap               = DataType("map[string]string")
	DataTypeStringBoolMap           = DataType("map[string]bool")
	DataTypeUUIDArray               = DataType("[]uuid")
	DataTypeUUIDStringMap           = DataType("map[uuid]string")
	DataTypeStringStringUUIDBoolMap = DataType("map[string]map[string]map[uuid]bool")

	// Structured data types
	DataTypeAttributeValueList   = DataType("AttributeValueList")
	DataTypeAttributeInfoList    = DataType("AttributeInfoList")
	DataTypePatchMap             = DataType("PatchMap")
	DataTypeResourceMutationList = DataType("ResourceMutationList")
	DataTypeResourceList         = DataType("ResourceList")
	DataTypeValueFilter          = DataType("ValueFilter")

	// Configuration format types
	DataTypeJSON       = DataType("JSON")
	DataTypeYAML       = DataType("YAML")
	DataTypeProperties = DataType("Properties")
	DataTypeTOML       = DataType("TOML")
	DataTypeINI        = DataType("INI")
	DataTypeEnv        = DataType("Env")
	DataTypeText       = DataType("Text")
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

const MaxResourceTypeLength = 128
const MaxResourceCategoryLength = MaxResourceTypeLength
const MaxResourceNameLength = 256

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

// AttributeSelector identifies a value to extract from configuration data.
type AttributeSelector struct {
	WhereResource string         `json:",omitempty"`                      // where expression to select resource(s)
	Path          UnresolvedPath `json:",omitempty" swaggertype:"string"` // path expression
}

// Valid function names. By convention we use kabob-case to match cub's convention.
const FunctionNamePrefixRegexpString = "^[A-Za-z0-9]([\\-_A-Za-z0-9]{0,127})?"

// Valid attribute names. By convention we use kabob-case to match cub's convention.
const AttributeNamePrefixRegexpString = "^[A-Za-z0-9]([\\-_A-Za-z0-9]{0,127})?"

// TODO: Validate these
const MaxFunctionNameLength = 128
const MaxNumFunctionArguments = 32
const MaxFunctionParameterNameLength = 128
const MaxFunctionDescriptionLength = 1024
const MaxAttributeNameLength = 128
const MaxDataTypeLength = 32
const MaxExpressionLength = 1024

// AttributeName represents the category name of an attribute used for getter and setter functions, and for
// matching Provided values to Needed values. There are some well known attribute names that are used across
// resource/configuration providers, and some that are specific to each provider. The well known ones are
// specified here. For functions that use the path registry, they identify sets of related resource paths.
type AttributeName string

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
	ToolchainType       workerapi.ToolchainType `description:"ToolchainType of the configuration data and function handlers"`
	UnitSlug            string                  `description:"Slug of the configuration Unit"`
	UnitID              uuid.UUID               `description:"Unique ID of the configuration Unit"`
	UnitLabels          map[string]string       `description:"Labels of the configuration Unit"`
	UnitAnnotations     map[string]string       `description:"Annotations of the configuration Unit"`
	SpaceID             uuid.UUID               `description:"ID of the Space of the configuration Unit"`
	SpaceSlug           string                  `description:"Slug of the Space of the configuration Unit"`
	SpaceLabels         map[string]string       `description:"Labels of the Space of the configuration Unit"`
	SpaceAnnotations    map[string]string       `description:"Annotations of the Space of the configuration Unit"`
	OrganizationID      uuid.UUID               `description:"ID of the Organization of the configuration Unit"`
	TargetID            uuid.UUID               `json:",omitempty" description:"ID of the Target where the function is executed; optional"`
	BridgeWorkerID      uuid.UUID               `json:",omitempty" description:"ID of the BridgeWorker that executes the function; optional; if not present, the function is executed by the Internal Function Executor"`
	RevisionID          uuid.UUID               `description:"Unique ID of the configuration Revision"`
	RevisionNum         int64                   `description:"Current/previous HeadRevisionNum of the configuration Unit"`
	QueuedOperationID   uuid.UUID               `description:"Unique ID of the operation generating the LiveState for the Unit"`
	NotLive             bool                    `description:"True if the configuration has never been applied or has been destroyed; not set for Revision or LiveState invocations"`
	IsLiveState         bool                    `description:"True if the ConfigData is the LiveState of the Unit"`
	PreviousContentHash RevisionHash            `description:"Deprecated: Use PreviousDataHash instead. crc32.ChecksumIEEE of the previous copy of the data, for determining whether it has been changed since it was last written"`
	PreviousDataHash    DataHash                `json:",omitempty" description:"SHA256 hash of the previous copy of the data, for determining whether it has been changed since it was last written"`
	ApprovedBy          []string                `description:"Usernames of users that have approved this revision of the configuration data"`
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

const (
	EvaluatorTemplate = "template"
	EvaluatorCEL      = "cel"
)

// FunctionArgument specifies the value of an argument in a function invocation and, optionally,
// its corresponding parameter name. If the parameter name is not specified for any argument,
// all of the arguments are expected to be passed in the same order as in the parameter list.
type FunctionArgument struct {
	ParameterName string `json:",omitempty" description:"Name of parameter corresponding to this argument; optional: if not specified, expected to be in order"`
	Value         any    `description:"Argument value; must be a Scalar type, currently string, int, or bool"`
	// DataType is not needed here because it's in the function signature
	Evaluator string `json:",omitempty" description:"Evaluate the provided Value with the specified Evaluator; supported values: template, cel"`
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
	ConfigData          []byte                 `swaggertype:"string" format:"byte" description:"Configuration data of the Unit to operate on"`
	NumFilters          int                    `description:"Number of validating functions to treat as filters: stop, but don't report errors"`
	StopOnError         bool                   `description:"If true, stop executing functions on the first error"`
	WhereResource       string                 `json:",omitempty" description:"If non-empty, restrict which resources functions operate on using ConfigHub metadata path expressions (ConfigHub.ResourceName, ConfigHub.ResourceNameWithoutScope, ConfigHub.ResourceType, ConfigHub.ResourceCategory)"`
	FunctionInvocations FunctionInvocationList `description:"List of functions to invoke and their arguments"`
}

// FunctionOptions contains options that affect how functions operate on resources.
type FunctionOptions struct {
	WhereResourceExpressions []*VisitorRelationalExpression
}

func GetWhereResourceExpressions(options *FunctionOptions) []*VisitorRelationalExpression {
	if options == nil {
		return nil
	}
	return options.WhereResourceExpressions
}

// ValidWhereResourcePaths lists the supported ConfigHub metadata paths for WhereResource filtering.
// Keep consistent with MatchesWhereResourceExpressions.
var ValidWhereResourcePaths = map[string]bool{
	"ConfigHub.ResourceName":             true,
	"ConfigHub.ResourceNameWithoutScope": true,
	"ConfigHub.ResourceType":             true,
	"ConfigHub.ResourceCategory":         true,
	"ConfigHub.ResourceMergeID":          true,
	"ConfigHub.ResourceNameStableCore":   true,
}

// ParseAndValidateWhereResource parses and validates a WhereResource filter string.
// It returns FunctionOptions with the parsed expressions, or nil if whereResource is empty.
// Paths with the "ConfigHub." prefix are validated against ValidWhereResourcePaths.
// Other paths are treated as resource field paths (e.g., "spec.replicas") and are resolved
// from the YAML document at evaluation time.
func ParseAndValidateWhereResource(whereResource string) (*FunctionOptions, error) {
	if whereResource == "" {
		return nil, nil
	}
	whereExpressions, err := ParseAndValidateWhereFilter(whereResource)
	if err != nil {
		return nil, fmt.Errorf("invalid WhereResource filter: %w", err)
	}
	for _, expr := range whereExpressions {
		if strings.HasPrefix(expr.Path, "ConfigHub.") && !ValidWhereResourcePaths[expr.Path] {
			return nil, fmt.Errorf("unsupported WhereResource path: %s; must be one of ConfigHub.ResourceName, ConfigHub.ResourceNameWithoutScope, ConfigHub.ResourceType, ConfigHub.ResourceCategory", expr.Path)
		}
	}
	return &FunctionOptions{WhereResourceExpressions: whereExpressions}, nil
}

// FunctionIDs contains the IDs related to a function invocation.
type FunctionIDs struct {
	OrganizationID uuid.UUID `description:"ID of the Unit's Organization"`
	SpaceID        uuid.UUID `description:"ID of the Unit's Space"`
	SpaceSlug      string    `json:",omitempty" description:"Slug of the Unit's Space"`
	UnitID         uuid.UUID `description:"ID of the Unit the configuration data is associated with"`
	UnitSlug       string    `json:",omitempty" description:"Slug of the Unit"`
	RevisionID     uuid.UUID `description:"ID of the Revision the configuration data is associated with"`
}

// FunctionInvocationSuccessResponse contains the data returned from a successful function invocation.
type FunctionInvocationSuccessResponse struct {
	ConfigData []byte                `swaggertype:"string" format:"byte" description:"The resulting configuration data, potentially mutated"`
	Outputs    map[OutputType][]byte `description:"Map of output types to their corresponding output data as embedded JSON"`
	Mutations  ResourceMutationList  `description:"List of mutations in the same order as the resources in ConfigData"`
	Mutators   []int                 `description:"List of function invocation indices that resulted in mutations"`
}

// A FunctionInvocationResponse is returned by the function executor in response to a
// FunctionInvocationRequest. It contains the potentially modified configuration data,
// any output produced by read-only and/or validation functions, whether the function
// sequence executed successfully, and any error messages returned.
// Output of compatible OutputTypes is combined, and otherwise the first output is
// returned. For instance, multiple AttributeValueLists will be appended
// together, and multiple ResourceInfoLists will be appended together.
type FunctionInvocationResponse struct {
	FunctionIDs
	FunctionInvocationSuccessResponse
	Success       bool     `description:"True if all functions executed successfully"`
	ErrorMessages []string `description:"Error messages from function execution; will be empty if Success is true"`
}

const MaxConfigDataLength = 64 * 1024 * 1024     // 64MB
const MaxFunctionOutputLength = 64 * 1024 * 1024 // 64MB
const MaxFunctionNumberOfErrors = 1024
const MaxFunctionErrorMessageLength = 1024

// ResourceInfo contains the ResourceName, ResourceNameWithoutScope, ResourceType, ResourceCategory, and ResourceMergeID for a configuration Element within a configuration Unit.
type ResourceInfo struct {
	ResourceName             ResourceName     `swaggertype:"string" description:"Name of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <metadata.namespace>/<metadata.name>; not all ToolchainTypes necessarily use '/' as a separator between any scope(s) and name or other client-chosen ID"`
	ResourceNameWithoutScope ResourceName     `swaggertype:"string" description:"Name of a resource in the system under management represented in the configuration data, without any uniquifying scope, such as Namespace, Project, Account, Region, etc.; Kubernetes resources are represented in the form <metadata.name>"`
	ResourceNameStableCore   ResourceName     `json:",omitempty" swaggertype:"string" description:"Name of a resource in the system under management represented in the configuration data with generated prefixes and suffixes stripped; empty if nothing to strip"`
	ResourceType             ResourceType     `swaggertype:"string" description:"Type of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <apiVersion>/<kind> (aka group-version-kind)"`
	ResourceCategory         ResourceCategory `json:",omitempty" swaggertype:"string" description:"Category of configuration element represented in the configuration data; Kubernetes and OpenTofu resources are of category Resource, and application configuration files are of category AppConfig"`
	ResourceMergeID          string           `json:",omitempty" swaggertype:"string" description:"Stable identifier (UUID) for a resource stored with the resource data that is intended to remain consistent across resource name and scope changes and across variants, used to match resources between config data documents when computing and patching mutations"`
}
type ResourceInfoList []ResourceInfo

func EffectiveResourceName(ri *ResourceInfo) ResourceName {
	if ri.ResourceNameStableCore != "" {
		return ri.ResourceNameStableCore
	}
	return ri.ResourceName
}

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
	Patches yamlpatch.Patch `description:"Isomorphic to a JSON patch, but to be applied to YAML"`
}

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

type Score string

const (
	ScoreCritical = Score("Critical")
	ScoreHigh     = Score("High")
	ScoreMedium   = Score("Medium")
	ScoreLow      = Score("Low")
	ScoreNone     = Score("")
)

var ScoreToNumber = map[Score]int{
	ScoreCritical: 4,
	ScoreHigh:     3,
	ScoreMedium:   2,
	ScoreLow:      1,
	ScoreNone:     0,
}

var NumberToScore = map[int]Score{
	4: ScoreCritical,
	3: ScoreHigh,
	2: ScoreMedium,
	1: ScoreLow,
	0: ScoreNone,
}

func ScoreMax(a, b Score) Score {
	anum := ScoreToNumber[a]
	bnum := ScoreToNumber[b]
	return NumberToScore[max(anum, bnum)]
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

// ValidationResult specifies whether a single validation function or sequence of validation
// functions passed for the given configuration Unit.
type ValidationResult struct {
	Passed           bool               `description:"True if valid, false otherwise"`
	Index            int                `description:"Index of the function invocation corresponding to the result. Useful in the case that multiple function invocations in the same executor call return ValidationResultList output."`
	FunctionName     string             `json:",omitempty" description:"Name of the function invocation corresponding to the result"`
	MaxScore         Score              `json:",omitempty" description:"Maximum score of all findings"`
	Details          []string           `json:",omitempty" description:"Deprecated. Use Issues or FailedAttributes instead. Optional list of failure details when not associated with specific attributes/paths."`
	Issues           []Issue            `json:",omitempty" description:"Issues found with the configuration unit that are not associated with specific attributes/paths. Use FailedAttributes where possible."`
	FailedAttributes AttributeValueList `json:",omitempty" description:"optional list of failed attributes/paths and issues found for them. Preferred over Issues and Details."`
}

type ValidationResultList []ValidationResult

var (
	ValidationResultTrue  = ValidationResult{Passed: true}
	ValidationResultFalse = ValidationResult{Passed: false}
)

// ValueFilter specifies allow and deny rules for value validation.
// For strings: AllowStrings/DenyStrings use exact match sets; AllowRegexp/DenyRegexp use regular expressions.
// AllowStrings and AllowRegexp are mutually exclusive. DenyStrings and DenyRegexp are mutually exclusive.
// For bools: AllowBool specifies the required boolean value (pointer; nil means any bool is allowed).
// For ints: Min and Max specify an inclusive range (pointer; nil means unbounded).
// If AllowStrings is non-empty, only values in the map are allowed.
// Values in DenyStrings are always rejected.
type ValueFilter struct {
	AllowStrings map[string]bool `json:",omitempty" description:"Set of allowed string values; if non-empty, only these values are permitted. Mutually exclusive with AllowRegexp."`
	DenyStrings  map[string]bool `json:",omitempty" description:"Set of denied string values; these values are always rejected. Mutually exclusive with DenyRegexp."`
	AllowRegexp  string          `json:",omitempty" description:"Regular expression for allowed string values; if non-empty, only matching values are permitted. Mutually exclusive with AllowStrings."`
	DenyRegexp   string          `json:",omitempty" description:"Regular expression for denied string values; matching values are always rejected. Mutually exclusive with DenyStrings."`
	AllowBool    *bool           `json:",omitempty" description:"Required boolean value; if set, only this boolean value is permitted"`
	Min          *int            `json:",omitempty" description:"Minimum allowed integer value (inclusive)"`
	Max          *int            `json:",omitempty" description:"Maximum allowed integer value (inclusive)"`
}

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
	Regexp     string             `json:",omitempty" description:"Regular expression matching valid values; applies to string parameters"`
	Min        *int               `json:",omitempty" description:"Minimum allowed value; applies to int parameters"`
	Max        *int               `json:",omitempty" description:"Maximum allowed value; applies to int parameters"`
	EnumValues []string           `json:",omitempty" description:"List of valid enum values; applies to enum parameters"`
	Schema     *jsonschema.Schema `json:",omitempty" description:"JSON schema (for embedded JSON values)"`
}

// FunctionOutput specifies the name and description of the result and its OutputType.
type FunctionOutput struct {
	ResultName  string             `description:"Name of the result in kabob-case"`
	Description string             `description:"Description of the result"`
	OutputType  OutputType         `swaggertype:"string" description:"Data type of the JSON embedded in the output"`
	Schema      *jsonschema.Schema `json:",omitempty" description:"JSON schema of the output type"`
}

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

// TODO: Add ResourceCategory

func ResourceTypeAndNameFromResourceInfo(resourceInfo ResourceInfo) ResourceTypeAndName {
	return ResourceTypeAndName(string(resourceInfo.ResourceType) + "#" + string(resourceInfo.ResourceNameWithoutScope))
}

// ResourceTypeAndFullNameFromResourceInfo returns ResourceType#ResourceName (including namespace/scope)
// Use this for tracking individual resource instances across scopes (e.g., ResourceStatusMap)
// For Kubernetes: returns "apiVersion/kind#namespace/name" (e.g., "apps/v1/Deployment#default/my-app")
func ResourceTypeAndFullNameFromResourceInfo(resourceInfo ResourceInfo) ResourceTypeAndName {
	return ResourceTypeAndName(string(resourceInfo.ResourceType) + "#" + string(resourceInfo.ResourceName))
}

func AttributeNameForResourceType(resourceType ResourceType) AttributeName {
	return AttributeName(string(AttributeNameResourceName) + "/" + string(resourceType))
}

// All types except int and bool are always serialized as strings.
func DataTypeIsSerializedAsString(dataType DataType) bool {
	switch dataType {
	case DataTypeInt, DataTypeBool:
		return false
	}
	return true
}

// Unmarshal well known output types.
func UnmarshalOutput(outputBytes []byte, outputType OutputType) (any, error) {
	switch outputType {
	case OutputTypeValidationResult, OutputTypeValidationResultList:
		var output ValidationResultList
		err := json.Unmarshal(outputBytes, &output)
		if err != nil {
			// Fallback to ValidationResult
			var output ValidationResult
			err := json.Unmarshal(outputBytes, &output)
			return output, err
		}
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
	case OutputTypeYAML:
		var output YAMLPayload
		err := json.Unmarshal(outputBytes, &output)
		return output, err
	default:
		var output any
		err := json.Unmarshal(outputBytes, &output)
		return output, err
	}
}

// Merge outputs from multiple function calls as a map by output type, combining lists of the same well known type.
func CombineOutputs(
	functionName string,
	instance string,
	newOutputType OutputType,
	outputs map[OutputType]any,
	newOutput any,
	functionInvocationIndex int,
	messages []string,
) (map[OutputType]any, []string) {
	if outputs == nil {
		outputs = make(map[OutputType]any)
	}

	// Get or initialize the output for this type
	output, exists := outputs[newOutputType]
	if !exists {
		// Initialize the output based on type
		switch newOutputType {
		case OutputTypeValidationResult, OutputTypeValidationResultList:
			output = ValidationResultList{}
		case OutputTypeAttributeValueList:
			output = AttributeValueList{}
		case OutputTypeResourceInfoList:
			output = ResourceInfoList{}
		case OutputTypeResourceList:
			output = ResourceList{}
		case OutputTypeYAML:
			output = YAMLPayload{}
		default:
			// includes OutputTypeJSON, etc.
			outputs[newOutputType] = newOutput
			return outputs, messages
		}
	}

	// Combine outputs of the same type
	switch newOutputType {
	case OutputTypeValidationResult:
		// Should be a ValidationResultList in actuality
		newResult, newExpectedType := newOutput.(ValidationResultList)
		if !newExpectedType {
			// Fall back to ValidationResult
			newResult, newExpectedType := newOutput.(ValidationResult)
			if !newExpectedType {
				messages = append(messages, "couldn't convert new result to ValidationResultList or ValidationResult")
				return outputs, messages
			}
			previousResults, previousExpectedType := output.(ValidationResultList)
			if !previousExpectedType {
				messages = append(messages, "couldn't convert previous result to ValidationResultList")
				return outputs, messages
			}
			newResult.Index = functionInvocationIndex
			newResult.FunctionName = functionName
			previousResults = append(previousResults, newResult)
			output = previousResults
		} else {
			previousResults, previousExpectedType := output.(ValidationResultList)
			if !previousExpectedType {
				messages = append(messages, "couldn't convert previous result to ValidationResultList")
				return outputs, messages
			}
			for i := range newResult {
				newResult[i].Index = functionInvocationIndex
				newResult[i].FunctionName = functionName
			}
			previousResults = append(previousResults, newResult...)
			output = previousResults
		}

	case OutputTypeValidationResultList:
		newResult, newExpectedType := newOutput.(ValidationResultList)
		if !newExpectedType {
			messages = append(messages, "couldn't convert new result to ValidationResult")
			return outputs, messages
		}
		for i := range newResult {
			newResult[i].Index = functionInvocationIndex
			newResult[i].FunctionName = functionName
		}
		previousResults, previousExpectedType := output.(ValidationResultList)
		if !previousExpectedType {
			messages = append(messages, "couldn't convert previous result to ValidationResultList")
			return outputs, messages
		}
		previousResults = append(previousResults, newResult...)
		output = previousResults

	case OutputTypeAttributeValueList:
		previousOutput, previousExpectedType := output.(AttributeValueList)
		if !previousExpectedType {
			messages = append(messages, "couldn't convert previous result to AttributeValueList")
			return outputs, messages
		}
		newOutput, newExpectedType := newOutput.(AttributeValueList)
		if !newExpectedType {
			messages = append(messages, "couldn't convert new result to AttributeValueList")
			return outputs, messages
		}
		for i := range newOutput {
			newOutput[i].Index = functionInvocationIndex
			newOutput[i].FunctionName = functionName
		}
		previousOutput = append(previousOutput, newOutput...)
		output = previousOutput

	case OutputTypeResourceInfoList:
		previousOutput, previousExpectedType := output.(ResourceInfoList)
		if !previousExpectedType {
			messages = append(messages, "couldn't convert previous result to ResourceInfoList")
			return outputs, messages
		}
		newOutput, newExpectedType := newOutput.(ResourceInfoList)
		if !newExpectedType {
			messages = append(messages, "couldn't convert new result to ResourceInfoList")
			return outputs, messages
		}
		previousOutput = append(previousOutput, newOutput...)
		output = previousOutput

	case OutputTypeResourceList:
		previousOutput, previousExpectedType := output.(ResourceList)
		if !previousExpectedType {
			messages = append(messages, "couldn't convert previous result to ResourceList")
			return outputs, messages
		}
		newOutput, newExpectedType := newOutput.(ResourceList)
		if !newExpectedType {
			messages = append(messages, "couldn't convert new result to ResourceList")
			return outputs, messages
		}
		previousOutput = append(previousOutput, newOutput...)
		output = previousOutput

	case OutputTypeYAML:
		previousOutput, previousExpectedType := output.(YAMLPayload)
		if !previousExpectedType {
			messages = append(messages, "couldn't convert previous result to YAMLPayload")
			return outputs, messages
		}
		newOutput, newExpectedType := newOutput.(YAMLPayload)
		if !newExpectedType {
			messages = append(messages, "couldn't convert new result to YAMLPayload")
			return outputs, messages
		}
		// Concatenate YAML documents
		if len(newOutput.Payload) > 0 {
			if len(previousOutput.Payload) == 0 {
				previousOutput.Payload = newOutput.Payload
			} else {
				payloads := []string{previousOutput.Payload, newOutput.Payload}
				previousOutput.Payload = strings.Join(payloads, "\n---\n")
			}
		}
		output = previousOutput

	default:
		messages = append(messages, fmt.Sprintf("functions with unmergeable output types; output of %s discarded", instance))
	}

	// Store the combined output back in the map
	outputs[newOutputType] = output
	return outputs, messages
}
