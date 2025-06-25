// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	// TODO: the API mechanism will change when we decide/build the "real" webhook model.
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/client"
	"github.com/confighub/sdk/workerapi"
)

func parseArguments(args []string) []api.FunctionArgument {
	var funcArgs []api.FunctionArgument
	namedArgMode := false
	
	for _, arg := range args {
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			// This is a named argument
			namedArgMode = true
			parts := strings.SplitN(arg, "=", 2)
			paramName := strings.TrimPrefix(parts[0], "--")
			value := parts[1]
			
			funcArgs = append(funcArgs, api.FunctionArgument{
				ParameterName: paramName,
				Value:         value,
			})
			
		} else if namedArgMode {
			// Once we've seen a named argument, all subsequent arguments must be named
			failOnError(fmt.Errorf("positional argument '%s' cannot follow named arguments", arg))
		} else {
			// This is a positional argument - no ParameterName
			funcArgs = append(funcArgs, api.FunctionArgument{
				Value: arg,
			})
		}
	}

	return funcArgs
}

func InvokeFunction(
	transportConfig *client.TransportConfig,
	toolchain workerapi.ToolchainType,
	data []byte,
	functionContext *api.FunctionContext,
	functionName string,
	args ...string,
) (*api.FunctionInvocationResponse, error) {

	// TODO: just rely on server validation?
	if !regexp.MustCompile(`^[a-z0-9-_]*$`).MatchString(functionName) {
		return nil, fmt.Errorf("function name '%s' contains invalid characters", functionName)
	}
	
	funcArgs := parseArguments(args)
	functions := []api.FunctionInvocation{{FunctionName: functionName, Arguments: funcArgs}}
	
	return client.InvokeFunctions(transportConfig, toolchain, api.FunctionInvocationRequest{
		ConfigData:               data,
		FunctionContext:          *functionContext,
		FunctionInvocations:      functions,
		CastStringArgsToScalars:  true,
		NumFilters:               0,
		StopOnError:              false,
		CombineValidationResults: true,
	})
}

func fakeFunctionContext(displayName string) *api.FunctionContext {
	// Ensure these IDs are deterministic for testing.
	unitID := uuid.MustParse("5837950a-619e-44da-9b75-f957c2aee14c")
	spaceID := uuid.MustParse("c73bbc39-7ad1-4f32-aba0-1ef0789c9571")
	spaceSlug := "DeepSpace"
	orgID := uuid.MustParse("362c7a01-ba2d-40f6-9454-805c9a4fbbbe")
	functionContext := api.FunctionContext{
		UnitDisplayName: displayName,
		UnitSlug:        strings.ToLower(displayName), // note this doesn't convert spaces, punctuation, etc.
		UnitID:          unitID,
		SpaceID:         spaceID,
		SpaceSlug:       spaceSlug,
		OrganizationID:  orgID,
		RevisionNum:     1,
		New:             true,
	}
	return &functionContext
}

func newDoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "do <filename or - for stdin> <unit name> <function name> [<arg1> ...]",
		Short: "Invoke one function",
		Args:  cobra.MinimumNArgs(3),
		Run: func(_ /*cmd*/ *cobra.Command, args []string) {
			if dataOnly && outputOnly {
				failOnError(fmt.Errorf("cannot specify both --data-only and --output-only"))
			}
			// Read test payload
			var content []byte
			if args[0] == "-" {
				content = readStdin()
			} else {
				content = readFile(args[0])
			}
			unitName := args[1]
			if !regexp.MustCompile(`^[a-zA-Z0-9-_ .()@#]*$`).MatchString(unitName) {
				failOnError(fmt.Errorf("unit name '%s' contains invalid characters", unitName))
			}
			functionName := args[2]
			invokeArgs := args[3:]

			respMsg, err := InvokeFunction(transportConfig, toolchain, content, fakeFunctionContext(unitName), functionName, invokeArgs...)
			failOnError(err)
			outputFunctionInvocationResponse(content, respMsg)
		},
	}
	cmd.Flags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	cmd.Flags().BoolVar(&outputOnly, "output-only", false, "show function output only")

	return cmd
}

