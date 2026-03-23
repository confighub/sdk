// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
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

Functions can be used to modify, validate, or inspect unit configurations.

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

  - set-image: Update container image in a deployment
  - set-int-path: Set an integer value at a specific path in the configuration
  - get-replicas: Get the number of replicas for deployments
  - set-replicas: Set the number of replicas for deployments
  - where-filter: Filter units based on a condition
  - cel-validate: Validate resources using CEL expressions

Examples:
` + "```" + `
  # Use set-image to update container image in a deployment
  cub function do \
    --space my-space \
    --where "Slug = 'my-deployment'" \
    set-image nginx nginx:mainline-otel \
    --wait

  # Use yq function to get container image from a deployment
  cub function do \
    --space my-space \
    --where "Slug = 'my-deployment'" \
    --output-only \
    yq '.spec.template.spec.containers[0].image'

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
    --output-jq '.[].Value'

  # Filter deployments with more than 1 replica
  cub function do \
    --space my-space \
    where-filter apps/v1/Deployment 'spec.replicas > 1' \
    --quiet \
    --output-jq '.[].Passed' \
    --jq '.[].UnitID'

  # Set best-practice pod fields, other than for probes, to default values in all units in all spaces
  cub function do \
    --space "*" \
	-- \
	set-pod-defaults --pod-security=true --automount-service-account-token=true --security-context=true --resources=true --probes=false

  # Validate deployment replicas using CEL
  cub function do --space my-space \
    cel-validate 'r.kind != "Deployment" || r.spec.replicas > 1' \
    --quiet \
    --output-jq '.[].Passed' \
    --jq '.[].UnitID'
` + "```" + `
`

	agentContext := `Agent-specific guidance:

Prerequisites:
- Authentication: Run 'cub auth login' first
- Space context: Set with 'cub context set --space SPACE_SLUG' or use --space flag
- Discovery: Use 'cub function list' to see available functions for each toolchain

Common agent workflows:

1. Inspect configuration before making changes:
   cub function do --space SPACE --where "Slug = 'UNIT'" get-placeholders
   cub function do --space SPACE --where "Slug = 'UNIT'" yq '.spec.replicas'

2. Modify configuration:
   cub function do --space SPACE --where "Slug = 'UNIT'" set-image nginx nginx:1.25-alpine --wait

3. Validate after changes:
   cub function do --space SPACE --where "Slug = 'UNIT'" no-placeholders
   cub function do --space SPACE --where "Slug = 'UNIT'" cel-validate 'r.spec.replicas > 0'

Key flags for agents:
- --where: Filter units (use quotes around expressions)
- --space: Target space ("*" for all spaces where supported)
- --output-only: Show just function output, not execution details
- --quiet: Suppress status messages
- --wait: Wait for async triggers to complete

Error handling:
- Functions return Success: true/false in response
- Use --quiet --output-jq to extract specific values
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
var revisionIdentifier string
var functionChangesetSlug string

func init() {
	functionDoCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the function")
	functionDoCmd.Flags().BoolVar(&outputOnly, "output-only", false, "show output without other response details")
	functionDoCmd.Flags().BoolVar(&outputRaw, "output-json", false, "show output as raw JSON")
	functionDoCmd.Flags().StringVar(&outputJQ, "output-jq", "", "apply jq to output JSON")
	functionDoCmd.Flags().BoolVar(&outputValuesOnly, "output-values-only", false, "show output values (from functions returning AttributeValueList) without other response details")
	functionDoCmd.Flags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	// Same flag as unit update
	functionDoCmd.Flags().StringVar(&changeDescription, "change-desc", "", "change description")
	functionDoCmd.Flags().StringVar(&functionChangesetSlug, "changeset", "", "changeset to associate units with")
	functionDoCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	functionDoCmd.Flags().StringVar(&revisionIdentifier, "revision", "", "target a specific revision (format: unit-slug/revision-number, e.g. mydeployment/3)")
	functionDoCmd.Flags().BoolVar(&dryRun, "dry-run", false, "dry run mode: execute functions but skip updating configuration data")
	functionDoCmd.Flags().StringSliceVar(&functionTriggerIdentifiers, "trigger", []string{}, "execute triggers by UUID, slug, or space/slug (can be repeated or comma-separated)")
	functionDoCmd.Flags().StringSliceVar(&functionInvocationIdentifiers, "invocation", []string{}, "execute invocations by UUID, slug, or space/slug (can be repeated or comma-separated)")
	functionDoCmd.Flags().BoolVar(&updateApplyGates, "update-apply-gates", false, "update ApplyGates on units based on trigger results (requires --trigger)")
	enableWhereFlag(functionDoCmd)
	enableFilterFlag(functionDoCmd)
	addStandardDisplayFlags(functionDoCmd)
	enableWaitFlag(functionDoCmd)
	enableDisplayMutationsFlag(functionDoCmd)
	functionDoCmd.Flags().StringVar(&resourceType, "resource-type", "", "resource-type filter")
	functionDoCmd.Flags().StringVar(&whereData, "where-data", "", "where data filter")
	functionDoCmd.Flags().StringVar(&whereResource, "where-resource", "", "filter which resources the function operates on")
	functionDoCmd.Flags().StringVar(&toolchainType, "toolchain", "Kubernetes/YAML", "Toolchain type for the function invocations")
	functionDoCmd.Flags().StringVar(&liveStateType, "livestate-type", "", "Invoke the function on the live state and use the flag value as the toolchain type for live state.")
	functionCmd.AddCommand(functionDoCmd)
}

