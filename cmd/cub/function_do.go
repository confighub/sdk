// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var functionDoCmd = &cobra.Command{
	Use:         "do <function> [<arg1> ...]",
	Short:       "Invoke one function",
	Long:        getFunctionDoHelp(),
	Args:        cobra.MinimumNArgs(0),
	Aliases:     []string{"invoke"},
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        functionDoCommandRun,
}

func getFunctionDoHelp() string {
	baseHelp := `Invoke a function on units in a space, or across all spaces if the selected space is "*".

This is the mixed escape hatch: it accepts any kind of function. Prefer the
verb that matches the kind -- cub function set for mutating functions, cub
function get for non-mutating ones, cub function vet for validating ones. Those
reject a function of the wrong kind before invoking anything, so a misremembered
name is an error rather than an unintended write. Reach for do only when a
single command must invoke functions of more than one kind.

Function argument values may be simply listed in order so long as no optional parameters are skipped.
Parameters may be specified out of order using the "--parameter-name=value" syntax.  These are not cub
flags, so specify "--" before the function name if using that syntax for function arguments.

To display a list of supported functions for each ToolchainType, run:
` + "```" + `
  cub function list
` + "```" + `

To display usage details of a specific function, run:
` + "```" + `

  cub function explain --toolchain TOOLCHAIN_TYPE FUNCTION_NAME
` + "```" + `

Example Functions:

  - set-container-image: Update container image in a deployment
  - set-int-path: Set an integer value at a specific path in the configuration
  - get-replicas: Get the number of replicas for deployments
  - set-replicas: Set the number of replicas for deployments
  - get-yq: Read a value with a yq expression
  - where-filter: Filter units based on a condition
  - vet-cel: Validate resources using CEL expressions

Examples:
` + "```" + `
  # Use set-container-image to update container image in a deployment
  cub function do \
    --space my-space \
    --where "Slug = 'my-deployment'" \
    set-container-image nginx nginx:mainline-otel \
    --wait

  # Read the container image with get-yq. Non-mutating, so cub function get
  # would be the better verb; do accepts it too.
  cub function do \
    --space my-space \
    --where "Slug = 'my-deployment'" \
    --show output \
    get-yq '.spec.template.spec.containers[0].image'

  # Use set-int-path to update replica count for a deployment
  # There's also set-replicas function to do the same
  cub function do \
    --space my-space \
    --where "Slug = 'headlamp'" \
    set-int-path apps/v1/Deployment spec.replicas 2 \
    --wait

  # Get replica counts
  cub function do \
    --space my-space \
    get-replicas \
    --quiet \
    --show output -o jq='.[].Value'

  # Filter deployments with more than 1 replica
  cub function do \
    --space my-space \
    where-filter apps/v1/Deployment 'spec.replicas > 1' \
    --quiet \
    --show output -o jq='. as $e | .Output[] | select(.Passed == true) | {space: $e.SpaceSlug, unit: $e.UnitSlug}'

  # Validate deployment replicas using CEL
  cub function do --space my-space \
    vet-cel 'r.kind != "Deployment" || r.spec.replicas > 1' \
    --quiet \
    --show output -o jq='. as $e | .Output[] | select(.Passed == true) | {space: $e.SpaceSlug, unit: $e.UnitSlug}'
` + "```" + `
`

	agentContext := `Agent-specific guidance:

Prerequisites:
- Authentication: Run 'cub auth login' first
- Space context: Set with 'cub context set --space SPACE_SLUG' or use --space flag
- Discovery: Use 'cub function list' to see available functions for each toolchain

Common agent workflows. Each uses the verb for the function's kind rather than
do, which is what an agent should emit unless one command must mix kinds:

1. Inspect configuration before making changes:
   cub function get --space SPACE --unit UNIT get-placeholders
   cub function get --space SPACE --unit UNIT get-yq '.spec.replicas'

2. Modify configuration:
   cub function set --space SPACE --unit UNIT -o mutations \
     --change-desc "Bump nginx to 1.25-alpine" \
     set-container-image nginx nginx:1.25-alpine --wait

3. Validate after changes:
   cub function vet --space SPACE --unit UNIT vet-placeholders
   cub function vet --space SPACE --unit UNIT vet-cel 'r.spec.replicas > 0'

Key flags for agents:
- --unit: Target units by slug or UUID (repeatable or comma-separated); shorter
  than --where "Slug = '...'" when the space is pinned
- --where: Filter units (use quotes around expressions)
- --space: Target space ("*" for all spaces where supported)
- --show output: Show just function output, not execution details
- --quiet: Suppress status messages
- --wait: Wait for async triggers to complete
- --change-desc: Record why a mutating invocation was made; it becomes the revision description
- -o mutations: Show the per-path diff, with or without --dry-run
- --protect: Record the paths written as protected local overrides that a later
  merge from upstream must not overwrite. Leave it off when applying a value
  decided elsewhere, such as propagating a release.

Error handling:
- Functions return Success: true/false in response
- Use --quiet --show output -o jq=to extract specific values
- Check function signatures with 'cub function explain FUNCTION_NAME'`

	return getCommandHelp(baseHelp, agentContext)
}

