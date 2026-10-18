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

// standardTargetLabels are the well-known Target labels, in the order they are
// displayed as columns by the target list command. They are the Space labels that
// describe where configuration runs and who owns it, so a Target can be selected by
// the same values as the Spaces that release to it.
var standardTargetLabels = []string{"Owner", "Environment", "Region"}

// standardLabelExamples gives each well-known label an example value, for the
// help text of the flag that sets it.
var standardLabelExamples = map[string]string{
	"Owner":       "Engineering",
	"Variant":     "prod",
	"Stage":       "Canary",
	"Environment": "Prod",
	"Region":      "us-east1",
	"Layer":       "App",
}

// standardLabelFlagValues holds the values given for the well-known label flags
// (--owner, --environment, …) of one kind of entity, registered by
// addStandardLabelFlags or addStandardLabelFilterFlags.
type standardLabelFlagValues struct {
	// names are the well-known labels, in display order.
	names []string
	// values are the flag values, keyed by label name.
	values map[string]*string
}

func newStandardLabelFlagValues(cmd *cobra.Command, names []string, usage func(name string) string) standardLabelFlagValues {
	values := standardLabelFlagValues{names: names, values: make(map[string]*string, len(names))}
	for _, name := range names {
		value := new(string)
		values.values[name] = value
		cmd.Flags().StringVar(value, strings.ToLower(name), "", usage(name))
	}
	return values
}

// addStandardLabelFlags registers one flag per well-known label of entityType,
// named for the label in lower case, as shorthand for the corresponding --label.
// The returned values are read back with apply().
func addStandardLabelFlags(cmd *cobra.Command, entityType string, names []string) standardLabelFlagValues {
	return newStandardLabelFlagValues(cmd, names, func(name string) string {
		return fmt.Sprintf("value for the well-known %q %s label (e.g. %s); shorthand for --label %s=<value>",
			name, entityType, standardLabelExamples[name], name)
	})
}

// addStandardLabelFilterFlags registers one flag per well-known label of
// entityType, named for the label in lower case, that lists only the entities
// with that label value. The returned values are read back with
// addToWhereClause().
func addStandardLabelFilterFlags(cmd *cobra.Command, entityType string, names []string) standardLabelFlagValues {
	return newStandardLabelFlagValues(cmd, names, func(name string) string {
		return fmt.Sprintf("only list %ss whose %q label has this value (e.g. %s); ANDed with --where",
			entityType, name, standardLabelExamples[name])
	})
}

// addToWhereClause AND's a Labels.<name> equality constraint into the where
// clause for each well-known label flag that was set.
func (values standardLabelFlagValues) addToWhereClause(whereClause string) string {
	for _, name := range values.names {
		whereClause = addEqualityToWhereClause(whereClause, "Labels."+name, *values.values[name])
	}
	return whereClause
}

// qualifyVariant lets --variant name its Component inline, as <component>/<variant>, the
// way the scope flag does: the component part goes to --component's value, which has to
// agree if it was given, and the label keeps the variant alone. A bare value is left as it
// is, since here the component is optional. Only Spaces have the Variant label.
func (values standardLabelFlagValues) qualifyVariant(component *string) error {
	value := *values.values["Variant"]
	if !strings.Contains(value, "/") {
		return nil
	}
	qualifier, variant, err := qualifiedVariant(*component, value)
	if err != nil {
		return err
	}
	*component = qualifier
	*values.values["Variant"] = variant
	return nil
}

// apply prepends the labels set by the well-known label flags to the --label
// slice, so that both the whole-body path (setLabels) and the patch path
// (BuildPatchData) pick them up. They go first so that an explicit --label for
// the same key wins.
func (values standardLabelFlagValues) apply() {
	pairs := make([]string, 0, len(values.names))
	for _, name := range values.names {
		if value := *values.values[name]; value != "" {
			pairs = append(pairs, name+"="+value)
		}
	}
	if len(pairs) > 0 {
		label = append(pairs, label...)
	}
}
