// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

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
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        functionExecCommandRun,
}

func init() {
	functionExecCmd.Flags().BoolVar(&useWorker, "use-worker", false, "use the attached worker to execute the function")
	functionExecCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the function")
	functionExecCmd.Flags().BoolVar(&combine, "combine", false, "combine results")
	functionExecCmd.Flags().BoolVar(&outputOnly, "output-only", false, "show output without other response details")
	functionExecCmd.Flags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	// Same flag as unit update
	functionExecCmd.Flags().StringVar(&changeDescription, "change-desc", "", "change description")
	enableWhereFlag(functionExecCmd)
	enableQuietFlag(functionExecCmd)
	enableJsonFlag(functionExecCmd)
	enableJqFlag(functionExecCmd)
	enableWaitFlag(functionExecCmd)
	functionExecCmd.Flags().StringVar(&outputJQ, "output-jq", "", "apply jq to output JSON")
	functionCmd.AddCommand(functionExecCmd)
}

// executeFunctionsFromFile reads functions from a file and executes them with the given where clause
func executeFunctionsFromFile(functionsFile, whereClause string) (*[]goclientnew.FunctionInvocationResponse, error) {
	var content []byte
	var err error

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

	// Create function invocations request
	newBody := newFunctionInvocationsRequest()
	newBody.FunctionInvocations = &invocations

	// Execute functions
	var resp *[]goclientnew.FunctionInvocationResponse
	if selectedSpaceID == "*" {
		newParams := &goclientnew.InvokeFunctionsOnOrgParams{}
		if whereClause != "" {
			encoded := url.QueryEscape(whereClause)
			newParams.Where = &encoded
		}
		funcRes, err := cubClientNew.InvokeFunctionsOnOrgWithResponse(ctx, newParams, *newBody)
		if IsAPIError(err, funcRes) {
			return nil, fmt.Errorf("failed to invoke function on org: %s", InterpretErrorGeneric(err, funcRes).Error())
		}
		resp = funcRes.JSON200
	} else {
		newParams := &goclientnew.InvokeFunctionsParams{}
		if whereClause != "" {
			encoded := url.QueryEscape(whereClause)
			newParams.Where = &encoded
		}
		funcRes, err := cubClientNew.InvokeFunctionsWithResponse(ctx, uuid.MustParse(selectedSpaceID), newParams, *newBody)
		if IsAPIError(err, funcRes) {
			return nil, InterpretErrorGeneric(err, funcRes)
		}
		resp = funcRes.JSON200
	}

	// Handle empty response
	if resp == nil {
		resp = &[]goclientnew.FunctionInvocationResponse{}
	}

	return resp, nil
}

func functionExecCommandRun(cmd *cobra.Command, args []string) error {
	resp, err := executeFunctionsFromFile(args[0], where)
	if err != nil {
		return err
	}
	outputFunctionInvocationResponse(resp)
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
			unitDetails, err := apiGetUnit(resp.UnitID.String())
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