var outputOnly bool
var outputValuesOnly bool
var outputRaw bool
var dataOnly bool
var outputJQ string
var unitIdentifiers []string
var dryRun bool
var functionTriggerIdentifiers []string
var functionInvocationIdentifiers []string
var updateApplyGates bool
var functionIncludeConflicts bool
var revisionIdentifier string
var functionChangesetSlug string
var functionToolchainType string
var functionOtherDataSource string

// Set by `cub invocation get/set/vet` to execute a single parameterized
// Invocation: the identifier of the Invocation and the values supplied for its
// declared parameters. Empty for `cub function do`.
var parameterizedInvocationIdentifier string
var parameterizedInvocationParams map[string]any

func init() {
	functionDoCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the function")
	enableShowFlag(functionDoCmd)
	functionDoCmd.Flags().BoolVar(&outputOnly, "output-only", false, "show output without other response details")
	_ = functionDoCmd.Flags().MarkDeprecated("output-only", "use --show output")
	functionDoCmd.Flags().BoolVar(&outputRaw, "output-json", false, "show output as raw JSON")
	_ = functionDoCmd.Flags().MarkDeprecated("output-json", "use --show output -o json")
	functionDoCmd.Flags().StringVar(&outputJQ, "output-jq", "", "apply jq to output JSON")
	_ = functionDoCmd.Flags().MarkDeprecated("output-jq", "use --show output -o jq=<expr>")
	functionDoCmd.Flags().BoolVar(&outputValuesOnly, "output-values-only", false, "show output values (from functions returning AttributeValueList) without other response details")
	_ = functionDoCmd.Flags().MarkDeprecated("output-values-only", "use --show values")
	functionDoCmd.Flags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	_ = functionDoCmd.Flags().MarkDeprecated("data-only", "use --show data")
	enableOutputFileFlag(functionDoCmd)
	// Same flag as unit update
	functionDoCmd.Flags().StringVar(&changeDescription, "change-desc", "", "change description")
	functionDoCmd.Flags().StringVar(&functionChangesetSlug, "changeset", "", "changeset to associate units with")
	functionDoCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	functionDoCmd.Flags().StringVar(&revisionIdentifier, "revision", "", "target a specific revision (format: unit-slug/revision-number, e.g. mydeployment/3)")
	functionDoCmd.Flags().BoolVar(&dryRun, "dry-run", false, "dry run mode: execute functions but skip updating configuration data")
	addClearanceFlag(functionDoCmd)
	addGuardFlag(functionDoCmd)
	functionDoCmd.Flags().BoolVar(&protectChange, "protect", false, "record the paths this change writes as protected local overrides, so a later merge from upstream does not overwrite them; by default a change claims nothing and each path keeps the protection it already has")
	functionDoCmd.Flags().StringSliceVar(&functionTriggerIdentifiers, "trigger", []string{}, "execute triggers by UUID, slug, or space/slug (can be repeated or comma-separated)")
	functionDoCmd.Flags().StringSliceVar(&functionInvocationIdentifiers, "invocation", []string{}, "execute invocations by UUID, slug, or space/slug (can be repeated or comma-separated)")
	functionDoCmd.Flags().BoolVar(&updateApplyGates, "update-apply-gates", false, "update ApplyGates on units based on trigger results (requires --trigger)")
	functionDoCmd.Flags().BoolVar(&functionIncludeConflicts, "include-conflicts", false, "pass each unit's outstanding merge conflicts to the functions, for functions that reason about them (e.g. vet-no-merge-conflicts)")
	enableWhereFlag(functionDoCmd)
	enableFilterFlag(functionDoCmd)
	addStandardDisplayFlags(functionDoCmd)
	enableWaitFlag(functionDoCmd)
	enableDisplayMutationsFlag(functionDoCmd)
	functionDoCmd.Flags().StringVar(&resourceType, "resource-type", "", "resource-type filter")
	functionDoCmd.Flags().StringVar(&whereData, "where-data", "", "where data filter")
	functionDoCmd.Flags().StringVar(&whereResource, "where-resource", "", "filter which resources the function operates on")
	functionDoCmd.Flags().StringVar(&functionToolchainType, "toolchain", "Kubernetes/YAML", "Toolchain type for the function invocations")
	functionDoCmd.Flags().StringVar(&functionOtherDataSource, "other-data-source", "", "additional data source to pass to functions (e.g., LastReleasedRevisionNum)")
	functionCmd.AddCommand(functionDoCmd)
}

