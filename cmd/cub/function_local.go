// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	functionlogger "github.com/labstack/gommon/log"

	"github.com/confighub/sdk/function"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/workerapi"
	"github.com/spf13/cobra"
)

var functionLocalCmd = &cobra.Command{
	Use:   "local <filename> <function> [<arg1> ...]",
	Short: "Execute a function locally on input data for debugging",
	Long: getCommandHelp(`Execute a ConfigHub function locally on input data for debugging purposes.

This command allows you to test functions without connecting to the ConfigHub server.
It reads configuration data from a file and executes the specified function locally.

The first positional argument must be a filename containing the configuration data.
Use "-" to read from stdin.

Function argument values may be simply listed in order so long as no optional parameters are skipped.
Parameters may be specified out of order using the "--parameter-name=value" syntax. These are not cub
flags, so specify "--" before the filename if using that syntax for function arguments.

Examples:
`+"```"+`
  # Execute set-image function on local file
  cub function local deployment.yaml set-image nginx nginx:1.25-alpine --toolchain Kubernetes/YAML

  # Execute yq function on local file to extract a field
  cub function local config.yaml yq '.spec.replicas' --toolchain Kubernetes/YAML

  # Execute function with named parameters
  cub function local -- deployment.yaml set-int-path --apiVersion=apps/v1 --kind=Deployment --path=spec.replicas --value=3

  # Read from stdin
  cat deployment.yaml | cub function local - set-replicas 3 --toolchain Kubernetes/YAML
`+"```"+`
`, ""),
	Args: cobra.MinimumNArgs(2),
	RunE: functionLocalCommandRun,
	// Skip the default pre-run which tries to connect to the server
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

var localToolchainType string
var localDataOnly bool
var localOutputOnly bool

func init() {
	functionLocalCmd.Flags().StringVar(&localToolchainType, "toolchain", "Kubernetes/YAML", "Toolchain type for the function execution")
	functionLocalCmd.Flags().BoolVar(&localDataOnly, "data-only", false, "show config data without other response details")
	functionLocalCmd.Flags().BoolVar(&localOutputOnly, "output-only", false, "show output without other response details")
	functionLocalCmd.Flags().BoolVar(&outputRaw, "output-json", false, "show output as raw JSON")
	functionLocalCmd.Flags().StringVar(&outputJQ, "output-jq", "", "apply jq to output JSON")
	addStandardDisplayFlags(functionLocalCmd)
	functionCmd.AddCommand(functionLocalCmd)
}

func invokeLocalFunction(inputData []byte, functionName string, functionArgs []string, toolchainTypeString string) (*api.FunctionInvocationResponse, error) {
	functionlogger.SetLevel(functionlogger.ERROR)

	// Parse the function arguments using the existing helper
	invocation := initializeFunctionInvocation(functionName, functionArgs)

	// Convert toolchain type string to workerapi.ToolchainType
	toolchainTypeEnum := workerapi.ToolchainType(toolchainTypeString)

	// Create function executor
	functionExecutor := function.NewStandardExecutor()

	// Get registered functions to validate the function exists
	registeredFunctions := functionExecutor.RegisteredFunctions()
	if toolchainFunctions, ok := registeredFunctions[toolchainTypeEnum]; ok {
		if _, functionExists := toolchainFunctions[functionName]; !functionExists {
			return nil, fmt.Errorf("function '%s' not found for toolchain '%s'", functionName, toolchainTypeString)
		}
	} else {
		return nil, fmt.Errorf("no functions registered for toolchain '%s'", toolchainTypeString)
	}

	// Convert function arguments from goclientnew.FunctionArgument to api.FunctionArgument
	var apiArgs []api.FunctionArgument
	for _, arg := range invocation.Arguments {
		apiArg := api.FunctionArgument{}

		// Handle parameter name if present
		if arg.ParameterName != nil {
			apiArg.ParameterName = *arg.ParameterName
		}

		// Handle value - need to extract the actual string value
		if arg.Value != nil {
			// The Value field contains the actual value as a union type
			// We need to get the string representation
			valueBytes, err := json.Marshal(arg.Value)
			if err == nil {
				var strValue string
				// Try to unmarshal as string first
				if err := json.Unmarshal(valueBytes, &strValue); err == nil {
					apiArg.Value = strValue
				} else {
					// If not a string, use the raw JSON string
					apiArg.Value = string(valueBytes)
				}
			}
		}

		apiArgs = append(apiArgs, apiArg)
	}

	// Create function invocation
	functionInvocations := []api.FunctionInvocation{
		{
			FunctionName: functionName,
			Arguments:    apiArgs,
		},
	}

	// Create function invocation request
	invocationRequest := &api.FunctionInvocationRequest{
		FunctionContext: api.FunctionContext{
			ToolchainType: toolchainTypeEnum,
		},
		ConfigData:          inputData,
		FunctionInvocations: functionInvocations,
	}

	// Execute the function
	ctx := context.Background()
	response, err := functionExecutor.Invoke(ctx, invocationRequest)
	if err != nil {
		return nil, fmt.Errorf("function execution failed: %w", err)
	}
	return response, nil
}

func functionLocalCommandRun(cmd *cobra.Command, args []string) error {
	// First argument is the filename
	filename := args[0]

	// Read the input data
	inputData, err := fetchContent(filename)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	// Second argument is the function name
	functionName := args[1]

	// Remaining arguments are function parameters
	functionArgs := args[2:]

	response, err := invokeLocalFunction(inputData, functionName, functionArgs, localToolchainType)
	if err != nil {
		return err
	}

	// Display the results
	displayLocalFunctionResults(response)

	return nil
}

func displayLocalFunctionResults(response *api.FunctionInvocationResponse) {
	// Check if any alternative output format is specified
	hasAltOutput := hasAlternativeOutput()

	// Handle data-only flag
	if localDataOnly {
		if len(response.ConfigData) > 0 {
			fmt.Print(string(response.ConfigData))
		}
		return
	}

	// Handle output-only flag
	if outputRaw || outputJQ != "" {
		localOutputOnly = true
	}
	if localOutputOnly {
		for outputType, outputData := range response.Outputs {
			if len(outputData) > 0 {
				outputBytes := outputData
				if strings.TrimSpace(string(outputBytes)) != "null" {
					displayFunctionOutputByType(string(outputType), outputBytes, true, len(response.Outputs) > 1)
				}
			}
		}
		return
	}

	// Handle alternative output formats
	if hasAltOutput {
		if jsonOutput {
			displayJSON(response)
		}
		if jq != "" {
			displayJQ(response)
		}
		if yamlOutput {
			displayYAML(response)
		}
		if yq != "" {
			displayYQ(response)
		}
		return
	}

	// Default output
	if !quiet {
		fmt.Printf("Success: %v\n", response.Success)
		if !response.Success && len(response.ErrorMessages) > 0 {
			fmt.Printf("Error: %s\n", strings.Join(response.ErrorMessages, "; "))
		}
	}

	// Display config data if modified
	if len(response.ConfigData) > 0 && len(response.Mutators) > 0 {
		if !quiet {
			fmt.Println("\nMODIFIED CONFIG DATA:")
			fmt.Println("--------------------")
		}
		fmt.Print(string(response.ConfigData))
	}

	// Display function output if present
	if len(response.Outputs) > 0 {
		if !quiet {
			fmt.Println("\nFUNCTION OUTPUT:")
			fmt.Println("----------------")
		}
		for outputType, outputData := range response.Outputs {
			if len(outputData) > 0 {
				outputBytes := outputData
				if strings.TrimSpace(string(outputBytes)) != "null" {
					displayFunctionOutputByType(string(outputType), outputBytes, false, len(response.Outputs) > 1)
				}
			}
		}
	}
}

func displayFunctionOutputByType(outputType string, outputBytes []byte, outputOnly bool, multipleTypes bool) {
	if multipleTypes && !outputRaw && outputJQ == "" {
		fmt.Printf("%s:\n", outputType)
	}

	// Handle different output types
	switch api.OutputType(outputType) {
	case api.OutputTypeYAML:
		var payload api.YAMLPayload
		if err := json.Unmarshal(outputBytes, &payload); err == nil {
			fmt.Print(payload.Payload)
		} else {
			fmt.Print(string(outputBytes))
		}
	case api.OutputTypeAttributeValueList:
		var payload api.AttributeValueList
		if err := json.Unmarshal(outputBytes, &payload); err == nil {
			if outputRaw {
				displayJSON(payload)
			} else if outputJQ != "" {
				displayJQ(payload)
			} else {
				for _, attr := range payload {
					fmt.Printf("%v %s %s %s %s\n", attr.Value, attr.DataType, attr.Path, attr.ResourceName, attr.ResourceType)
				}
			}
		} else {
			fmt.Print(string(outputBytes))
		}
	case api.OutputTypeValidationResult, api.OutputTypeValidationResultList:
		var payload api.ValidationResultList
		if err := json.Unmarshal(outputBytes, &payload); err == nil {
			if outputRaw {
				displayJSON(payload)
			} else if outputJQ != "" {
				displayJQ(payload)
			} else {
				for _, result := range payload {
					details := ""
					if len(result.Details) > 0 {
						details = ": " + strings.Join(result.Details, ", ")
					}
					fmt.Printf("Passed: %v%s\n", result.Passed, details)
					if len(result.FailedAttributes) > 0 {
						fmt.Println("Failed Attributes:")
						for _, attr := range result.FailedAttributes {
							fmt.Printf("  %v %s %s %s %s\n", attr.Value, attr.DataType, attr.Path, attr.ResourceName, attr.ResourceType)
						}
					}
				}
			}
		} else {
			// Try single result
			var singleResult api.ValidationResult
			if err := json.Unmarshal(outputBytes, &singleResult); err == nil {
				if outputRaw {
					displayJSON(payload)
				} else if outputJQ != "" {
					displayJQ(payload)
				} else {
					details := ""
					if len(singleResult.Details) > 0 {
						details = ": " + strings.Join(singleResult.Details, ", ")
					}
					fmt.Printf("Passed: %v%s\n", singleResult.Passed, details)
					if len(singleResult.FailedAttributes) > 0 {
						fmt.Println("Failed Attributes:")
						for _, attr := range singleResult.FailedAttributes {
							fmt.Printf("  %v %s %s %s %s\n", attr.Value, attr.DataType, attr.Path, attr.ResourceName, attr.ResourceType)
						}
					}
				}
			} else {
				fmt.Print(string(outputBytes))
			}
		}
	case api.OutputTypeResourceInfoList:
		var payload api.ResourceInfoList
		if err := json.Unmarshal(outputBytes, &payload); err == nil {
			if outputRaw {
				displayJSON(payload)
			} else if outputJQ != "" {
				displayJQ(payload)
			} else {
				for _, resource := range payload {
					fmt.Printf("%s %s\n", resource.ResourceName, resource.ResourceType)
				}
			}
		} else {
			fmt.Print(string(outputBytes))
		}
	case api.OutputTypeResourceList:
		var payload api.ResourceList
		if err := json.Unmarshal(outputBytes, &payload); err == nil {
			if outputRaw {
				displayJSON(payload)
			} else if outputJQ != "" {
				displayJQ(payload)
			} else {
				for _, resource := range payload {
					fmt.Printf("%s %s:\n", resource.ResourceName, resource.ResourceType)
					fmt.Println(resource.ResourceBody)
				}
			}
		} else {
			fmt.Print(string(outputBytes))
		}
	default:
		// Try to format as JSON
		var jsonData interface{}
		if err := json.Unmarshal(outputBytes, &jsonData); err == nil {
			displayJSON(jsonData)
		} else {
			fmt.Print(string(outputBytes))
		}
	}
}
