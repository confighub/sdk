// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var functionListCmd = &cobra.Command{
	Use:         "list",
	Short:       "List functions",
	Long:        getFunctionListHelp(),
	Args:        cobra.ExactArgs(0),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        functionListCmdRun,
}

func getFunctionListHelp() string {
	baseHelp := `List functions you have access to in this space`
	agentContext := `Essential for function discovery and understanding available operations.

Agent workflow:
1. Run 'function list' to see all available functions by toolchain type
2. Note the 'Mutating' and 'Validating' columns to understand function behavior
3. Use 'function explain FUNCTION_NAME' for detailed parameter information

Filter options:
- --where: SQL-inspired filter over FunctionSignature attributes. Supported attributes: ToolchainType, FunctionName, Description, RequiredParameters, VarArgs, Mutating, Validating, Hermetic, Idempotent, Replayable, FunctionType, AttributeName, and OutputInfo.OutputType / OutputInfo.ResultName / OutputInfo.Description. Example: --where "ToolchainType = 'Kubernetes/YAML' AND FunctionName LIKE '%image%'"
- --toolchain: Shortcut that is AND'd into --where as ToolchainType = '<toolchain>'
- --channel: Show functions the client of a channel serves

Function types:
- Mutating: false = Read-only inspection functions (safe to run repeatedly)
- Mutating: true = Modifies configuration data (changes unit state)
- Validating: true = Returns pass/fail validation results

Use -o name to get just function names for scripting.

Note: Filtered results (--where or --toolchain supplied) are not cached locally.`

	return getCommandHelp(baseHelp, agentContext)
}

var functionListCmdArgs struct {
	host          functionHost
	toolchainType string
}

func init() {
	functionListCmd.Flags().StringVar(&functionListCmdArgs.host.channel, "channel", "", "Channel whose served functions to list")
	functionListCmd.Flags().StringVar(&functionListCmdArgs.toolchainType, "toolchain", "", "Toolchain type to list functions for (AND'd into --where as ToolchainType = '<toolchain>')")
	enableWhereFlag(functionListCmd)
	addStandardListDisplayFlags(functionListCmd)
	functionCmd.AddCommand(functionListCmd)
}

type functionsByToolchain map[string]map[string]goclientnew.FunctionSignature
type functionsByEntity map[string]functionsByToolchain

const builtinFunctionKey = "builtin"

// functionHost names where functions run other than the builtin executor: the client of a
// channel. The zero value is the builtin executor.
type functionHost struct {
	channel string
}

func (h functionHost) isBuiltin() bool {
	return h.channel == ""
}

// currentFunctionHost is the host named by the --channel flag that function do, exec, run and
// the vet/get/set verbs share.
func currentFunctionHost() functionHost {
	return functionHost{channel: functionChannel}
}

func listFunctions(host functionHost, whereClause string) (string, functionsByToolchain, error) {
	entity := builtinFunctionKey
	funcs := functionsByToolchain{}
	params := &goclientnew.ListFunctionsParams{}
	if whereClause != "" {
		params.Where = &whereClause
	}

	// A channel is listed through a space-scoped route, so it needs a space.
	if selectedSpaceID == "*" && host.channel != "" {
		return entity, funcs, fmt.Errorf("--channel needs --space, the space the channel is in")
	}
	if host.channel != "" {
		channel, err := resolveChannel(host.channel, selectedSpaceID, idSelect("ChannelID"))
		if err != nil {
			return entity, funcs, fmt.Errorf("failed to get channel '%s': %w", host.channel, err)
		}
		entityType := "channel"
		params.Entity = &entityType
		channelIDStr := channel.Channel.ChannelID.String()
		params.Id = &channelIDStr
		entity = channelIDStr
	}

	// No space and the wildcard both mean the organization: a command that runs
	// with no space selected (login preloading the builtin functions, a
	// SpaceOptional command) has nothing to parse as a UUID.
	if selectedSpaceID == "" || selectedSpaceID == "*" {
		orgListParams := &goclientnew.ListOrgFunctionsParams{}
		if executorSpace != "" {
			orgListParams.ExecutorSpace = &executorSpace
		}
		if whereClause != "" {
			orgListParams.Where = &whereClause
		}
		orgFuncsRes, err := cubClientNew.ListOrgFunctionsWithResponse(ctx, orgListParams)
		if cubapi.IsAPIError(err, orgFuncsRes) {
			return entity, funcs, cubapi.InterpretErrorGeneric(err, orgFuncsRes)
		}
		// This shouldn't happen
		if orgFuncsRes.JSON200 == nil {
			return entity, funcs, fmt.Errorf("no functions returned")
		}
		funcs = *orgFuncsRes.JSON200
	} else {
		funcsRes, err := cubClientNew.ListFunctionsWithResponse(ctx, uuid.MustParse(selectedSpaceID), params)
		if cubapi.IsAPIError(err, funcsRes) {
			return entity, funcs, cubapi.InterpretErrorGeneric(err, funcsRes)
		}
		// This shouldn't happen
		if funcsRes.JSON200 == nil {
			return entity, funcs, fmt.Errorf("no functions returned")
		}
		funcs = *funcsRes.JSON200
	}
	return entity, funcs, nil
}