// Resolved Trigger / Invocation entities from the most recent call to
// newFunctionInvocationsRequest. validateFunctionKinds reads the function names off
// these so vet/get/set don't have to re-fetch entities we already looked up
// when building the request body.
var (
	resolvedTriggers    []goclientnew.Trigger
	resolvedInvocations []goclientnew.Invocation
)

func newFunctionInvocationsRequest() *goclientnew.FunctionInvocationsRequest {
	req := &goclientnew.FunctionInvocationsRequest{}
	req.NumFilters = 0
	req.StopOnError = false
	req.ChangeDescription = changeDescription
	req.UpdateApplyGates = updateApplyGates
	req.WhereResource = whereResource
	req.IncludeConflicts = functionIncludeConflicts
	req.ToolchainType = functionToolchainType
	if workerSlug != "" {
		workerUUID, err := parseEntityIdentifierSingle[goclientnew.BridgeWorker](
			workerSlug,
			EntityTypeBridgeWorker,
			apiGetBridgeWorkerFromSlugInSpace,
			func(w *goclientnew.BridgeWorker) string { return w.BridgeWorkerID.String() },
		)
		if err != nil {
			failOnError(err)
		}
		workerID := goclientnew.UUID(workerUUID)
		req.BridgeWorkerID = &workerID
	}

	// Reset caches; this may be the second call in a process (run.go uses
	// newFunctionInvocationsRequest for its compute-mutations helper too).
	resolvedTriggers = nil
	resolvedInvocations = nil

	// Resolve trigger identifiers to full entities so we also have FunctionName
	// available without a second API round-trip for verb-scoped validation.
	if len(functionTriggerIdentifiers) > 0 {
		triggers, err := parseEntityIdentifiersAsEntities[goclientnew.Trigger](
			functionTriggerIdentifiers,
			EntityTypeTrigger,
			"TriggerID,FunctionName",
			apiGetTriggerFromSlugInSpaceCore,
			func(t *goclientnew.Trigger) string { return t.TriggerID.String() },
		)
		if err != nil {
			failOnError(err)
		}
		resolvedTriggers = triggers
		triggerUUIDs, err := entityUUIDs(triggers, func(t *goclientnew.Trigger) string { return t.TriggerID.String() })
		if err != nil {
			failOnError(err)
		}
		req.Triggers = make([]goclientnew.UUID, len(triggerUUIDs))
		for i, id := range triggerUUIDs {
			req.Triggers[i] = goclientnew.UUID(id)
		}
	}

	// Same shape for invocations.
	if len(functionInvocationIdentifiers) > 0 {
		invocations, err := parseEntityIdentifiersAsEntities[goclientnew.Invocation](
			functionInvocationIdentifiers,
			EntityTypeInvocation,
			"InvocationID,FunctionInvocations",
			apiGetInvocationFromSlugInSpace,
			func(i *goclientnew.Invocation) string { return i.InvocationID.String() },
		)
		if err != nil {
			failOnError(err)
		}
		resolvedInvocations = invocations
		invocationUUIDs, err := entityUUIDs(invocations, func(i *goclientnew.Invocation) string { return i.InvocationID.String() })
		if err != nil {
			failOnError(err)
		}
		req.Invocations = make([]goclientnew.UUID, len(invocationUUIDs))
		for i, id := range invocationUUIDs {
			req.Invocations[i] = goclientnew.UUID(id)
		}
	}

	// Resolve a single parameterized Invocation (cub invocation get/set/vet),
	// attaching the supplied parameter values. Resolved into resolvedInvocations
	// too, so verb-scoped kind validation sees the functions it calls.
	if parameterizedInvocationIdentifier != "" {
		invocations, err := parseEntityIdentifiersAsEntities[goclientnew.Invocation](
			[]string{parameterizedInvocationIdentifier},
			EntityTypeInvocation,
			"InvocationID,FunctionInvocations",
			apiGetInvocationFromSlugInSpace,
			func(i *goclientnew.Invocation) string { return i.InvocationID.String() },
		)
		if err != nil {
			failOnError(err)
		}
		resolvedInvocations = append(resolvedInvocations, invocations...)
		refs := make([]goclientnew.ParameterizedInvocationRef, 0, len(invocations))
		for i := range invocations {
			refs = append(refs, goclientnew.ParameterizedInvocationRef{
				InvocationID: invocations[i].InvocationID,
				Parameters:   parameterizedInvocationParams,
			})
		}
		req.ParameterizedInvocations = refs
	}

	return req
}