var dataOnly bool
var outputOnly bool
var numFilters int
var stop bool

func newDoSeqCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doseq <filename or - for stdin> <unit name> <FunctionInvocationList>",
		Short: "Invoke a sequence of functions",
		Args:  cobra.ExactArgs(3),
		Run: func(_ /*cmd*/ *cobra.Command, args []string) {
			// Read test payload
			var content []byte
			if args[0] == "-" {
				content = readStdin()
			} else {
				content = readFile(args[0])
			}
			unitName := args[1]
			if !regexp.MustCompile(`^[a-zA-Z0-9-_ .()@#]*$`).MatchString(unitName) {
				failOnError(fmt.Errorf("unit name '%s' contains invalid characters", unitName))
			}
			var functionList api.FunctionInvocationList
			err := json.Unmarshal([]byte(args[2]), &functionList)
			failOnError(err)

			respMsg, err := client.InvokeFunctions(transportConfig, toolchain, api.FunctionInvocationRequest{
				FunctionContext:          *fakeFunctionContext(unitName),
				ConfigData:               content,
				CastStringArgsToScalars:  true,
				NumFilters:               numFilters,
				StopOnError:              stop,
				CombineValidationResults: true,
				FunctionInvocations:      functionList,
			})
			failOnError(err)
			outputFunctionInvocationResponse(content, respMsg)
		},
	}
	cmd.Flags().IntVar(&numFilters, "num-filters", 0, "number of validating functions to treat as filters")
	cmd.Flags().BoolVar(&stop, "stop", false, "stop on error")

	return cmd
}

func outputFunctionInvocationResponse(data []byte, respMsg *api.FunctionInvocationResponse) {
	if !dataOnly && !outputOnly {
		// Timestamps disrupt golden outputs
		// log.Info(fmt.Sprintf("Received %d bytes of config data\n", len(respMsg.ConfigData)))
		detail := detailView()
		detail.Append([]string{strings.ToUpper("Success"), fmt.Sprintf("%v", respMsg.Success)})
		if !respMsg.Success {
			messages := ""
			for _, msg := range respMsg.ErrorMessages {
				messages += msg + "; "
			}
			detail.Append([]string{strings.ToUpper("ErrorMessages"), messages})
		}
		detail.Render()
	}
	if (!bytes.Equal(respMsg.ConfigData, data) || dataOnly) && !outputOnly {
		// Don't use detailView to print the data because it pads the entire width with spaces.
		if !dataOnly {
			fmt.Print("CONFIGDATA\n---------\n")
		}
		fmt.Print(string(respMsg.ConfigData))
	}
	if !dataOnly && len(respMsg.Output) != 0 && string(respMsg.Output) != "null" {
		// Don't use detailView to print the output because it pads the entire width with spaces.
		if !outputOnly {
			fmt.Printf("OUTPUT\n------\n")
		}
		switch respMsg.OutputType {
		case api.OutputTypeYAML:
			var payload api.YAMLPayload
			err := json.Unmarshal(respMsg.Output, &payload)
			// If there's an error print the raw output
			if err != nil {
				fmt.Print(string(respMsg.Output))
			} else {
				fmt.Print(payload.Payload)
			}
		default:
			// Output should be JSON, but if there's an error print the raw output
			var out bytes.Buffer
			err := json.Indent(&out, respMsg.Output, "", "  ")
			if err != nil {
				fmt.Print(string(respMsg.Output))
			} else {
				fmt.Print(out.String())
			}
		}
	}
}

func readFile(fileName string) []byte {
	data, err := os.ReadFile(fileName)
	if err != nil {
		failOnError(err)
	}
	return data
}

func readStdin() []byte {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		failOnError(err)
	}
	return data
}