// functionSpecFilePath returns the path of the local function-list cache:
// functions.json in the same directory as the config file, so CUB_CONFIG
// relocates it along with config.yaml, tokens, and keys. It is resolved on
// each call rather than at process start so a CUB_CONFIG set after init
// (e.g. by tests) is honored.
func functionSpecFilePath() string {
	if contextManager != nil {
		return filepath.Join(contextManager.ConfigDir(), "functions.json")
	}
	// Before the context manager exists. CUB_CONFIG is the config directory.
	if v := os.Getenv("CUB_CONFIG"); v != "" {
		return filepath.Join(v, "functions.json")
	}
	return filepath.Join(os.Getenv("HOME"), ".confighub", "functions.json")
}

func loadFunctions() (functionsByEntity, error) {
	functions := make(functionsByEntity)

	functionSpec, err := os.ReadFile(functionSpecFilePath())
	if err != nil {
		return functions, err
	}
	err = json.Unmarshal(functionSpec, &functions)
	return functions, err
}

func saveFunctions(functions functionsByEntity) error {
	functionSpec, err := json.Marshal(functions)
	if err != nil {
		return err
	}
	path := functionSpecFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, functionSpec, 0644)
}

func removeFunctions() error {
	return os.Remove(functionSpecFilePath())
}

// listAndMaybeSaveFunctions fetches functions and caches them locally only when the
// result is the full builtin listing: no --where / --toolchain filtering and no
// --channel. Only the builtin entry is read back to register function commands,
// and per-host listings would accumulate one entry per channel ever listed;
// every cub invocation parses the cache at startup, so its size is paid on every
// command. Saving replaces the whole cache.
func listAndMaybeSaveFunctions(host functionHost, whereClause string) (string, functionsByToolchain, error) {
	entity, functions, err := listFunctions(host, whereClause)
	if err != nil {
		return entity, functions, err
	}
	if whereClause != "" || entity != builtinFunctionKey {
		return entity, functions, nil
	}
	err = saveFunctions(functionsByEntity{builtinFunctionKey: functions})
	return entity, functions, err
}

func functionListCmdRun(cmd *cobra.Command, args []string) error {
	if err := rejectRequestedColumns(cmd); err != nil {
		return err
	}
	effectiveWhere := addToolchainToWhereClause(where, functionListCmdArgs.toolchainType)
	_, funcs, err := listAndMaybeSaveFunctions(functionListCmdArgs.host, effectiveWhere)
	if err != nil {
		return err
	}

	// The return type doesn't match displayListResults, so we route through
	// effectiveOutput() manually.
	spec := effectiveOutput()
	alt := isAlternativeOutput()

	if !quiet && !alt {
		displayFunctionList(funcs)
	}
	if spec.Kind == OutputName {
		for _, functionMap := range funcs {
			for functionName := range functionMap {
				tprintRaw(functionName)
			}
		}
	}
	renderPayload(funcs)

	return nil
}

func displayFunctionList(funcs map[string]map[string]goclientnew.FunctionSignature) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{
			"ToolchainType",
			"FunctionName",
			"Mutating",
			"Validating",
			"Description",
		})
	}
	functions := [][]string{}
	for toolchainType, functionMap := range funcs {
		for functionName, f := range functionMap {
			row := []string{
				toolchainType,
				functionName,
				fmt.Sprintf("%v", f.Mutating),
				fmt.Sprintf("%v", f.Validating),
				f.Description,
			}
			functions = append(functions, row)
		}
	}
	// Sort by function name
	sort.Slice(functions, func(i, j int) bool {
		return functions[i][1] < functions[j][1]
	})
	for _, row := range functions {
		table.Append(row)
	}
	table.Render()
}