const templateEvaluator = "template"
const templatePrefix = templateEvaluator + ":"
const celEvaluator = "cel"
const celPrefix = celEvaluator + ":"

func parseFunctionArguments(args []string) []goclientnew.FunctionArgument {
	var funcArgs []goclientnew.FunctionArgument
	namedArgMode := false

	for _, arg := range args {
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			// This is a named argument
			namedArgMode = true
			parts := strings.SplitN(arg, "=", 2)
			paramName := strings.TrimPrefix(parts[0], "--")
			value := parts[1]
			evaluator := ""
			if strings.HasPrefix(value, templatePrefix) {
				evaluator = templateEvaluator
				value = strings.TrimPrefix(value, templatePrefix)
			} else if strings.HasPrefix(value, celPrefix) {
				evaluator = celEvaluator
				value = strings.TrimPrefix(value, celPrefix)
			}

			funcArgs = append(funcArgs, goclientnew.FunctionArgument{
				ParameterName: &paramName,
				Value:         &goclientnew.FunctionArgument_Value{},
				Evaluator:     &evaluator,
			})
			funcArgs[len(funcArgs)-1].Value.FromFunctionArgumentValue0(value)

		} else if namedArgMode {
			// Once we've seen a named argument, all subsequent arguments must be named
			failOnError(fmt.Errorf("positional argument '%s' cannot follow named arguments", arg))
		} else {
			evaluator := ""
			if strings.HasPrefix(arg, templatePrefix) {
				evaluator = templateEvaluator
				arg = strings.TrimPrefix(arg, templatePrefix)
			} else if strings.HasPrefix(arg, celPrefix) {
				evaluator = celEvaluator
				arg = strings.TrimPrefix(arg, celPrefix)
			}
			// This is a positional argument - no ParameterName
			funcArgs = append(funcArgs, goclientnew.FunctionArgument{
				Value:     &goclientnew.FunctionArgument_Value{},
				Evaluator: &evaluator,
			})
			funcArgs[len(funcArgs)-1].Value.FromFunctionArgumentValue0(arg)
		}
	}

	return funcArgs
}

