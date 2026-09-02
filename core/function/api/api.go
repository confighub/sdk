// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
)

// Fields are documented using https://github.com/swaggest/jsonschema-go#field-tags

// The FunctionContext contains metadata about the configuration Unit provided as input to a
// function invocation sequence.
type FunctionContext struct {
	ToolchainType     workerapi.ToolchainType `description:"ToolchainType of the configuration data and function handlers"`
	UnitSlug          string                  `description:"Slug of the configuration Unit"`
	UnitID            uuid.UUID               `description:"Unique ID of the configuration Unit"`
	UnitLabels        map[string]string       `description:"Labels of the configuration Unit"`
	UnitAnnotations   map[string]string       `description:"Annotations of the configuration Unit"`
	SpaceID           uuid.UUID               `description:"ID of the Space of the configuration Unit"`
	SpaceSlug         string                  `description:"Slug of the Space of the configuration Unit"`
	SpaceLabels       map[string]string       `description:"Labels of the Space of the configuration Unit"`
	SpaceAnnotations  map[string]string       `description:"Annotations of the Space of the configuration Unit"`
	OrganizationID    uuid.UUID               `description:"ID of the Organization of the configuration Unit"`
	TargetID          uuid.UUID               `json:",omitempty" description:"ID of the Unit's Target; optional"`
	BridgeWorkerID    uuid.UUID               `json:",omitempty" description:"ID of the BridgeWorker that executes the function; optional; if not present, the function is executed by the Internal Function Executor"`
	TargetFacts       map[string]string       `json:",omitempty" description:"Facts of the Target where the function is executed; only populated when the Unit has a Target with non-empty Facts; optional"`
	RevisionID        uuid.UUID               `description:"Unique ID of the configuration Revision"`
	RevisionNum       int64                   `description:"Current/previous HeadRevisionNum of the configuration Unit"`
	QueuedOperationID uuid.UUID               `description:"Unique ID of the operation this function is executed under"`
	NotLive           bool                    `description:"True if the configuration has never been applied or has been destroyed; not set for Revision invocations"`
	PreviousDataHash  DataHash                `json:",omitempty" description:"SHA256 hash of the previous copy of the data, for determining whether it has been changed since it was last written"`
	ApprovedBy        []string                `description:"Usernames of users that have approved this revision of the configuration data"`

	// Conflicts are the Unit's outstanding merge conflicts — the parts of a merge's patch
	// that were not applied. Populated only when the invocation asks for them with
	// FunctionInvocationOptions.IncludeConflicts, and empty when the Unit has none, so an
	// ordinary invocation carries nothing extra. A validating function reads them to
	// decide whether an unresolved merge should block apply; an argument's template or CEL
	// expression can reach them as {{.Conflicts}} / functionContext.Conflicts.
	Conflicts MutationConflictList `json:",omitempty" description:"The Unit's outstanding merge conflicts; present only when the invocation asked for them"`
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

// MaxNumFunctionInvocations bounds a stored list of function invocations, such as an
// Invocation's FunctionInvocations.
const MaxNumFunctionInvocations = 32
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
	FunctionName  string             `description:"Function name"`
	Arguments     []FunctionArgument `description:"Function arguments"`
	WhereResource string             `json:",omitempty" description:"Per-invocation resource filter. AND-combined with the request-level WhereResource. Same path syntax as the request-level field (see ParseAndValidateWhereResource)."`
	// Params carries caller-supplied values for a stored Invocation's declared
	// parameters. They are the scope (.Params / params) against which templated
	// argument Values (Evaluator template/cel) are expanded at execution time.
	// Transient and per-invocation: not persisted on stored Invocations/Triggers
	// (bun:"-"), and never part of an Invocation's identity hash.
	Params map[string]any `json:",omitempty" bun:"-" description:"Caller-supplied parameter values for expanding templated argument Values; transient, not persisted"`
	// Clearance is the set of guarded reasons this invocation is cleared for. A path whose
	// guards it does not cover is not written, and the withheld change is reported.
	//
	// Per invocation, so a stored Invocation can carry the clearance for the reasons its own
	// function is meant to disturb, rather than every Trigger that runs it having to restate
	// them. An execution combines it with the clearance of whatever drove it -- the Trigger,
	// the Link, or the API call -- by union.
	Clearance Clearance `json:",omitempty" description:"Classes of guarded reason this invocation is cleared for; combined by union with the clearance of whatever drove the execution"`
	// Guards are the reasons this invocation states about the paths it writes, recorded on
	// the Unit's path annotations so a later operation has to be cleared for them.
	//
	// Per invocation, so a stored Invocation names the reason its own function is run for:
	// an invocation that maintains a field says so once, rather than every Trigger running it
	// restating it. An execution combines it with the guards of whatever drove it, later
	// winning per key.
	Guards GuardStamp `json:",omitempty" description:"Guards to record on the paths this invocation writes, so a later operation must be cleared for them; combined with the guards of whatever drove the execution, later winning per key"`
}

