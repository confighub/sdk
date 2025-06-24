// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var workerListFunctionCmd = &cobra.Command{
	Use:   "list-function <worker-slug>",
	Args:  cobra.ExactArgs(1),
	Short: "List functions supported by a worker",
	Long:  `List functions supported by a worker.`,
	RunE:  workerListFunctionCmdRun,
}

func init() {
	addStandardListFlags(workerListFunctionCmd)
	workerCmd.AddCommand(workerListFunctionCmd)
}

func workerListFunctionCmdRun(_ *cobra.Command, args []string) error {
	entity, err := apiGetBridgeWorkerFromSlug(args[0])
	if err != nil {
		return err
	}

	funcsRes, err := cubClientNew.ListBridgeWorkerFunctionsWithResponse(ctx, uuid.MustParse(selectedSpaceID), entity.BridgeWorkerID)
	if IsAPIError(err, funcsRes) {
		return InterpretErrorGeneric(err, funcsRes)
	}

	table := tableView()
	table.SetHeader([]string{
		"ToolchainType",
		"FunctionName",
		"Req'dParameters",
		"VarArgs",
		"Mutating",
		"Validating",
		"Hermetic",
		"Idempotent",
		"Description",
		"Parameters",
	})
	functions := [][]string{}
	for toolchainType, functionMap := range *funcsRes.JSON200 {
		for functionName, f := range functionMap {
			row := []string{
				toolchainType,
				functionName,
				fmt.Sprintf("%v", f.RequiredParameters),
				fmt.Sprintf("%v", f.VarArgs),
				fmt.Sprintf("%v", f.Mutating),
				fmt.Sprintf("%v", f.Validating),
				fmt.Sprintf("%v", f.Hermetic),
				fmt.Sprintf("%v", f.Idempotent),
				f.Description,
			}
			rstring := map[bool]string{true: "req", false: "opt"}
			parameters := ""
			for _, param := range f.Parameters {
				parameters += fmt.Sprintf(`%s:%q(%s), `, param.ParameterName, param.Description, rstring[param.Required])
				row = append(row, parameters)
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

	return nil
}