// buildWhereClauseFromIdentifiers generates a WHERE clause from entity identifiers.
// uuidField and slugField specify the field names for UUIDs and slugs respectively.
func buildWhereClauseFromIdentifiers(identifiers []string, uuidField, slugField string) (string, error) {
	if len(identifiers) == 0 {
		return "", nil
	}

	// Check if the first identifier is a UUID to determine type
	_, err := uuid.Parse(identifiers[0])
	isUUID := err == nil

	// Validate all identifiers are of the same type
	for i, identifier := range identifiers {
		_, parseErr := uuid.Parse(identifier)
		currentIsUUID := parseErr == nil

		if i == 0 {
			continue // First one sets the type
		}

		if currentIsUUID != isUUID {
			return "", fmt.Errorf("all identifiers must be the same type (all UUIDs or all slugs)")
		}
	}

	// Build the appropriate WHERE clause
	var field string
	if isUUID {
		field = uuidField
	} else {
		field = slugField
	}

	// Build the IN clause
	var values []string
	for _, identifier := range identifiers {
		values = append(values, fmt.Sprintf("'%s'", identifier))
	}

	return fmt.Sprintf("%s IN (%s)", field, strings.Join(values, ", ")), nil
}

func initializeFunctionInvocation(functionName string, args []string) *goclientnew.FunctionInvocation {
	funcArgs := parseFunctionArguments(args)
	return &goclientnew.FunctionInvocation{
		FunctionName: functionName,
		Arguments:    funcArgs,
	}
}

// hasMultipleFunctions reports whether the request will execute more than one
// function invocation, counting direct FunctionInvocations plus any referenced
// Triggers and Invocations.
func hasMultipleFunctions(req *goclientnew.FunctionInvocationsRequest) bool {
	if req == nil {
		return false
	}
	count := 0
	if req.FunctionInvocations != nil {
		count += len(*req.FunctionInvocations)
	}
	count += len(req.Triggers) + len(req.Invocations)
	return count > 1
}

func initializeFunctionInvocationsRequest(cmdArgs []string) (*goclientnew.FunctionInvocationsRequest, error) {
	req := newFunctionInvocationsRequest()
	if len(cmdArgs) > 0 {
		functionName := cmdArgs[0]
		invokeArgs := cmdArgs[1:]
		invocation := initializeFunctionInvocation(functionName, invokeArgs)
		req.FunctionInvocations = &[]goclientnew.FunctionInvocation{*invocation}
	}
	if (req.FunctionInvocations == nil || len(*req.FunctionInvocations) == 0) && len(req.Triggers) == 0 && len(req.Invocations) == 0 && len(req.ParameterizedInvocations) == 0 {
		return nil, fmt.Errorf("A function file and/or triggers and/or invocations must be specified")
	}
	return req, nil
}

// invokeFunctiosnOnRevision handles function invocation on a specific revision
func invokeFunctionsOnRevision(revisionIdentifier string, body goclientnew.FunctionInvocationsRequest, dryRun bool) (*[]goclientnew.FunctionInvocationsResponse, error) {
	// Parse revision identifier (format: unit-slug/revision-number)
	parts := strings.Split(revisionIdentifier, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid revision format: expected unit-slug/revision-number, got %s", revisionIdentifier)
	}

	unitSlug := parts[0]
	revisionNumStr := parts[1]

	// Parse revision number
	var revisionNum int64
	fmt.Sscanf(revisionNumStr, "%d", &revisionNum)
	if revisionNum <= 0 {
		return nil, fmt.Errorf("invalid revision number: %s", revisionNumStr)
	}

	// Get unit from slug
	unit, err := apiGetUnitFromSlug(unitSlug, "UnitID")
	if err != nil {
		return nil, fmt.Errorf("failed to get unit '%s': %w", unitSlug, err)
	}

	// Get revision from number
	revision, err := apiGetRevisionFromNumber(revisionNum, unit.UnitID.String(), "RevisionID")
	if err != nil {
		return nil, fmt.Errorf("failed to get revision %d for unit '%s': %w", revisionNum, unitSlug, err)
	}

	return invokeFunctionsOnRevisionID(unit.UnitID, revision.RevisionID, body, dryRun)
}

