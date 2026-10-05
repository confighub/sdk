// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// standardSpaceLabels are the well-known Space labels, in the order they are
// displayed as columns by the space list command.
var standardSpaceLabels = []string{"Owner", "Variant", "Stage", "Environment", "Region", "Layer"}

// standardSpaceLabelExamples gives each well-known label an example value, for
// the help text of the flag that sets it.
var standardSpaceLabelExamples = map[string]string{
	"Owner":       "Engineering",
	"Variant":     "prod",
	"Stage":       "Canary",
	"Environment": "Prod",
	"Region":      "us-east1",
	"Layer":       "App",
}

// spaceLabelFlagValues holds the values given for the well-known Space label
// flags (--owner, --stage, …) registered by addStandardSpaceLabelFlags,
// keyed by label name.
type spaceLabelFlagValues map[string]*string

// addStandardSpaceLabelFlags registers one flag per well-known Space label,
// named for the label in lower case, as shorthand for the corresponding
// --label. The returned values are read back with apply().
func addStandardSpaceLabelFlags(cmd *cobra.Command) spaceLabelFlagValues {
	values := make(spaceLabelFlagValues, len(standardSpaceLabels))
	for _, name := range standardSpaceLabels {
		value := new(string)
		values[name] = value
		cmd.Flags().StringVar(value, strings.ToLower(name), "",
			fmt.Sprintf("value for the well-known %q Space label (e.g. %s); shorthand for --label %s=<value>",
				name, standardSpaceLabelExamples[name], name))
	}
	return values
}

// addStandardSpaceLabelFilterFlags registers one flag per well-known Space
// label, named for the label in lower case, that lists only the Spaces with that
// label value. The returned values are read back with addToWhereClause().
func addStandardSpaceLabelFilterFlags(cmd *cobra.Command) spaceLabelFlagValues {
	values := make(spaceLabelFlagValues, len(standardSpaceLabels))
	for _, name := range standardSpaceLabels {
		value := new(string)
		values[name] = value
		cmd.Flags().StringVar(value, strings.ToLower(name), "",
			fmt.Sprintf("only list Spaces whose %q label has this value (e.g. %s); ANDed with --where",
				name, standardSpaceLabelExamples[name]))
	}
	return values
}

// addToWhereClause AND's a Labels.<name> equality constraint into the where
// clause for each well-known label flag that was set.
func (values spaceLabelFlagValues) addToWhereClause(whereClause string) string {
	for _, name := range standardSpaceLabels {
		whereClause = addEqualityToWhereClause(whereClause, "Labels."+name, *values[name])
	}
	return whereClause
}

// apply prepends the labels set by the well-known label flags to the --label
// slice, so that both the whole-body path (setLabels) and the patch path
// (BuildPatchData) pick them up. They go first so that an explicit --label for
// the same key wins.
func (values spaceLabelFlagValues) apply() {
	pairs := make([]string, 0, len(values))
	for _, name := range standardSpaceLabels {
		if value := *values[name]; value != "" {
			pairs = append(pairs, name+"="+value)
		}
	}
	if len(pairs) > 0 {
		label = append(pairs, label...)
	}
}
