// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
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

// Valid function names. By convention we use kabob-case to match cub's convention.
const FunctionNamePrefixRegexpString = "^[A-Za-z0-9]([\\-_A-Za-z0-9]{0,127})?"

// TODO: Validate these
const MaxFunctionNameLength = 128
const MaxNumFunctionArguments = 32
const MaxFunctionParameterNameLength = 128
const MaxFunctionDescriptionLength = 1024

// FunctionType represents the function's implementation pattern, if a common pattern.
// The type is Custom if it doesn't fit a common pattern.
type FunctionType string

const (
	FunctionTypePathVisitor = FunctionType("PathVisitor")
	FunctionTypeCustom      = FunctionType("Custom")
)

// FunctionInvocation specifies the name of the function to invoke and the arguments to pass
// to the function. The function name must uniquely identify the function within its resource/configuration
// provider on its executor instance.
type FunctionInvocation struct {
	FunctionName string             `description:"Function name"`
	Arguments    []FunctionArgument `description:"Function arguments"`
}

type OtherDataSource string

type FunctionInvocationList []FunctionInvocation

// A FunctionInvocationRequest contains the configuration data of a configuration Unit, the function context
// for that configuration Unit, a sequence of functions to invoke and their arguments, and various
// options for the invocation.
type FunctionInvocationRequest struct {
	FunctionContext
	ConfigData []byte                     `swaggertype:"string" format:"byte" description:"Configuration data of the Unit to operate on"`
	OtherData  map[OtherDataSource][]byte `swaggertype:"string" format:"byte" description:"Additional configuration data by source, such as from another revision (e.g., LiveRevisionNum, Before:HeadRevisionNum). If provided, must be of the same ToolchainType as ConfigData. Changes are discarded."`
	FunctionInvocationOptions
	FunctionInvocations FunctionInvocationList `description:"List of functions to invoke and their arguments"`
}

// FunctionInvocationOptions contains function execution options passed in FunctionInvocationRequest.
type FunctionInvocationOptions struct {
	// Options that apply to sequences of function invocations.
	NumFilters  int  `description:"Number of validating functions to treat as filters: stop, but don't report errors"`
	StopOnError bool `description:"If true, stop executing functions on the first error"`

	// Options passed to individual function implementations, translated to FunctionOptions.
	WhereResource string `json:",omitempty" description:"If non-empty, restrict which resources functions operate on using ConfigHub metadata path expressions (ConfigHub.ResourceName, ConfigHub.ResourceNameWithoutScope, ConfigHub.ResourceType, ConfigHub.ResourceCategory)"`
}

// FunctionOptions contains options that affect how functions operate on resources that need to be
// passed to individual function implementations.
type FunctionOptions struct {
	WhereResourceExpressions []*VisitorRelationalExpression

	// If true, include full details in function output. For now this is a per-function property
	// that is passed to the visitor implementation.
	IncludeDetails bool
}

func NewFunctionOptions() *FunctionOptions {
	return &FunctionOptions{}
}

func GetWhereResourceExpressions(options *FunctionOptions) []*VisitorRelationalExpression {
	if options == nil {
		return nil
	}
	return options.WhereResourceExpressions
}

func GetIncludeDetails(options *FunctionOptions) bool {
	if options == nil {
		return false
	}
	return options.IncludeDetails
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

const MaxExpressionLength = 1024

// ParseAndValidateWhereResource parses and validates a WhereResource filter string.
// It returns the parsed expressions, or nil if whereResource is empty.
// Paths with the "ConfigHub." prefix are validated against ValidWhereResourcePaths.
// Other paths are treated as resource field paths (e.g., "spec.replicas") and are resolved
// from the YAML document at evaluation time.
func ParseAndValidateWhereResource(whereResource string) ([]*VisitorRelationalExpression, error) {
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
	return whereExpressions, nil
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
	ConfigData      []byte                `swaggertype:"string" format:"byte" description:"The resulting configuration data, potentially mutated"`
	Outputs         map[OutputType][]byte `description:"Map of output types to their corresponding output data as embedded JSON"`
	HasNewMutations bool                  `description:"Functions produced new mutations (of type other than None)"`
	Mutations       ResourceMutationList  `description:"List of mutations in the same order as the resources in ConfigData"`
	Mutators        []int                 `description:"List of function invocation indices that resulted in mutations"`
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
