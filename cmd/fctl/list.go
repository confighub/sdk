// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/confighub/sdk/function/client"
	"github.com/spf13/cobra"
)

func newListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available functions",
		Args:  cobra.ExactArgs(0),
		Run: func(_ /*cmd*/ *cobra.Command, _ []string) {
			respMsg, err := client.GetFunctionList(transportConfig, toolchain)
			failOnError(err)

			// Timestamps disrupt golden outputs
			// log.Info(fmt.Sprintf("Received map of %d functions\n", len(respMsg)))
			table := tableView()
			table.SetHeader([]string{
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
			for _, f := range respMsg {
				row := []string{
					f.FunctionName,
					strconv.Itoa(f.RequiredParameters),
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
				}
				row = append(row, parameters)
				functions = append(functions, row)
			}
			// Sort by function name
			sort.Slice(functions, func(i, j int) bool {
				return functions[i][0] < functions[j][0]
			})
			for _, row := range functions {
				table.Append(row)
			}
			table.Render()
		},
	}

	return cmd
}
