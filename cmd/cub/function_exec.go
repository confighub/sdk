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

To get a list of supported functions, run:
  cub function list

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

func functionExecCommandRun(cmd *cobra.Command, args []string) error {
	var resp *[]goclientnew.FunctionInvocationResponse
	newBody := newFunctionInvocationsRequest()

	var content []byte
	var err error
	if args[0] == "-" {
		content, err = readStdin()
		if err != nil {
			return err
		}
	} else {
		content = readFile(args[0])
	}
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

	if selectedSpaceID == "*" {
		newParams := &goclientnew.InvokeFunctionsOnOrgParams{}
		if where != "" {
			where = url.QueryEscape(where)
			newParams.Where = &where
		}
		funcRes, err := cubClientNew.InvokeFunctionsOnOrgWithResponse(ctx, newParams, *newBody)
		if IsAPIError(err, funcRes) {
			return fmt.Errorf("failed to invoke function on org: %s", InterpretErrorGeneric(err, funcRes).Error())
		}
		resp = funcRes.JSON200
	} else {
		newParams := &goclientnew.InvokeFunctionsParams{}
		if where != "" {
			where = url.QueryEscape(where)
			newParams.Where = &where
		}
		funcRes, err := cubClientNew.InvokeFunctionsWithResponse(ctx, uuid.MustParse(selectedSpaceID), newParams, *newBody)
		if IsAPIError(err, funcRes) {
			return InterpretErrorGeneric(err, funcRes)
		}
		resp = funcRes.JSON200
	}

	// if server 200 empty-response
	if resp == nil {
		resp = &[]goclientnew.FunctionInvocationResponse{}
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
					tprint(resp.Output)
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
			tprint("Awaiting triggers...")
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
