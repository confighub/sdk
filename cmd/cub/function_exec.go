// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var functionExecCmd = &cobra.Command{
	Use:   "exec file",
	Short: "Invoke a list of functions",
	Long: getCommandHelp(`Invoke functions on units. Functions can be used to modify, validate, or query unit configurations.

To display a list of supported functions, run:
`+"```"+`
  cub function list
`+"```"+`

To display usage details of a specific function, run:
`+"```"+`
  cub function explain --toolchain TOOLCHAIN_TYPE FUNCTION_NAME
`+"```"+`

Example Functions:

  - set-image: Update container image in a deployment
  - set-int-path: Set an integer value at a specific path in the configuration
  - get-replicas: Get the number of replicas for deployments
  - set-replicas: Set the number of replicas for deployments
  - where-filter: Filter units based on a condition
  - cel-validate: Validate resources using CEL expressions

The syntax is the same as the cub function do command line, but without "cub function do" and without flags.

Example:
`+"```"+`
  cub function exec functions.txt --where "Slug = 'mydeployment'
`+"```"+`

Where functions.txt contains:
`+"```"+`
set-replicas 3
set-image nginx nginx:v234
set-namespace myns
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        functionExecCommandRun,
}

func init() {
	functionExecCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the function")
	functionExecCmd.Flags().BoolVar(&outputOnly, "output-only", false, "show output without other response details")
	functionExecCmd.Flags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	// Same flag as unit update
	functionExecCmd.Flags().StringVar(&changeDescription, "change-desc", "", "change description")
	functionExecCmd.Flags().StringVar(&functionChangesetSlug, "changeset", "", "changeset to associate units with")
	functionExecCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	functionExecCmd.Flags().StringVar(&revisionIdentifier, "revision", "", "target a specific revision (format: unit-slug/revision-number, e.g. mydeployment/3)")
	functionExecCmd.Flags().BoolVar(&dryRun, "dry-run", false, "dry run mode: execute functions but skip updating configuration data")
	functionExecCmd.Flags().StringSliceVar(&functionTriggerIdentifiers, "trigger", []string{}, "execute triggers by UUID, slug, or space/slug (can be repeated or comma-separated)")
	functionExecCmd.Flags().StringSliceVar(&functionInvocationIdentifiers, "invocation", []string{}, "execute invocations by UUID, slug, or space/slug (can be repeated or comma-separated)")
	enableWhereFlag(functionExecCmd)
	enableFilterFlag(functionExecCmd)
	addStandardDisplayFlags(functionExecCmd)
	enableWaitFlag(functionExecCmd)
	enableDisplayMutationsFlag(functionExecCmd)
	functionExecCmd.Flags().StringVar(&resourceType, "resource-type", "", "resource-type filter")
	functionExecCmd.Flags().StringVar(&whereData, "where-data", "", "where data filter")
	functionExecCmd.Flags().StringVar(&whereResource, "where-resource", "", "filter which resources the function operates on")
	functionExecCmd.Flags().StringVar(&outputJQ, "output-jq", "", "apply jq to output JSON")
	functionExecCmd.Flags().StringVar(&toolchainType, "toolchain", "Kubernetes/YAML", "Toolchain type for the function invocations")
	functionExecCmd.Flags().StringVar(&liveStateType, "livestate-type", "", "Invoke the functions on the live state and use the flag value as the toolchain type for live state.")
	functionCmd.AddCommand(functionExecCmd)
}

// executeFunctionsFromFile reads functions from a file and executes them with the given where clause
func executeFunctionsFromFile(functionsFile, whereClause string, unitIds []string) (*[]goclientnew.FunctionInvocationsResponse, error) {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return nil, err
	}

	// Check for mutual exclusivity between flags
	flagCount := 0
	if len(unitIds) > 0 {
		flagCount++
	}
	if whereClause != "" {
		flagCount++
	}
	if revisionIdentifier != "" {
		flagCount++
	}
	if filterID != "" {
		flagCount++
	}
	if flagCount > 1 {
		return nil, fmt.Errorf("--unit, --where, --filter, and --revision flags are mutually exclusive")
	}

	// Build WHERE clause from unit identifiers if provided
	var effectiveWhere string
	if len(unitIds) > 0 {
		whereFromUnits, err := buildWhereClauseFromUnits(unitIds)
		if err != nil {
			return nil, err
		}
		effectiveWhere = whereFromUnits
	} else {
		effectiveWhere = whereClause
	}

	// Parse changeset once if specified (not for revision-based invocations)
	var changesetID string
	var changesetUUID uuid.UUID
	if functionChangesetSlug != "" && revisionIdentifier == "" {
		changesetUUID, err = parseChangeSetSlug(functionChangesetSlug)
		if err != nil {
			return nil, err
		}
		changesetID = changesetUUID.String()
		// Add changeset constraint to WHERE clause
		effectiveWhere = addChangeSetIDToWhereClause(effectiveWhere, changesetID)
	}

	// Create function invocations request. This also parses invocations and triggers.
	newBody := newFunctionInvocationsRequest()

	if functionsFile != "" {
		var content []byte
		if functionsFile == "-" {
			content, err = readStdin()
			if err != nil {
				return nil, err
			}
		} else {
			content = readFile(functionsFile)
		}

		// Parse functions from file content
		invocations := []goclientnew.FunctionInvocation{}
		lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
		for _, line := range lines {
			args := strings.Fields(line)
			if len(args) == 0 {
				continue
			}
			functionName := args[0]
			invokeArgs := args[1:]
			invocation := initializeFunctionInvocation(functionName, invokeArgs)
			invocations = append(invocations, *invocation)
		}

		newBody.FunctionInvocations = &invocations
	}

	if (newBody.FunctionInvocations == nil || len(*newBody.FunctionInvocations) == 0) && len(newBody.Triggers) == 0 && len(newBody.Invocations) == 0 {
		return nil, fmt.Errorf("A function file and/or triggers and/or invocations must be specified")
	}

	// Execute functions
	var resp *[]goclientnew.FunctionInvocationsResponse

	// Handle revision flag
	if revisionIdentifier != "" {
		resp, err = invokeFunctionsOnRevision(revisionIdentifier, *newBody, dryRun)
		if err != nil {
			return resp, err
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
			return resp, err
		}
	}

	// Handle empty response
	if resp == nil {
		resp = &[]goclientnew.FunctionInvocationsResponse{}
	}

	return resp, nil
}

func functionExecCommandRun(cmd *cobra.Command, args []string) error {
	file := ""
	if len(args) > 0 {
		file = args[0]
	}

	// Save prior HeadMutationNums if displaying mutations
	var priorHeadMutationNums map[string]priorUnitInfo
	if displayMutations {
		// Build effective WHERE clause
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
		priorHeadMutationNums = savePriorUnitInfoFromWhere(effectiveWhere, "")
	}

	resp, err := executeFunctionsFromFile(file, where, unitIdentifiers)
	if err != nil {
		return err
	}
	// Check if any alternative output format is specified
	hasAlternativeOutput := hasAlternativeOutput() || outputJQ != ""

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
	if wait {
		if !quiet && !dataOnly && !outputOnly {
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
		execDesc := "function exec"
		if file != "" && file != "-" {
			execDesc = "function exec " + file
		}
		displayMutationsFromFunctionResponse(resp, dryRun, priorHeadMutationNums, execDesc)
	}

	return nil
}