type OtherDataSource string

type FunctionInvocationList []FunctionInvocation

// A FunctionInvocationRequest contains the configuration data of a configuration Unit, the function context
// for that configuration Unit, a sequence of functions to invoke and their arguments, and various
// options for the invocation.
type FunctionInvocationRequest struct {
	FunctionContext
	ConfigData string                     `description:"Configuration data of the Unit to operate on"`
	OtherData  map[OtherDataSource]string `description:"Additional configuration data by source, such as from another revision (e.g., LastReleasedRevisionNum, Before:HeadRevisionNum). If provided, must be of the same ToolchainType as ConfigData. Changes are discarded."`
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

	// IncludeConflicts asks the server to put the Unit's outstanding merge conflicts into
	// the FunctionContext, where a function can read them directly or an argument's
	// template/CEL expression can reference them. Off by default: the conflicts are
	// unbounded in a way the rest of the context is not, and no ordinary invocation needs
	// them.
	IncludeConflicts bool `json:",omitempty" description:"Put the Unit's outstanding merge conflicts in the FunctionContext, for functions that reason about them"`
}

// FunctionOptions contains options that affect how functions operate on resources that need to be
// passed to individual function implementations.
type FunctionOptions struct {
	WhereResourceExpressions []*VisitorRelationalExpression

	// ResourceIndexes, when non-nil, restricts functions to the resources at these indexes
	// in the parsed configuration data, AND-ed with WhereResourceExpressions. A non-nil but
	// empty slice matches no resources. Indexes identify a resource independently of its
	// content, so a selection resolved before a mutation still designates the same resource
	// after it; set-attributes relies on that to scope an attribute to the resource it names
	// even when an earlier attribute in the same list renames that resource.
	ResourceIndexes []int

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

// GetResourceIndexes returns the resource index restriction in options, or nil if there
// is none. A nil return means all resources; an empty non-nil slice means none.
func GetResourceIndexes(options *FunctionOptions) []int {
	if options == nil {
		return nil
	}
	return options.ResourceIndexes
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
		return nil, errors.Wrap(err, "invalid WhereResource filter")
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
	TargetID       uuid.UUID `json:",omitempty" description:"ID of the Unit's Target; optional"`
}

// FunctionInvocationSuccessResponse contains the data returned from a successful function invocation.
type FunctionInvocationSuccessResponse struct {
	// ConfigData is present only when the invocation changed the configuration. Compare
	// DataHash against the hash of what was sent to tell "unchanged" from "now empty":
	// an absent ConfigData is not a statement on its own. ResultData does that comparison.
	ConfigData string `json:",omitempty" description:"The resulting configuration data; present only when the invocation changed it"`
	// DataHash is always populated, and is the SHA256 of the configuration the response
	// describes -- the data that was sent when nothing changed it, the new data when
	// something did. It is what makes omitting ConfigData unambiguous.
	DataHash        DataHash              `description:"SHA256 of the resulting configuration data, whether or not ConfigData is present"`
	Outputs         map[OutputType][]byte `description:"Map of output types to their corresponding output data as embedded JSON"`
	HasNewMutations bool                  `description:"Functions produced new mutations (of type other than None)"`
	Mutations       ResourceMutationList  `description:"List of mutations in the same order as the resources in ConfigData"`
	Mutators        []int                 `description:"List of function invocation indices that resulted in mutations"`
}

// ResultData returns the configuration the response describes, given the configuration the
// caller sent: what it sent when the invocation changed nothing, and the returned data when
// it did. Callers should use this rather than reading ConfigData directly, which is empty
// both when the data is unchanged and when it is genuinely empty.
func (r *FunctionInvocationSuccessResponse) ResultData(sent string) string {
	if r.DataHash != "" && r.DataHash == HashConfigDataSHA256(sent) {
		return sent
	}
	return r.ConfigData
}

// SetResultData records what a completed invocation produced, given the configuration it
// started from: the hash always, and the data itself only when it changed. It is the write
// side of ResultData, so the two sides of the contract sit together.
//
// includeUnchanged carries the configuration even when the invocation did not change it, for a
// caller that asked for it and is going to display the result. Omitting it is the right default
// at the worker boundary -- re-sending what was just sent is waste -- but it makes a read-only
// invocation, which by definition changes nothing, cost a second request to show its result.
// The flag belongs here rather than at the call site so that nothing sets ConfigData directly
// and the two sides of the contract stay the only place that decides.
func (r *FunctionInvocationSuccessResponse) SetResultData(from, result string, includeUnchanged bool) {
	r.DataHash = HashConfigDataSHA256(result)
	if includeUnchanged || result != from {
		r.ConfigData = result
	}
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
