// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:               "run",
	Short:             "invoke a function",
	Long:              getCommandHelp(`invoke a function`, ""),
	PersistentPreRunE: spacePreRunE,
	RunE:              RunRunCmd,
}

var reload = false
var reset = false

func init() {
	runCmd.Flags().BoolVar(&reload, "reload", false, "Reload the function list")
	runCmd.Flags().BoolVar(&reload, "reset", false, "Reset the function list")

	addSpaceFlags(runCmd)
	runCmd.PersistentFlags().StringVar(&workerSlug, "worker", "", "worker to execute the function")
	runCmd.PersistentFlags().StringVar(&showSection, "show", "",
		"Select which part of the function response to display. One of: output, values, data")
	runCmd.PersistentFlags().BoolVar(&outputOnly, "output-only", false, "show output without other response details")
	_ = runCmd.PersistentFlags().MarkDeprecated("output-only", "use --show output")
	runCmd.PersistentFlags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	_ = runCmd.PersistentFlags().MarkDeprecated("data-only", "use --show data")
	runCmd.PersistentFlags().StringVar(&where, "where", "", "where filter")
	runCmd.PersistentFlags().StringVar(&filter, "filter", "", "filter to apply (slug, space/filter, or UUID)")
	runCmd.PersistentFlags().StringVar(&resourceType, "resource-type", "", "resource-type filter")
	runCmd.PersistentFlags().StringVar(&whereData, "where-data", "", "where data filter")
	runCmd.PersistentFlags().StringVar(&whereResource, "where-resource", "", "filter which resources the function operates on")
	runCmd.PersistentFlags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	//These flags don't make sense in the context of cub run because it assumes one function will be explicitly specified on the command line
	//runCmd.PersistentFlags().StringSliceVar(&functionTriggerIdentifiers, "trigger", []string{}, "execute triggers by UUID, slug, or space/slug (can be repeated or comma-separated)")
	//runCmd.PersistentFlags().StringSliceVar(&functionInvocationIdentifiers, "invocation", []string{}, "execute invocations by UUID, slug, or space/slug (can be repeated or comma-separated)")
	runCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "No output")
	runCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "",
		"Output format. One of: json, yaml, name, wide, mutations, jq=<expr>, yq=<expr>, custom-columns=<spec>")
	runCmd.PersistentFlags().StringVar(&jq, "jq", "", "jq expression")
	_ = runCmd.PersistentFlags().MarkDeprecated("jq", "use -o jq=<expr>")
	runCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "JSON output")
	_ = runCmd.PersistentFlags().MarkDeprecated("json", "use -o json")
	runCmd.PersistentFlags().StringVar(&yq, "yq", "", "yq expression")
	_ = runCmd.PersistentFlags().MarkDeprecated("yq", "use -o yq=<expr>")
	runCmd.PersistentFlags().BoolVar(&yamlOutput, "yaml", false, "YAML output")
	_ = runCmd.PersistentFlags().MarkDeprecated("yaml", "use -o yaml")
	runCmd.PersistentFlags().BoolVar(&wait, "wait", false, "wait for completion")
	runCmd.PersistentFlags().StringVar(&outputJQ, "output-jq", "", "apply jq to output JSON")
	_ = runCmd.PersistentFlags().MarkDeprecated("output-jq", "use --show output -o jq=<expr>")
	runCmd.PersistentFlags().StringVar(&changeDescription, "change-desc", "", "change description")
	runCmd.PersistentFlags().StringVar(&functionChangesetSlug, "changeset", "", "changeset to associate units with")
	runCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "dry run mode: execute functions but skip updating configuration data")
	runCmd.PersistentFlags().StringVar(&functionToolchainType, "toolchain", "Kubernetes/YAML", "Toolchain type for the function invocations")
	runCmd.PersistentFlags().StringVar(&functionLiveStateType, "livestate-type", "", "Invoke the function on the live state and use the flag value as the toolchain type for live state.")
	runCmd.PersistentFlags().StringVar(&executorSpace, "executor-space", "", "Space ID or slug whose executor to use for builtin functions (org-level only)")
	runCmd.PersistentFlags().BoolVar(&displayMutations, "display-mutations", false, "display resource mutations")
	_ = runCmd.PersistentFlags().MarkDeprecated("display-mutations", "use -o mutations")

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
				Use:         cmdDef.FunctionName,
				Short:       fmt.Sprintf("%s%s Supported toolchains: %s", description, functionAttributes, toolchain),
				Annotations: map[string]string{"OrgLevel": ""},
				RunE: func(cmd *cobra.Command, args []string) error {
					// Parse filter parameter
					filterID, err := parseFilterFlag(filter)
					if err != nil {
						return err
					}

					// Check for mutual exclusivity between --unit and --where flags
					if len(unitIdentifiers) > 0 && where != "" {
						return fmt.Errorf("--unit and --where flags are mutually exclusive")
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
						changesetUUID, err := parseChangeSetSlug(functionChangesetSlug)
						if err != nil {
							return err
						}
						changesetID = changesetUUID.String()
						// Add changeset constraint to WHERE clause
						effectiveWhere = addChangeSetIDToWhereClause(effectiveWhere, changesetID)
					}

					newBody := newFunctionInvocationsRequest()

					var funcParams []goclientnew.FunctionArgument

					// Handle positional arguments first
					for i, param := range cmdDef.Parameters {
						if i < len(args) {
							// Use positional argument
							value := goclientnew.FunctionArgument_Value{}
							value.FromFunctionArgumentValue0(args[i])
							funcParams = append(funcParams, goclientnew.FunctionArgument{
								ParameterName: &param.ParameterName,
								Value:         &value,
							})
						} else {
							// Use flag value
							p := param.ParameterName
							value := goclientnew.FunctionArgument_Value{}
							var hasValue bool
							switch param.DataType {
							case "int":
								v, _ := cmd.Flags().GetInt(p)
								if v != 0 || cmd.Flags().Changed(p) {
									value.FromFunctionArgumentValue1(int64(v))
									hasValue = true
								}
							case "bool":
								v, _ := cmd.Flags().GetBool(p)
								if v || cmd.Flags().Changed(p) {
									value.FromFunctionArgumentValue2(v)
									hasValue = true
								}
							default:
								v, _ := cmd.Flags().GetString(p)
								if v != "" || cmd.Flags().Changed(p) {
									value.FromFunctionArgumentValue0(v)
									hasValue = true
								}
							}

							// Only add argument if value was provided or parameter is required
							if hasValue || param.Required {
								if !hasValue && param.Required {
									return fmt.Errorf("required parameter '%s' not provided", param.ParameterName)
								}
								funcParams = append(funcParams, goclientnew.FunctionArgument{
									ParameterName: &param.ParameterName,
									Value:         &value,
								})
							}
						}
					}

					// Handle VarArgs - any extra positional arguments beyond the parameter count
					if cmdDef.VarArgs && len(args) > len(cmdDef.Parameters) {
						lastParam := cmdDef.Parameters[len(cmdDef.Parameters)-1]
						for i := len(cmdDef.Parameters); i < len(args); i++ {
							value := goclientnew.FunctionArgument_Value{}
							value.FromFunctionArgumentValue0(args[i])
							funcParams = append(funcParams, goclientnew.FunctionArgument{
								ParameterName: &lastParam.ParameterName,
								Value:         &value,
							})
						}
					}
					newBody.FunctionInvocations = &[]goclientnew.FunctionInvocation{
						{
							FunctionName: cmdDef.FunctionName,
							Arguments:    funcParams,
						},
					}

					// Save prior HeadMutationNums if displaying mutations
					var priorHeadMutationNums map[string]priorUnitInfo
					if shouldDisplayMutations() {
						priorHeadMutationNums = savePriorUnitInfoFromWhere(effectiveWhere, filterID)
					}

					invokeArgs := &invokeArgs{
						Where:        effectiveWhere,
						FilterID:     filterID,
						ResourceType: resourceType,
						WhereData:    whereData,
						DryRun:       dryRun,
						ChangeSetID:  changesetUUID,
						Body:         newBody,
					}
					respMsgs, err := invokeFunctionsOnUnits(invokeArgs)
					if err != nil {
						return err
					}

					// Shouldn't happen
					if respMsgs == nil {
						respMsgs = &[]goclientnew.FunctionInvocationsResponse{}
					}
					if !renderFunctionResponse(respMsgs) {
						outputFunctionInvocationResponse(respMsgs)
					}
					if wait {
						if !quiet {
							tprint("Awaiting triggers...")
						}
						// Wait one at a time
						for _, respMsg := range *respMsgs {
							unitDetails, err := apiGetUnitInSpace(respMsg.UnitID.String(), respMsg.SpaceID.String(), "*") // get all fields for now
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
					if shouldDisplayMutations() {
						displayMutationsFromFunctionResponse(respMsgs, dryRun, priorHeadMutationNums, cmdDef.FunctionName)
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

				// Don't mark flags as required since parameters can be provided positionally
			}
			commands[cmdDef.FunctionName] = cmd
			runCmd.AddCommand(cmd)
		}
	}
}
