// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/confighub/sdk/function/api"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var functionDoCmd = &cobra.Command{
	Use:   "do <function> [<arg1> ...]",
	Short: "Invoke one function",
	Long: `Invoke a function on units in a space. Functions can be used to modify, validate, or query unit configurations.

To get a list of supported functions, run:
  cub function list

Example Functions:
  - set-image: Update container image in a deployment
  - set-int-path: Set an integer value at a specific path in the configuration
  - get-replicas: Get the number of replicas for deployments
  - set-replicas: Set the number of replicas for deployments
  - where-filter: Filter units based on a condition
  - cel-validate: Validate resources using CEL expressions

Examples:
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

  # Validate deployment replicas using CEL
  cub function do --space my-space \
    cel-validate 'r.kind != "Deployment" || r.spec.replicas > 1' \
    --quiet \
    --output-jq '.[].Passed' \
    --jq '.[].UnitID'`,
	Args:        cobra.MinimumNArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        functionDoCommandRun,
}

var useWorker bool
var combine bool
var outputOnly bool
var outputValuesOnly bool
var outputRaw bool
var dataOnly bool
var outputJQ string

func init() {
	functionDoCmd.Flags().BoolVar(&useWorker, "use-worker", false, "use the attached worker to execute the function")
	functionDoCmd.Flags().BoolVar(&combine, "combine", false, "combine results")
	functionDoCmd.Flags().BoolVar(&outputOnly, "output-only", false, "show output without other response details")
	functionDoCmd.Flags().BoolVar(&outputRaw, "output-raw", false, "show output as raw JSON")
	functionDoCmd.Flags().BoolVar(&outputValuesOnly, "output-values-only", false, "show output values (from functions returning AttributeValueList) without other response details")
	functionDoCmd.Flags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	// Same flag as unit update
	functionDoCmd.Flags().StringVar(&changeDescription, "change-desc", "", "change description")
	enableWhereFlag(functionDoCmd)
	enableQuietFlag(functionDoCmd)
	enableJsonFlag(functionDoCmd)
	enableJqFlag(functionDoCmd)
	enableWaitFlag(functionDoCmd)
	functionDoCmd.Flags().StringVar(&outputJQ, "output-jq", "", "apply jq to output JSON")
	functionCmd.AddCommand(functionDoCmd)
}

func newFunctionInvocationsRequest() *goclientnew.FunctionInvocationsRequest {
	req := &goclientnew.FunctionInvocationsRequest{}
	req.CastStringArgsToScalars = true
	req.NumFilters = 0
	req.StopOnError = false
	req.UseFunctionWorker = useWorker
	req.CombineResults = combine
	req.ChangeDescription = changeDescription
	return req
}

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

			funcArgs = append(funcArgs, goclientnew.FunctionArgument{
				ParameterName: &paramName,
				Value:         &goclientnew.FunctionArgument_Value{},
			})
			funcArgs[len(funcArgs)-1].Value.FromFunctionArgumentValue0(value)

		} else if namedArgMode {
			// Once we've seen a named argument, all subsequent arguments must be named
			failOnError(fmt.Errorf("positional argument '%s' cannot follow named arguments", arg))
		} else {
			// This is a positional argument - no ParameterName
			funcArgs = append(funcArgs, goclientnew.FunctionArgument{
				Value: &goclientnew.FunctionArgument_Value{},
			})
			funcArgs[len(funcArgs)-1].Value.FromFunctionArgumentValue0(arg)
		}
	}

	return funcArgs
}

func initializeFunctionInvocation(functionName string, args []string) *goclientnew.FunctionInvocation {
	funcArgs := parseFunctionArguments(args)
	return &goclientnew.FunctionInvocation{
		FunctionName: functionName,
		Arguments:    funcArgs,
	}
}

func initializeFunctionInvocationsRequest(cmdArgs []string) *goclientnew.FunctionInvocationsRequest {
	req := newFunctionInvocationsRequest()
	functionName := cmdArgs[0]
	invokeArgs := cmdArgs[1:]
	invocation := initializeFunctionInvocation(functionName, invokeArgs)
	req.FunctionInvocations = &[]goclientnew.FunctionInvocation{*invocation}
	return req
}

