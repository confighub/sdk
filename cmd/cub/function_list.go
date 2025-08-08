// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
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
- --toolchain: Show functions for specific toolchain (Kubernetes/YAML, OpenTofu/HCL, etc.)
- --target: Show functions available for a specific deployment target
- --worker: Show functions available on a specific worker
- --unit: Show functions available for a specific unit

Function types:
- Mutating: false = Read-only inspection functions (safe to run repeatedly)
- Mutating: true = Modifies configuration data (changes unit state)
- Validating: true = Returns pass/fail validation results

Use --names to get just function names for scripting.`

	return getCommandHelp(baseHelp, agentContext)
}

var functionListCmdArgs struct {
	targetSlug    string
	workerSlug    string
	unitSlug      string
	toolchainType string
}

func init() {
	functionListCmd.Flags().StringVar(&functionListCmdArgs.targetSlug, "target", "", "Target slug to list functions for")
	functionListCmd.Flags().StringVar(&functionListCmdArgs.workerSlug, "worker", "", "Worker slug to list functions for")
	functionListCmd.Flags().StringVar(&functionListCmdArgs.unitSlug, "unit", "", "Unit slug to list functions for")
	functionListCmd.Flags().StringVar(&functionListCmdArgs.toolchainType, "toolchain", "", "Toolchain type to list functions for")
	// Function list doesn't support where
	enableNamesFlag(functionListCmd)
	enableQuietFlag(functionListCmd)
	enableJsonFlag(functionListCmd)
	enableJqFlag(functionListCmd)
	enableNoheaderFlag(functionListCmd)
	functionCmd.AddCommand(functionListCmd)
}

type functionsByToolchain map[string]map[string]goclientnew.FunctionSignature
type functionsByEntity map[string]functionsByToolchain

const builtinFunctionKey = "builtin"

func listFunctions(targetSlug, workerSlug, unitSlug string) (string, functionsByToolchain, error) {
	entity := builtinFunctionKey
	funcs := functionsByToolchain{}
	params := &goclientnew.ListFunctionsParams{}
	
	// Validate that selectedSpaceID is not "*" when target, worker, or unit is specified
	if selectedSpaceID == "*" && (targetSlug != "" || workerSlug != "" || unitSlug != "") {
		return entity, funcs, fmt.Errorf("cannot use --space '*' with --target, --worker, or --unit flags")
	}
	if targetSlug != "" {
		targetDetails, err := apiGetTargetFromSlug(targetSlug, selectedSpaceID)
		if err != nil {
			return entity, funcs, fmt.Errorf("failed to get target '%s': %w", targetSlug, err)
		}
		entityType := "target"
		params.Entity = &entityType
		targetIDStr := targetDetails.Target.TargetID.String()
		params.Id = &targetIDStr
		entity = targetIDStr
	} else if workerSlug != "" {
		workerDetails, err := apiGetBridgeWorkerFromSlug(workerSlug)
		if err != nil {
			return entity, funcs, fmt.Errorf("failed to get worker '%s': %w", workerSlug, err)
		}
		entityType := "worker"
		params.Entity = &entityType
		workerIDStr := workerDetails.BridgeWorkerID.String()
		params.Id = &workerIDStr
		entity = workerIDStr
	} else if unitSlug != "" {
		unitDetails, err := apiGetUnitFromSlug(unitSlug)
		if err != nil {
			return entity, funcs, fmt.Errorf("failed to get unit '%s': %w", unitSlug, err)
		}
		entityType := "unit"
		params.Entity = &entityType
		unitIDStr := unitDetails.UnitID.String()
		params.Id = &unitIDStr
		entity = unitIDStr
	}

	if selectedSpaceID == "*" {
		orgFuncsRes, err := cubClientNew.ListOrgFunctionsWithResponse(ctx)
		if IsAPIError(err, orgFuncsRes) {
			return entity, funcs, InterpretErrorGeneric(err, orgFuncsRes)
		}
		// This shouldn't happen
		if orgFuncsRes.JSON200 == nil {
			return entity, funcs, fmt.Errorf("no functions returned")
		}
		funcs = *orgFuncsRes.JSON200
	} else {
		funcsRes, err := cubClientNew.ListFunctionsWithResponse(ctx, uuid.MustParse(selectedSpaceID), params)
		if IsAPIError(err, funcsRes) {
			return entity, funcs, InterpretErrorGeneric(err, funcsRes)
		}
		// This shouldn't happen
		if funcsRes.JSON200 == nil {
			return entity, funcs, fmt.Errorf("no functions returned")
		}
		funcs = *funcsRes.JSON200
	}
	return entity, funcs, nil
}

var functionSpecFile = filepath.Join(os.Getenv("HOME"), ".confighub", "functions.json")

func loadFunctions() (functionsByEntity, error) {
	functions := make(functionsByEntity)

	functionSpec, err := os.ReadFile(functionSpecFile)
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
	err = os.WriteFile(functionSpecFile, functionSpec, 0644)
	if err != nil {
		return err
	}
	// tprint("Function list saved to %s", functionSpecFile)
	return nil
}

func removeFunctions() error {
	return os.Remove(functionSpecFile)
}

func saveFunctionsForEntity(entity string, functionMap functionsByToolchain) error {
	// Ignore errors in the case that functions weren't saved or weren't compatible.
	functions, _ := loadFunctions()

	// Update the functions for the specified entity
	functions[entity] = functionMap

	// Save the functions
	err := saveFunctions(functions)
	if err != nil {
		return err
	}
	return nil
}

func listAndSaveFunctions(targetSlug, workerSlug, unitSlug string) (string, functionsByToolchain, error) {
	entity, functions, err := listFunctions(targetSlug, workerSlug, unitSlug)
	if err != nil {
		return entity, functions, err
	}
	err = saveFunctionsForEntity(entity, functions)
	return entity, functions, err
}

func functionListCmdRun(cmd *cobra.Command, args []string) error {
	_, funcs, err := listAndSaveFunctions(functionListCmdArgs.targetSlug, functionListCmdArgs.workerSlug, functionListCmdArgs.unitSlug)
	if err != nil {
		return err
	}

	// The return type doesn't match displayListResults, so we repeat that code here.
	// Check if any alternative output format is specified
	hasAlternativeOutput := names || jsonOutput || jq != ""

	if !quiet && !hasAlternativeOutput {
		displayFunctionList(funcs)
	}
	if names {
		table := tableView()
		for toolchainType, functionMap := range funcs {
			if functionListCmdArgs.toolchainType != "" && functionListCmdArgs.toolchainType != toolchainType {
				continue
			}
			for functionName := range functionMap {
				table.Append([]string{functionName})
			}
		}
		table.Render()
	}
	if jsonOutput {
		displayJSON(funcs)
	}
	if jq != "" {
		displayJQ(funcs)
	}

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
		if functionListCmdArgs.toolchainType != "" && functionListCmdArgs.toolchainType != toolchainType {
			continue
		}
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
