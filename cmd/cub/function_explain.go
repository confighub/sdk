// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var functionExplainCmd = &cobra.Command{
	Use:   "explain <function>",
	Short: "Explain a function",
	Long:  getFunctionExplainHelp(),
	Args:  cobra.ExactArgs(1),
	RunE:  functionExplainCmdRun,
}

func getFunctionExplainHelp() string {
	baseHelp := `Explain details about a function you have access to in this space`
	agentContext := `Critical for understanding function parameters before execution.

Agent workflow:
1. First run 'function list' to discover available functions
2. Use 'function explain FUNCTION_NAME' to understand parameters and behavior
3. Review the generated usage string for proper invocation syntax

Key information provided:
- Function signature with required and optional parameters
- Behavior flags (Mutating, Validating, Hermetic, Idempotent)
- Parameter types, constraints, and examples
- Affected resource types (which Kubernetes resources this function operates on)

Usage pattern:
  cub function explain set-image --toolchain Kubernetes/YAML

The output includes a ready-to-use command template showing required and optional parameters.

Important flags:
- --toolchain: Specify function toolchain (defaults to "Kubernetes/YAML")
- Use same --target, --worker, --unit flags as 'function list' if needed`

	return getCommandHelp(baseHelp, agentContext)
}

var functionExplainCmdArgs struct {
	targetSlug    string
	workerSlug    string
	unitSlug      string
	toolchainType string
}

func init() {
	functionExplainCmd.Flags().StringVar(&functionExplainCmdArgs.targetSlug, "target", "", "Target slug to explain a function for")
	functionExplainCmd.Flags().StringVar(&functionExplainCmdArgs.workerSlug, "worker", "", "Worker slug to explain a function for")
	functionExplainCmd.Flags().StringVar(&functionExplainCmdArgs.unitSlug, "unit", "", "Unit slug to explain a function for")
	functionExplainCmd.Flags().StringVar(&functionExplainCmdArgs.toolchainType, "toolchain", "Kubernetes/YAML", "Toolchain type to explain a function for")
	functionCmd.AddCommand(functionExplainCmd)
}

func functionExplainCmdRun(cmd *cobra.Command, args []string) error {
	_, funcs, err := listAndSaveFunctions(functionExplainCmdArgs.targetSlug, functionExplainCmdArgs.workerSlug, functionExplainCmdArgs.unitSlug)
	failOnError(err)

	toolchainType := functionExplainCmdArgs.toolchainType
	functionName := args[0]

	toolchainFuncs, found := funcs[toolchainType]
	if !found {
		failOnError(fmt.Errorf("Toolchain %s not found", toolchainType))
	}
	functionDetails, found := toolchainFuncs[functionName]
	if !found {
		failOnError(fmt.Errorf("Function %s not found", functionName))
	}

	if !quiet {
		displayFunctionDetails(toolchainType, functionName, &functionDetails)
	}
	if jsonOutput {
		displayJSON(functionDetails)
	}
	if jq != "" {
		displayJQ(functionDetails)
	}

	return nil
}

func displayFunctionDetails(toolchainType, functionName string, functionDetails *goclientnew.FunctionSignature) {
	view := tableView()
	view.Append([]string{"Toolchain Type", toolchainType})
	view.Append([]string{"Function Name", functionName})
	view.Append([]string{"Description", functionDetails.Description})
	view.Append([]string{"Required Parameters", fmt.Sprintf("%d", functionDetails.RequiredParameters)})
	view.Append([]string{"Varargs", fmt.Sprintf("%v", functionDetails.VarArgs)})
	view.Append([]string{"Mutating", fmt.Sprintf("%v", functionDetails.Mutating)})
	view.Append([]string{"Validating", fmt.Sprintf("%v", functionDetails.Validating)})
	view.Append([]string{"Hermetic", fmt.Sprintf("%v", functionDetails.Hermetic)})
	view.Append([]string{"Idempotent", fmt.Sprintf("%v", functionDetails.Idempotent)})
	if functionDetails.FunctionType != "" {
		view.Append([]string{"Function Type", functionDetails.FunctionType})
	}
	if functionDetails.AttributeName != "" {
		view.Append([]string{"Attribute Name", functionDetails.AttributeName})
	}
	if len(functionDetails.AffectedResourceTypes) != 0 {
		affectedTypes := ""
		for i, affectedType := range functionDetails.AffectedResourceTypes {
			if i > 0 {
				affectedTypes += ", "
			}
			affectedTypes += affectedType
		}
		view.Append([]string{"Affected Resource Types", affectedTypes})
	}

	// Generate usage string
	usageStr := fmt.Sprintf("Usage: cub function do -- %s", functionName)
	for _, param := range functionDetails.Parameters {
		if param.Required {
			usageStr += fmt.Sprintf(" <%s>", param.ParameterName)
		} else {
			usageStr += fmt.Sprintf(" [--%s=%s]", param.ParameterName, param.DataType)
		}
	}
	if functionDetails.VarArgs {
		usageStr += " ..."
	}
	tprintRaw(usageStr)
	tprintRaw("")

	view.Render()
	view = tableView()
	view.SetHeader([]string{"Parameter", "Name", "Data-Type", "Required", "Description", "Example", "Constraint"})
	rstring := map[bool]string{true: "required", false: "optional"}
	for i, param := range functionDetails.Parameters {
		constraint := ""
		if param.Regexp != "" {
			constraint = param.Regexp
		}
		// TODO: Min and Max aren't pointers, so it's not possible to differentiate set to zero from unset
		view.Append([]string{fmt.Sprintf("%d", i), param.ParameterName, param.DataType, rstring[param.Required], param.Description, param.Example, constraint})
	}
	view.Render()
}