// invokeFunctionsOnRevisionID invokes functions against the configuration of one Revision,
// which the server requires to be named by both its Unit and its own ID. The Revision is the
// data the functions run on, so it is how a function reads a Unit at a point other than its
// head.
func invokeFunctionsOnRevisionID(unitID, revisionID uuid.UUID, body goclientnew.FunctionInvocationsRequest, dryRun bool) (*[]goclientnew.FunctionInvocationsResponse, error) {
	newParams := &goclientnew.InvokeFunctionsParams{}
	newParams.Include = invokeIncludeConfigData()
	unitUUID := goclientnew.UUID(unitID)
	revisionUUID := goclientnew.UUID(revisionID)
	newParams.UnitId = &unitUUID
	newParams.RevisionId = &revisionUUID
	if dryRun {
		dryRunStr := "true"
		newParams.DryRun = &dryRunStr
	}
	if functionOtherDataSource != "" {
		newParams.OtherDataSource = &functionOtherDataSource
	}

	funcRes, err := cubClientNew.InvokeFunctionsWithResponse(ctx, uuid.MustParse(selectedSpaceID), newParams, body)
	if cubapi.IsAPIError(err, funcRes) {
		return nil, cubapi.InterpretErrorGeneric(err, funcRes)
	}

	// Handle both successful (200) and partial success/failure (207) responses
	var resp *[]goclientnew.FunctionInvocationsResponse
	if funcRes.JSON200 != nil {
		resp = funcRes.JSON200
	} else if funcRes.JSON207 != nil {
		resp = funcRes.JSON207
	}

	return resp, nil
}

type invokeArgs struct {
	Where        string
	FilterID     string
	ResourceType string
	WhereData    string
	DryRun       bool
	// Protect records the written paths as protected local overrides. Without it the
	// invocation claims nothing and each path keeps the protection it already has.
	Protect bool
	// Clearance names the classes of guarded reason this invocation is cleared for. A path
	// whose guards it does not cover is not written, and the withheld change is reported.
	Clearance string
	// Guards names the reasons this invocation states about the paths it writes, so a later
	// operation must be cleared for them.
	Guards      string
	ChangeSetID uuid.UUID
	Body        *goclientnew.FunctionInvocationsRequest
}

