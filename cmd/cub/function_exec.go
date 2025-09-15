// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var functionExecCmd = &cobra.Command{
	Use:   "exec file",
	Short: "Invoke a list of functions",
	Long: `Invoke functions on units. Functions can be used to modify, validate, or query unit configurations.

To display a list of supported functions, run:
  cub function list

To display usage details of a specific function, run:
  cub function explain --toolchain TOOLCHAIN_TYPE FUNCTION_NAME

Example Functions:
  - set-image: Update container image in a deployment
  - set-int-path: Set an integer value at a specific path in the configuration
  - get-replicas: Get the number of replicas for deployments
  - set-replicas: Set the number of replicas for deployments
  - where-filter: Filter units based on a condition
  - cel-validate: Validate resources using CEL expressions

The syntax is the same as the cub function do command line, but without "cub function do" and without flags.

Example:
  cub function exec functions.txt --where "Slug = 'mydeployment'

Where functions.txt contains:
set-replicas 3
set-image nginx nginx:v234
set-namespace myns`,
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
	enableQuietFlag(functionExecCmd)
	enableJsonFlag(functionExecCmd)
	enableJqFlag(functionExecCmd)
	enableWaitFlag(functionExecCmd)
	functionExecCmd.Flags().StringVar(&outputJQ, "output-jq", "", "apply jq to output JSON")
	functionExecCmd.Flags().StringVar(&toolchainType, "toolchain", "Kubernetes/YAML", "Toolchain type for the function invocations")
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
		resp, err = invokeFunctionOnRevision(revisionIdentifier, *newBody, dryRun)
		if err != nil {
			return nil, err
		}
	} else if selectedSpaceID == "*" {
		newParams := &goclientnew.InvokeFunctionsOnOrgParams{}
		if effectiveWhere != "" {
			newParams.Where = &effectiveWhere
		}
		if filterID != "" {
			newParams.Filter = &filterID
		}
		if dryRun {
			dryRunStr := "true"
			newParams.DryRun = &dryRunStr
		}
		if functionChangesetSlug != "" {
			changesetUUID, err := parseChangeSetSlug(functionChangesetSlug)
			if err != nil {
				return nil, err
			}
			changesetID := changesetUUID.String()
			newParams.ChangeSetId = &changesetID
		}
		funcRes, err := cubClientNew.InvokeFunctionsOnOrgWithResponse(ctx, newParams, *newBody)
		if cubapi.IsAPIError(err, funcRes) {
			return nil, fmt.Errorf("failed to invoke function on org: %s", cubapi.InterpretErrorGeneric(err, funcRes).Error())
		}
		// Handle both successful (200) and partial success/failure (207) responses
		if funcRes.JSON200 != nil {
			resp = funcRes.JSON200
		} else if funcRes.JSON207 != nil {
			resp = funcRes.JSON207
		}
	} else {
		newParams := &goclientnew.InvokeFunctionsParams{}
		if effectiveWhere != "" {
			newParams.Where = &effectiveWhere
		}
		if filterID != "" {
			newParams.Filter = &filterID
		}
		if dryRun {
			dryRunStr := "true"
			newParams.DryRun = &dryRunStr
		}
		if functionChangesetSlug != "" {
			changesetUUID, err := parseChangeSetSlug(functionChangesetSlug)
			if err != nil {
				return nil, err
			}
			changesetID := changesetUUID.String()
			newParams.ChangeSetId = &changesetID
		}
		funcRes, err := cubClientNew.InvokeFunctionsWithResponse(ctx, uuid.MustParse(selectedSpaceID), newParams, *newBody)
		if cubapi.IsAPIError(err, funcRes) {
			return nil, cubapi.InterpretErrorGeneric(err, funcRes)
		}
		// Handle both successful (200) and partial success/failure (207) responses
		if funcRes.JSON200 != nil {
			resp = funcRes.JSON200
		} else if funcRes.JSON207 != nil {
			resp = funcRes.JSON207
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
	resp, err := executeFunctionsFromFile(file, where, unitIdentifiers)
	if err != nil {
		return err
	}
	// Check if any alternative output format is specified
	hasAlternativeOutput := jsonOutput || jq != "" || outputJQ != ""

	if !hasAlternativeOutput {
		outputFunctionInvocationResponse(resp)
	}
	if jsonOutput {
		displayJSON(resp)
	}
	if jq != "" {
		displayJQ(resp)
	}
	if outputJQ != "" {
		for _, resp := range *resp {
			if len(resp.Output) != 0 {
				outputBytes, err := base64.StdEncoding.DecodeString(resp.Output)
				if err != nil {
					tprintRaw(resp.Output)
					failOnError(fmt.Errorf("%s: Failed to decode output", err.Error()))
				}
				if strings.TrimSpace(string(outputBytes)) != "null" {
					displayJQForBytes(outputBytes, outputJQ)
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
			selectedSpaceID = resp.SpaceID.String()
			unitDetails, err := apiGetUnit(resp.UnitID.String(), "*")
			if err != nil {
				return err
			}
			err = awaitTriggersRemoval(unitDetails)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