func newFunctionInvocationsRequest() *goclientnew.FunctionInvocationsRequest {
	req := &goclientnew.FunctionInvocationsRequest{}
	req.NumFilters = 0
	req.StopOnError = false
	req.ChangeDescription = changeDescription
	req.UpdateApplyGates = updateApplyGates
	req.WhereResource = whereResource
	if liveStateType != "" {
		req.OnLiveState = true
		req.ToolchainType = liveStateType
	} else {
		req.ToolchainType = toolchainType
	}
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

	// Parse and add trigger identifiers
	if len(functionTriggerIdentifiers) > 0 {
		triggers, err := parseEntityIdentifiersForTrigger(functionTriggerIdentifiers)
		if err != nil {
			failOnError(err)
		}
		// Convert []uuid.UUID to []goclientnew.UUID
		triggerUUIDs := make([]goclientnew.UUID, len(triggers))
		for i, t := range triggers {
			triggerUUIDs[i] = goclientnew.UUID(t)
		}
		req.Triggers = triggerUUIDs
	}

	// Parse and add invocation identifiers
	if len(functionInvocationIdentifiers) > 0 {
		invocations, err := parseEntityIdentifiersForInvocation(functionInvocationIdentifiers)
		if err != nil {
			failOnError(err)
		}
		// Convert []uuid.UUID to []goclientnew.UUID
		invocationUUIDs := make([]goclientnew.UUID, len(invocations))
		for i, inv := range invocations {
			invocationUUIDs[i] = goclientnew.UUID(inv)
		}
		req.Invocations = invocationUUIDs
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

// buildWhereClauseFromIdentifiers generates a WHERE clause from entity identifiers
// uuidField and slugField specify the field names for UUIDs and slugs respectively
// parseEntityIdentifiersForTrigger wraps the generic function for trigger parsing
func parseEntityIdentifiersForTrigger(identifiers []string) ([]uuid.UUID, error) {
	return parseEntityIdentifiers[goclientnew.Trigger](
		identifiers,
		EntityTypeTrigger,
		apiGetTriggerFromSlugInSpaceCore,
		func(t *goclientnew.Trigger) string { return t.TriggerID.String() },
	)
}

// parseEntityIdentifiersForInvocation wraps the generic function for invocation parsing
func parseEntityIdentifiersForInvocation(identifiers []string) ([]uuid.UUID, error) {
	return parseEntityIdentifiers[goclientnew.Invocation](
		identifiers,
		EntityTypeInvocation,
		apiGetInvocationFromSlugInSpace,
		func(i *goclientnew.Invocation) string { return i.InvocationID.String() },
	)
}

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

func initializeFunctionInvocationsRequest(cmdArgs []string) (*goclientnew.FunctionInvocationsRequest, error) {
	req := newFunctionInvocationsRequest()
	if len(cmdArgs) > 0 {
		functionName := cmdArgs[0]
		invokeArgs := cmdArgs[1:]
		invocation := initializeFunctionInvocation(functionName, invokeArgs)
		req.FunctionInvocations = &[]goclientnew.FunctionInvocation{*invocation}
	}
	if (req.FunctionInvocations == nil || len(*req.FunctionInvocations) == 0) && len(req.Triggers) == 0 && len(req.Invocations) == 0 {
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

	// Call the API with revision parameters
	newParams := &goclientnew.InvokeFunctionsParams{}
	unitUUID := goclientnew.UUID(unit.UnitID)
	revisionUUID := goclientnew.UUID(revision.RevisionID)
	newParams.UnitId = &unitUUID
	newParams.RevisionId = &revisionUUID
	if dryRun {
		dryRunStr := "true"
		newParams.DryRun = &dryRunStr
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
	ChangeSetID  uuid.UUID
	Body         *goclientnew.FunctionInvocationsRequest
}

func invokeFunctionsOnUnits(invokeArgs *invokeArgs) (*[]goclientnew.FunctionInvocationsResponse, error) {
	var resp *[]goclientnew.FunctionInvocationsResponse
	if selectedSpaceID == "*" {
		newParams := &goclientnew.InvokeFunctionsOnOrgParams{}
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
		if invokeArgs.ChangeSetID != uuid.Nil {
			newParams.ChangeSetId = &invokeArgs.ChangeSetID
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
		if invokeArgs.ChangeSetID != uuid.Nil {
			newParams.ChangeSetId = &invokeArgs.ChangeSetID
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

func hasAlternativeFunctionOutput() bool {
	return outputRaw || outputOnly || outputValuesOnly || dataOnly || outputJQ != ""
}

func functionDoCommandRun(cmd *cobra.Command, args []string) error {
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

	newBody, err := initializeFunctionInvocationsRequest(args)
	failOnError(err)

	// Save prior HeadMutationNums if displaying mutations
	var priorHeadMutationNums map[string]priorUnitInfo
	if displayMutations {
		priorHeadMutationNums = savePriorUnitInfoFromWhere(effectiveWhere, filterID)
	}

	// Handle revision flag
	if revisionIdentifier != "" {
		resp, err = invokeFunctionsOnRevision(revisionIdentifier, *newBody, dryRun)
		if err != nil {
			return err
		}
	} else {
		invokeArgs := &invokeArgs{
			Where:        effectiveWhere,
			FilterID:     filterID,
			ResourceType: resourceType,
			WhereData:    whereData,
			DryRun:       dryRun,
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

	// Check if any alternative output format is specified
	hasAlternativeOutput := hasAlternativeOutput() || outputJQ != "" || outputValuesOnly

	if !hasAlternativeOutput {
		outputFunctionInvocationResponse(resp)
	}
	if jsonOutput {
		displayJSON(resp)
	}
	if jq != "" {
		displayJQ(resp)
	}
	if yamlOutput {
		displayYAML(resp)
	}
	if yq != "" {
		displayYQ(resp)
	}
	if outputJQ != "" {
		for _, resp := range *resp {
			for _, outputData := range resp.Outputs {
				if len(outputData) != 0 {
					outputBytes, err := base64.StdEncoding.DecodeString(outputData)
					if err != nil {
						tprintRaw(outputData)
						failOnError(fmt.Errorf("%s: Failed to decode output", err.Error()))
					}
					if strings.TrimSpace(string(outputBytes)) != "null" {
						displayJQForBytes(outputBytes, outputJQ)
					}
				}
			}
		}
	}
	if outputValuesOnly {
		for _, resp := range *resp {
			if attributeOutput, exists := resp.Outputs[string(api.OutputTypeAttributeValueList)]; exists && len(attributeOutput) != 0 {
				outputBytes, err := base64.StdEncoding.DecodeString(attributeOutput)
				if err != nil {
					tprintRaw(attributeOutput)
					failOnError(fmt.Errorf("%s: Failed to decode output", err.Error()))
				}
				if strings.TrimSpace(string(outputBytes)) != "null" {
					// Try to decode as AttributeValueList
					var attrValueList api.AttributeValueList
					err = json.Unmarshal(outputBytes, &attrValueList)
					if err != nil {
						tprintRaw(string(outputBytes))
						failOnError(fmt.Errorf("%s: Failed to decode output as AttributeValueList", err.Error()))
					}
					for i := range attrValueList {
						tprint("%v", attrValueList[i].Value)
					}
				}
			}
		}
	}
	if wait {
		if !quiet && !hasAlternativeOutput && !hasAlternativeFunctionOutput() {
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
	if displayMutations {
		// Build description from function names
		funcDesc := ""
		if len(args) > 0 {
			funcDesc = args[0]
		}
		displayMutationsFromFunctionResponse(resp, dryRun, priorHeadMutationNums, funcDesc)
	}

	return nil
}

// unitDisplayName returns the unit slug with ID in parentheses if available, otherwise just the UUID.
func unitDisplayName(respMsg *goclientnew.FunctionInvocationsResponse) string {
	if respMsg.UnitSlug != "" {
		slug := respMsg.UnitSlug
		if respMsg.SpaceSlug != "" {
			slug = respMsg.SpaceSlug + "/" + slug
		}
		return fmt.Sprintf("%s (%s)", slug, respMsg.UnitID.String())
	}
	return respMsg.UnitID.String()
}

// functionDisplayName returns the function name if available, otherwise the index.
func functionDisplayName(functionName string, index int) string {
	if functionName != "" {
		return functionName
	}
	return fmt.Sprintf("#%d", index)
}

// displayAttributeValue prints a single attribute value with labeled fields.
func displayAttributeValue(av *api.AttributeValue) {
	description := ""
	if av.Details != nil && av.Details.Description != "" {
		description = av.Details.Description
	}
	tprint("Value: %v  DataType: %s  Path: %s  Resource: %s  Type: %s",
		av.Value, av.DataType, av.Path, av.ResourceName, av.ResourceType)
	if av.Score != "" {
		tprint("  Score: %s", av.Score)
	}
	if description != "" {
		tprint("  Description: %s", description)
	}
	displayIssues(av.Issues)
}

// displayResourceInfo prints a single resource info entry.
func displayResourceInfo(ri *api.ResourceInfo) {
	tprint("Resource: %s  Type: %s", ri.ResourceName, ri.ResourceType)
}

// displayResource prints a single resource entry with its body.
func displayResource(r *api.Resource) {
	tprint("Resource: %s  Type: %s:", r.ResourceName, r.ResourceType)
	tprintRaw(r.ResourceBody)
}

// displayResponseError prints a response error with details on separate indented lines.
func displayResponseError(respErr *goclientnew.ResponseError) {
	detail := detailView()
	detail.Append([]string{strings.ToUpper("Error"), respErr.Message})
	detail.Render()
	for _, d := range respErr.Details {
		tprint("  %s", d)
	}
}

// displayIssues prints issues if present.
func displayIssues(issues []api.Issue) {
	for _, issue := range issues {
		if issue.Identifier != "" {
			tprint("  Issue [%s]: %s", issue.Identifier, issue.Message)
		} else {
			tprint("  Issue: %s", issue.Message)
		}
	}
}

// displayValidationResult prints a single validation result with its details.
func displayValidationResult(vr *api.ValidationResult) {
	funcDisplay := functionDisplayName(vr.FunctionName, vr.Index)
	tprint("Passed: %v  Function: %s", vr.Passed, funcDisplay)
	if vr.MaxScore != "" {
		tprint("  MaxScore: %s", vr.MaxScore)
	}
	for _, detail := range vr.Details {
		tprint("  %s", detail)
	}
	for _, issue := range vr.Issues {
		if issue.Identifier != "" {
			tprint("  Issue [%s]: %s", issue.Identifier, issue.Message)
		} else {
			tprint("  Issue: %s", issue.Message)
		}
	}
	if len(vr.FailedAttributes) > 0 {
		tprintRaw("  Attributes:")
		for j := range vr.FailedAttributes {
			tprint("  ")
			displayAttributeValue(&vr.FailedAttributes[j])
		}
	}
}

func outputFunctionInvocationResponse(respMsgs *[]goclientnew.FunctionInvocationsResponse) {
	for i := range *respMsgs {
		respMsg := &(*respMsgs)[i]
		if !quiet && !outputOnly && !dataOnly && !outputValuesOnly && !outputRaw {
			statusVerb := "failed"
			if respMsg.Success {
				statusVerb = "succeeded"
			}
			unitDisplay := unitDisplayName(respMsg)
			if respMsg.RevisionID != uuid.Nil {
				tprint("Function(s) %s on revision %s of unit %s", statusVerb, respMsg.RevisionID.String(), unitDisplay)
			} else {
				tprint("Function(s) %s on unit %s", statusVerb, unitDisplay)
			}
			if !respMsg.Success && respMsg.Error != nil {
				displayResponseError(respMsg.Error)
			}
		}
		if dataOnly || (!quiet && !outputOnly && !outputValuesOnly && !outputRaw) {
			if dataOnly || len(respMsg.Mutators) > 0 {
				// Don't use detailView to print the data because it pads the entire width with spaces.
				if !dataOnly {
					if verbose && len(respMsg.ConfigData) != 0 {
						tprintRaw("CONFIGDATA\n---------\n")
					} else {
						tprintRaw("Config data changed")
					}
				}
				if len(respMsg.ConfigData) != 0 {
					data, err := base64.StdEncoding.DecodeString(respMsg.ConfigData)
					if err != nil {
						failOnError(fmt.Errorf("%s: Failed to decode config data", err.Error()))
					}
					if dataOnly || verbose {
						tprintRaw(string(data))
					}
				}
			} else if !dataOnly && len(respMsg.ConfigData) != 0 && len(respMsg.Outputs) == 0 {
				tprintRaw("Config data not changed")
			}
		}
		if (outputOnly || (!quiet && !dataOnly && !outputValuesOnly)) && len(respMsg.Outputs) != 0 {
			// Don't use detailView to print the output because it pads the entire width with spaces.
			if !outputOnly && !outputValuesOnly && !outputRaw {
				tprintRaw("OUTPUT\n------\n")
			}

			// Handle all output types
			for outputType, outputData := range respMsg.Outputs {
				if len(outputData) == 0 {
					continue
				}

				outputBytes, err := base64.StdEncoding.DecodeString(outputData)
				if err != nil {
					tprintRaw(outputData)
					failOnError(fmt.Errorf("%s: Failed to decode output", err.Error()))
				}
				if strings.TrimSpace(string(outputBytes)) == "null" {
					continue
				}

				if len(respMsg.Outputs) > 1 {
					tprintRaw(fmt.Sprintf("%s:\n", outputType))
				}

				switch outputType {
				case string(api.OutputTypeYAML):
					var payload api.YAMLPayload
					err := json.Unmarshal(outputBytes, &payload)
					// If there's an error print the raw output
					if err != nil || outputRaw {
						tprintRaw(string(outputBytes))
					} else {
						tprintRaw(payload.Payload)
					}
				case string(api.OutputTypeAttributeValueList):
					var payload api.AttributeValueList
					err := json.Unmarshal(outputBytes, &payload)
					// If there's an error print the raw output
					if err != nil {
						tprintRaw(string(outputBytes))
					} else if outputRaw {
						displayJSON(payload)
					} else {
						for i := range payload {
							funcDisplay := functionDisplayName(payload[i].FunctionName, payload[i].Index)
							tprint("Function: %s", funcDisplay)
							displayAttributeValue(&payload[i])
						}
					}
				case string(api.OutputTypeValidationResultList), string(api.OutputTypeValidationResult):
					var payload api.ValidationResultList
					err := json.Unmarshal(outputBytes, &payload)
					if err != nil {
						// Try parsing as a single result. Shouldn't happen now.
						var single api.ValidationResult
						err := json.Unmarshal(outputBytes, &single)
						// If there's an error print the raw output
						if err != nil {
							tprintRaw(string(outputBytes))
						} else if outputRaw {
							displayJSON(single)
						} else {
							displayValidationResult(&single)
						}
					} else if outputRaw {
						displayJSON(payload)
					} else {
						for i := range payload {
							displayValidationResult(&payload[i])
						}
					}
				case string(api.OutputTypeResourceInfoList):
					var payload api.ResourceInfoList
					err := json.Unmarshal(outputBytes, &payload)
					// If there's an error print the raw output
					if err != nil {
						tprintRaw(string(outputBytes))
					} else if outputRaw {
						displayJSON(payload)
					} else {
						for i := range payload {
							displayResourceInfo(&payload[i])
						}
					}
				case string(api.OutputTypeResourceList):
					var payload api.ResourceList
					err := json.Unmarshal(outputBytes, &payload)
					// If there's an error print the raw output
					if err != nil {
						tprintRaw(string(outputBytes))
					} else if outputRaw {
						displayJSON(payload)
					} else {
						for i := range payload {
							displayResource(&payload[i])
						}
					}
				default:
					// Output should be JSON, but if there's an error print the raw output
					var out bytes.Buffer
					err := json.Indent(&out, outputBytes, "", "  ")
					if err != nil || outputRaw {
						tprintRaw(string(outputBytes))
					} else {
						tprintRaw(out.String())
					}
				}

				if len(respMsg.Outputs) > 1 {
					tprintRaw("\n")
				}
			}
		}
	}
}