func invokeFunctionsOnUnits(invokeArgs *invokeArgs) (*[]goclientnew.FunctionInvocationsResponse, error) {
	var resp *[]goclientnew.FunctionInvocationsResponse
	if selectedSpaceID == "*" {
		newParams := &goclientnew.InvokeFunctionsOnOrgParams{}
		newParams.Include = invokeIncludeConfigData()
		if executorSpace != "" {
			newParams.ExecutorSpace = &executorSpace
		}
		if invokeArgs.Where != "" {
			newParams.Where = &invokeArgs.Where
		}
		if invokeArgs.FilterID != "" {
			newParams.Filter = &invokeArgs.FilterID
		}
		if invokeArgs.ResourceType != "" {
			newParams.ResourceType = &invokeArgs.ResourceType
		}
		if invokeArgs.WhereData != "" {
			newParams.WhereData = &invokeArgs.WhereData
		}
		if invokeArgs.DryRun {
			dryRunStr := "true"
			newParams.DryRun = &dryRunStr
		}
		if invokeArgs.Protect {
			newParams.Protect = &invokeArgs.Protect
		}
		// Independent of Protect: one says what the invocation claims about what it wrote, the
		// other what it understands about what was already there.
		if invokeArgs.Clearance != "" {
			newParams.Clearance = &invokeArgs.Clearance
		}
		if invokeArgs.Guards != "" {
			newParams.Guards = &invokeArgs.Guards
		}
		if invokeArgs.ChangeSetID != uuid.Nil {
			newParams.ChangeSetId = &invokeArgs.ChangeSetID
		}
		if functionOtherDataSource != "" {
			newParams.OtherDataSource = &functionOtherDataSource
		}
		funcRes, err := cubClientNew.InvokeFunctionsOnOrgWithResponse(ctx, newParams, *invokeArgs.Body)
		if cubapi.IsAPIError(err, funcRes) {
			return nil, errors.Wrap(cubapi.InterpretErrorGeneric(err, funcRes), "failed to invoke function(s) on org")
		}
		// Handle both successful (200) and partial success/failure (207) responses
		if funcRes.JSON200 != nil {
			resp = funcRes.JSON200
		} else if funcRes.JSON207 != nil {
			resp = funcRes.JSON207
		}
	} else {
		newParams := &goclientnew.InvokeFunctionsParams{}
		newParams.Include = invokeIncludeConfigData()
		if invokeArgs.Where != "" {
			newParams.Where = &invokeArgs.Where
		}
		if invokeArgs.FilterID != "" {
			newParams.Filter = &invokeArgs.FilterID
		}
		if invokeArgs.ResourceType != "" {
			newParams.ResourceType = &invokeArgs.ResourceType
		}
		if invokeArgs.WhereData != "" {
			newParams.WhereData = &invokeArgs.WhereData
		}
		if invokeArgs.DryRun {
			dryRunStr := "true"
			newParams.DryRun = &dryRunStr
		}
		if invokeArgs.Protect {
			newParams.Protect = &invokeArgs.Protect
		}
		// Independent of Protect: one says what the invocation claims about what it wrote, the
		// other what it understands about what was already there.
		if invokeArgs.Clearance != "" {
			newParams.Clearance = &invokeArgs.Clearance
		}
		if invokeArgs.Guards != "" {
			newParams.Guards = &invokeArgs.Guards
		}
		if invokeArgs.ChangeSetID != uuid.Nil {
			newParams.ChangeSetId = &invokeArgs.ChangeSetID
		}
		if functionOtherDataSource != "" {
			newParams.OtherDataSource = &functionOtherDataSource
		}
		funcRes, err := cubClientNew.InvokeFunctionsWithResponse(ctx, uuid.MustParse(selectedSpaceID), newParams, *invokeArgs.Body)
		if cubapi.IsAPIError(err, funcRes) {
			return nil, errors.Wrap(cubapi.InterpretErrorGeneric(err, funcRes), "failed to invoke function(s) on space "+selectedSpaceID)
		}
		// Handle both successful (200) and partial success/failure (207) responses
		if funcRes.JSON200 != nil {
			resp = funcRes.JSON200
		} else if funcRes.JSON207 != nil {
			resp = funcRes.JSON207
		}
	}
	return resp, nil
}

// hasAlternativeFunctionOutput reports whether a function sub-payload selector
// (--show, or any of the deprecated --output-*/--data-only aliases) is active.
// Used by helpers like awaitTriggersRemoval to decide whether to print
// informational lines (e.g., apply-gates warnings) alongside the payload.
func hasAlternativeFunctionOutput() bool {
	return effectiveShow() != ShowDefault
}

func functionDoCommandRun(cmd *cobra.Command, args []string) error {
	return runFunctionInvocations(cmd, args, ModeAny)
}

