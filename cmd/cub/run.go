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

var runCmd = &cobra.Command{
	Use:               "run",
	Short:             "invoke a function",
	Long:              `invoke a function`,
	PersistentPreRunE: spacePreRunE,
	RunE:              RunRunCmd,
}

var reload = false
var reset = false

func init() {
	runCmd.Flags().BoolVar(&reload, "reload", false, "Reload the function list")
	runCmd.Flags().BoolVar(&reload, "reset", false, "Reset the function list")

	addSpaceFlags(runCmd)
	runCmd.PersistentFlags().BoolVar(&useWorker, "use-worker", false, "use the attached worker to execute the function")
	runCmd.PersistentFlags().BoolVar(&combine, "combine", false, "combine results")
	runCmd.PersistentFlags().BoolVar(&outputOnly, "output-only", false, "show output without other response details")
	runCmd.PersistentFlags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	runCmd.PersistentFlags().StringVar(&where, "where", "", "where filter")
	runCmd.PersistentFlags().StringVar(&jq, "jq", "", "jq expression")
	runCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "No output")
	runCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "JSON output")
	runCmd.PersistentFlags().BoolVar(&wait, "wait", false, "wait for completion")
	runCmd.PersistentFlags().StringVar(&outputJQ, "output-jq", "", "apply jq to output JSON")

	RegisterFunctionsAsCobraCommands()

	rootCmd.AddCommand(runCmd)
}

func RunRunCmd(cmd *cobra.Command, args []string) error {
	if !reload && !reset {
		tprint("Run with --reload or --reset to load and set up function list in CLI")
		return nil
	}

	if reset {
		// Clear previously saved functions, if any
		err := removeFunctions()
		failOnError(err)
	}

	// Preload builtin functions
	_, _, err := listAndSaveFunctions("", "", "")
	failOnError(err)
	tprint("Function list saved to %s", functionSpecFile)
	return nil
}

func RegisterFunctionsAsCobraCommands() {
	functionsByEntity, err := loadFunctions()
	// Fail silently because if the user doesn't need functions, it's ok they haven't been loaded yet
	if err != nil {
		return
	}
	functions, present := functionsByEntity[builtinFunctionKey]
	if !present {
		return
	}
	// TODO: Iterate through functions for other entities

	commands := map[string]*cobra.Command{}

	// Iterate through categories and commands
	for toolchain, cmds := range functions {

		for _, cmdDef := range cmds {
			// Deduplicate identical functions across ToolchainTypes
			cmd, alreadyRegistered := commands[cmdDef.FunctionName]
			if alreadyRegistered {
				cmd.Short += ", " + toolchain
				// TODO: Verify the functions are actually identical
				continue
			}
			description := strings.TrimSuffix(strings.TrimSpace(cmdDef.Description), ".") + "."
			functionAttributes := ""
			if cmdDef.Mutating {
				functionAttributes += " Mutating."
			}
			if cmdDef.Validating {
				functionAttributes += " Validating."
			}
			cmd = &cobra.Command{
				Use:   cmdDef.FunctionName,
				Short: fmt.Sprintf("%s%s Supported toolchains: %s", description, functionAttributes, toolchain),
				RunE: func(cmd *cobra.Command, args []string) error {
					newParams := &goclientnew.InvokeFunctionsParams{}
					newBody := newFunctionInvocationsRequest()

					if where != "" {
						where = url.QueryEscape(where)
						newParams.Where = &where
					}

					var funcParams []goclientnew.FunctionArgument
					for _, param := range cmdDef.Parameters {
						p := param.ParameterName
						value := goclientnew.FunctionArgument_Value{}
						switch param.DataType {
						case "int":
							v, _ := cmd.Flags().GetInt(p)
							value.FromFunctionArgumentValue1(int64(v))
						case "bool":
							v, _ := cmd.Flags().GetBool(p)
							value.FromFunctionArgumentValue2(v)
						default:
							v, _ := cmd.Flags().GetString(p)
							value.FromFunctionArgumentValue0(v)
						}
						funcParams = append(funcParams, goclientnew.FunctionArgument{
							ParameterName: &param.ParameterName,
							Value:         &value,
						})
					}
					newBody.FunctionInvocations = &[]goclientnew.FunctionInvocation{
						{
							FunctionName: cmdDef.FunctionName,
							Arguments:    funcParams,
						},
					}

					funcRes, err := cubClientNew.InvokeFunctionsWithResponse(ctx,
						uuid.MustParse(selectedSpaceID), newParams, *newBody)
					if IsAPIError(err, funcRes) {
						return InterpretErrorGeneric(err, funcRes)
					}
					respMsgs := funcRes.JSON200
					// Shouldn't happen
					if respMsgs == nil {
						respMsgs = &[]goclientnew.FunctionInvocationResponse{}
					}
					if !quiet {
						outputFunctionInvocationResponse(respMsgs)
					}
					if jsonOutput {
						displayJSON(respMsgs)
					}
					if jq != "" {
						displayJQ(respMsgs)
					}
					if outputJQ != "" {
						for _, respMsg := range *respMsgs {
							if len(respMsg.Output) != 0 {
								outputBytes, err := base64.StdEncoding.DecodeString(respMsg.Output)
								if err != nil {
									return err
								}
								if strings.TrimSpace(string(outputBytes)) != "null" {
									displayJQForBytes(outputBytes, outputJQ)
								}
							}
						}
					}
					if wait {
						if !quiet {
							tprint("Awaiting triggers...")
						}
						// Wait one at a time
						for _, respMsg := range *respMsgs {
							unitDetails, err := apiGetUnit(respMsg.UnitID.String())
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
				},
			}

			// Add parameters as flags
			for _, param := range cmdDef.Parameters {
				desc := param.Description
				if param.Required {
					desc = "(required) " + desc
				}

				switch param.DataType {
				case "string":
					cmd.Flags().String(param.ParameterName, "", desc)
				case "int":
					cmd.Flags().Int(param.ParameterName, 0, desc)
				case "bool":
					cmd.Flags().Bool(param.ParameterName, false, desc)
				default:
					cmd.Flags().String(param.ParameterName, "", desc)
				}

				// Mark required flags
				if param.Required {
					_ = cmd.MarkFlagRequired(param.ParameterName)
				}
			}
			commands[cmdDef.FunctionName] = cmd
			runCmd.AddCommand(cmd)
		}
	}
}