func functionDoCommandRun(cmd *cobra.Command, args []string) error {
	var resp *[]goclientnew.FunctionInvocationResponse

	// Functions operate on lists of units. That makes it harder to know what ToolchainType(s)
	// are having functions invoked on them and what workers the functions might be running in.
	// There could be multiple of each.

	// That makes it more difficult to look up the FunctionSignature in order to validate arguments,
	// and support optional arguments. run sort of merges all functions together.

	newBody := initializeFunctionInvocationsRequest(args)
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
	if outputValuesOnly {
		for _, resp := range *resp {
			if len(resp.Output) != 0 {
				outputBytes, err := base64.StdEncoding.DecodeString(resp.Output)
				if err != nil {
					tprint(resp.Output)
					failOnError(fmt.Errorf("%s: Failed to decode output", err.Error()))
				}
				if strings.TrimSpace(string(outputBytes)) != "null" {
					// Try to decode as AttributeValueList
					var attrValueList api.AttributeValueList
					err = json.Unmarshal(outputBytes, &attrValueList)
					if err != nil {
						tprint(string(outputBytes))
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
		if !quiet && !dataOnly && !outputOnly && !outputValuesOnly {
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

func outputFunctionInvocationResponse(respMsgs *[]goclientnew.FunctionInvocationResponse) {
	for _, respMsg := range *respMsgs {
		if !quiet && !outputOnly && !dataOnly && !outputValuesOnly {
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
		if dataOnly || ((!quiet && !outputOnly && !outputValuesOnly) && len(respMsg.ConfigData) != 0 && len(respMsg.Mutators) > 0) {
			// Don't use detailView to print the data because it pads the entire width with spaces.
			if !dataOnly {
				tprint("CONFIGDATA\n---------\n")
			}
			data, err := base64.StdEncoding.DecodeString(respMsg.ConfigData)
			if err != nil {
				failOnError(fmt.Errorf("%s: Failed to decode config data", err.Error()))
			}
			tprint(string(data))
		}
		if (outputOnly || (!quiet && !dataOnly && !outputValuesOnly)) && len(respMsg.Output) != 0 {
			// TODO: handle more output types
			outputBytes, err := base64.StdEncoding.DecodeString(respMsg.Output)
			if err != nil {
				tprint(respMsg.Output)
				failOnError(fmt.Errorf("%s: Failed to decode output", err.Error()))
			}
			if strings.TrimSpace(string(outputBytes)) == "null" {
				continue
			}
			// Don't use detailView to print the output because it pads the entire width with spaces.
			if !outputOnly && !outputValuesOnly {
				tprint("OUTPUT\n------\n")
			}
			switch respMsg.OutputType {
			case string(api.OutputTypeYAML):
				var payload api.YAMLPayload
				err := json.Unmarshal(outputBytes, &payload)
				// If there's an error print the raw output
				if err != nil || outputRaw {
					tprint(string(outputBytes))
				} else {
					tprint(payload.Payload)
				}
			case string(api.OutputTypeAttributeValueList):
				var payload api.AttributeValueList
				err := json.Unmarshal(outputBytes, &payload)
				// If there's an error print the raw output
				if err != nil || outputRaw {
					tprint(string(outputBytes))
				} else {
					for i := range payload {
						tprint("%v %s %s %s %s", payload[i].Value, payload[i].DataType, payload[i].Path, payload[i].ResourceName, payload[i].ResourceType)
					}
				}
			case string(api.OutputTypeValidationResultList), string(api.OutputTypeValidationResult):
				var payload api.ValidationResultList
				err := json.Unmarshal(outputBytes, &payload)
				if err != nil || outputRaw {
					// Try parsing as a single result
					// TODO: check CombineValidationResults
					var payload api.ValidationResult
					err := json.Unmarshal(outputBytes, &payload)
					// If there's an error print the raw output
					if err != nil || outputRaw {
						tprint(string(outputBytes))
					} else {
						// TODO: Factor this out
						details := ""
						for j, detail := range payload.Details {
							if j > 0 {
								details += ","
							}
							details += " " + detail
						}
						tprint("%v%s", payload.Passed, details)
						tprint("Attributes:")
						for j := range payload.FailedAttributes {
							tprint("%v %s %s %s %s", payload.FailedAttributes[j].Value, payload.FailedAttributes[j].DataType,
								payload.FailedAttributes[j].Path, payload.FailedAttributes[j].ResourceName, payload.FailedAttributes[j].ResourceType)
						}
					}
				} else {
					for i := range payload {
						details := ""
						for j, detail := range payload[i].Details {
							if j > 0 {
								details += ","
							}
							details += " " + detail
						}
						tprint("%v %d%s", payload[i].Passed, payload[i].Index, details)
						tprint("Attributes:")
						for j := range payload[i].FailedAttributes {
							tprint("%v %s %s %s %s", payload[i].FailedAttributes[j].Value, payload[i].FailedAttributes[j].DataType,
								payload[i].FailedAttributes[j].Path, payload[i].FailedAttributes[j].ResourceName, payload[i].FailedAttributes[j].ResourceType)
						}
					}
				}
			case string(api.OutputTypeResourceInfoList):
				var payload api.ResourceInfoList
				err := json.Unmarshal(outputBytes, &payload)
				// If there's an error print the raw output
				if err != nil || outputRaw {
					tprint(string(outputBytes))
				} else {
					for i := range payload {
						tprint("%s %s", payload[i].ResourceName, payload[i].ResourceType)
					}
				}
			case string(api.OutputTypeResourceList):
				var payload api.ResourceList
				err := json.Unmarshal(outputBytes, &payload)
				// If there's an error print the raw output
				if err != nil || outputRaw {
					tprint(string(outputBytes))
				} else {
					for i := range payload {
						tprint("%s %s:", payload[i].ResourceName, payload[i].ResourceType)
						tprint("%s", payload[i].ResourceBody)
					}
				}
			default:
				// Output should be JSON, but if there's an error print the raw output
				var out bytes.Buffer
				err := json.Indent(&out, outputBytes, "", "  ")
				if err != nil || outputRaw {
					tprint(respMsg.Output)
				} else {
					tprint(out.String())
				}
			}
		}
	}
}
