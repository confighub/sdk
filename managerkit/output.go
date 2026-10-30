// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package managerkit

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cliutil"
)

// Output formats. JSON is the default so that the tools compose; a table is for
// a human reading one answer.
//
// A command does not compare against these: [Render] decides, and a command only
// renders its table when Render declines. They are here because the flag's
// default is one of them.
const (
	OutputJSON  = "json"
	OutputTable = "table"
)

// AddOutputFlag registers the -o/--output flag every command in a manager tool
// has.
//
// The default is JSON rather than a table, which is the opposite of what a
// person at a terminal usually wants. Manager tools are meant to be read by an
// agent as often as by a person, and an agent that has to parse a table is an
// agent that will eventually parse it wrong.
func AddOutputFlag(cmd *cobra.Command, dest *string) {
	cmd.Flags().StringVarP(dest, "output", "o", OutputJSON,
		"output format: json | yaml | table | jq=<expr> | yq=<expr>")
}

// Render writes v in the format output names, reporting whether it did.
//
// It handles every format that is a projection of the value itself: json, yaml,
// and the jq and yq expressions, which walk what the JSON is built from. It
// returns false for a table, which only the command can render -- a table's
// columns are a decision about what matters, not something derivable from the
// value.
//
// So the calling convention is: ask Render first, and render the table only if
// it declined.
//
//	if done, err := managerkit.Render(out, output, report); done || err != nil {
//		return err
//	}
//	printTable(cmd, report)
//	return nil
//
// name, wide and custom-columns are refused. cliutil parses them and can render
// them over an entity with named columns, but these commands report their own
// models rather than entities, and no command here has named columns for one.
func Render(w io.Writer, output string, v any) (bool, error) {
	if output == "" {
		output = OutputJSON
	}
	spec, err := cliutil.ParseOutput(output)
	if err != nil {
		// cliutil's own message lists everything it can parse, three of which
		// this command would then refuse. One list, and it is the true one.
		return false, unsupported(output)
	}
	switch spec.Kind {
	case cliutil.OutputName, cliutil.OutputWide, cliutil.OutputCustomColumns:
		return false, unsupported(output)
	default:
		return spec.Render(w, v)
	}
}

// ProgressWriter is where a command writes the lines it prints while it works:
// stdout under a table, which is text throughout, and stderr under every format
// Render writes, whose stdout has to hold the value alone for it to parse.
func ProgressWriter(cmd *cobra.Command, output string) io.Writer {
	if spec, err := cliutil.ParseOutput(output); err == nil && spec.Kind == cliutil.OutputTable {
		return cmd.OutOrStdout()
	}
	return cmd.ErrOrStderr()
}

func unsupported(output string) error {
	return fmt.Errorf("--output=%s is not supported; use json, yaml, table, jq=<expr> or yq=<expr>", output)
}