// runFunctionInvocations is the shared core for `function do` and the
// verb-scoped `function vet`, `function get`, `function set` commands.
// The mode argument enforces the kind constraint on the specified functions
// (direct, via --trigger, via --invocation) using cached signatures.
func runFunctionInvocations(cmd *cobra.Command, args []string, mode FunctionKindMode) error {
	var resp *[]goclientnew.FunctionInvocationsResponse

	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Check for mutual exclusivity between flags
	flagCount := 0
	if len(unitIdentifiers) > 0 {
		flagCount++
	}
	if where != "" {
		flagCount++
	}
	if revisionIdentifier != "" {
		flagCount++
	}
	if filterID != "" {
		flagCount++
	}
	if flagCount > 1 {
		return fmt.Errorf("--unit, --where, --filter, and --revision flags are mutually exclusive")
	}

	// Build WHERE clause from unit identifiers if provided
	var effectiveWhere string
	if len(unitIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromUnits(unitIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Parse changeset once if specified
	var changesetID string
	var changesetUUID uuid.UUID
	if functionChangesetSlug != "" {
		changesetUUID, err = parseChangeSetSlug(functionChangesetSlug)
		if err != nil {
			return err
		}
		changesetID = changesetUUID.String()
		// Add changeset constraint to WHERE clause
		effectiveWhere = addChangeSetIDToWhereClause(effectiveWhere, changesetID)
	}

	// Functions operate on lists of units. That makes it harder to know what ToolchainType(s)
	// are having functions invoked on them and what workers the functions might be running in.
	// There could be multiple of each.

	// That makes it more difficult to look up the FunctionSignature in order to validate arguments,
	// and support optional arguments. run sort of merges all functions together.

	// Verb-prefix convenience: "cub function set image" → "set-image" when
	// "image" isn't a registered function but "set-image" is. No-op for ModeAny.
	if mode != ModeAny && len(args) > 0 {
		args[0] = resolveFunctionNameForVerb(mode, args[0])
	}

	newBody, err := initializeFunctionInvocationsRequest(args)
	failOnError(err)

	// Enforce verb-scoped function-kind constraints (no-op for ModeAny).
	if err := validateFunctionKinds(mode, newBody); err != nil {
		return err
	}

	// Save prior HeadMutationNums if displaying mutations
	var priorHeadMutationNums map[string]priorUnitInfo
	if shouldDisplayMutations() {
		priorHeadMutationNums = savePriorUnitInfoFromWhere(effectiveWhere, filterID)
	}

	// Handle revision flag
	if revisionIdentifier != "" {
		resp, err = invokeFunctionsOnRevision(revisionIdentifier, *newBody, dryRun)
		if err != nil {
			return err
		}
	} else {
		clearance, cerr := clearanceJSON()
		if cerr != nil {
			return cerr
		}
		guards, gerr := guardsJSON()
		if gerr != nil {
			return gerr
		}
		invokeArgs := &invokeArgs{
			Where:        effectiveWhere,
			FilterID:     filterID,
			ResourceType: resourceType,
			WhereData:    whereData,
			DryRun:       dryRun,
			Protect:      protectChange,
			Clearance:    clearance,
			Guards:       guards,
			ChangeSetID:  changesetUUID,
			Body:         newBody,
		}
		resp, err = invokeFunctionsOnUnits(invokeArgs)
		if err != nil {
			return err
		}
	}

	// if server 200 empty-response
	if resp == nil {
		resp = &[]goclientnew.FunctionInvocationsResponse{}
	}

	// Dispatch through the unified --show / -o rendering helper. If it didn't
	// handle output, fall back to the default human summary.
	if !renderFunctionResponse(resp, hasMultipleFunctions(newBody)) {
		outputFunctionInvocationResponse(resp)
	}
	if wait {
		if !quiet && !isAlternativeOutput() && effectiveShow() == ShowDefault {
			tprintRaw("Awaiting triggers...")
		}
		// Wait one at a time
		for _, resp := range *resp {
			unitDetails, err := apiGetUnitInSpace(resp.UnitID.String(), resp.SpaceID.String(), "*")
			if err != nil {
				return err
			}
			err = awaitTriggersRemoval(unitDetails)
			if err != nil {
				return err
			}
		}
	}

	// Display mutations if requested
	if shouldDisplayMutations() {
		// Build description from function names
		funcDesc := ""
		if len(args) > 0 {
			funcDesc = args[0]
		}
		displayMutationsFromFunctionResponse(resp, dryRun, priorHeadMutationNums, funcDesc)
	}

	return nil
}
