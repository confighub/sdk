// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var functionReduceCmd = &cobra.Command{
	Use:         "reduce <map-function> [<arg1> ...] --reducer <function> [--reducer-arg <arg> ...] [--group-by <expression>]",
	Short:       "Summarize what a function returns across units",
	Long:        getCommandHelp(functionReduceLong, functionReduceAgentContext),
	Args:        cobra.MinimumNArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        functionReduceCommandRun,
}

const functionReduceLong = `Run a function on units in a space, or across all spaces without --space, and
summarize what it returns.

The function named first, the map, is run on each unit selected. It must read
configuration and return values (an AttributeValueList), as get-starlark,
get-replicas and the other get- functions do; a missing get- prefix is
supplied. Each value it returns is a row, which carries where it came from:
the unit, its space, and the target the unit is released to.

--group-by is a CEL expression evaluated on each row, as row, returning the key
of the row's group, or a list of keys for a row that belongs to several groups.
Without it, every row is in one group. A row has the fields of the value
(ResourceName, ResourceType, Path, Value, AttributeName, DataType, FunctionName)
and Origin (UnitID, UnitSlug, UnitLabels, SpaceID, SpaceSlug, SpaceLabels,
TargetID, TargetSlug, TargetSpaceSlug, RevisionNum). TargetID is null and the
target's slugs empty for a unit with no target. A target's slug is unique only
in its space, so name a cluster with both, as in
row.Origin.TargetSpaceSlug + "/" + row.Origin.TargetSlug. Read a label a unit or space may not have with ?, as in
row.Origin.SpaceLabels[?"Environment"].orValue("none"): a unit with a row the
expression fails on is reported, and none of its rows is grouped.

The reducer, --reducer, is run once on each group's rows. reduce-starlark takes
a Starlark program defining reduce(rows), which returns a list of values, each a
dict with at least Path and Value; 'group' is the group's key. Pass the
reducer's arguments with --reducer-arg, once each, in order.

Nothing is written. The default output lists each value the reducer returned
for each group. -o json and the other output formats show the whole response.

Examples:
` + "```" + `
  # Count Deployments per space across the organization
  cub function reduce \
    --resource-type apps/v1/Deployment \
    --group-by 'row.Origin.SpaceSlug' \
    --reducer reduce-starlark \
    --reducer-arg 'def reduce(rows): return [{"Path": "deployments", "Value": len(rows)}]' \
    get-replicas

  # Total replicas per Environment label of the spaces, where total-replicas.star is
  #   def reduce(rows):
  #     total = 0
  #     for r in rows:
  #       total += r["Value"]
  #     return [{"Path": "replicas", "Value": total}]
  cub function reduce \
    --group-by 'row.Origin.SpaceLabels[?"Environment"].orValue("none")' \
    --reducer reduce-starlark \
    --reducer-arg "$(cat total-replicas.star)" \
    get-replicas

  # Distinct container images per target, read with a Starlark map
  cub function reduce --space my-space \
    --group-by 'row.Origin.TargetSpaceSlug + "/" + row.Origin.TargetSlug' \
    --reducer reduce-starlark \
    --reducer-arg "$(cat images-per-target.star)" \
    get-starlark "$(cat container-images.star)"
` + "```" + `
`

const functionReduceAgentContext = `Agent-specific guidance:

- Use reduce to answer a question about many units at once (counts, totals,
  distinct values, rollups per space, label or target) instead of fetching each
  unit's output and summarizing it yourself.
- The map has to be a non-mutating function returning an AttributeValueList;
  'cub function explain FUNCTION' shows a function's OutputType.
- The reducer is a function whose InputType is AttributeValueList, such as
  reduce-starlark; 'cub function explain reduce-starlark' shows its parameters.
- Use -o json to read the whole response, including the units whose map failed.`

var (
	reduceGroupBy     string
	reduceReducer     string
	reduceReducerArgs []string
)

func init() {
	functionReduceCmd.Flags().StringVar(&reduceGroupBy, "group-by", "", "CEL expression evaluated on each row, as row, returning the key of its group or a list of keys; without it, every row is in one group")
	functionReduceCmd.Flags().StringVar(&reduceReducer, "reducer", "", "the function run once on each group's rows, such as reduce-starlark")
	functionReduceCmd.Flags().StringArrayVar(&reduceReducerArgs, "reducer-arg", []string{}, "an argument of the reducer, in order; repeat for each, or use --name=value to name its parameter")
	_ = functionReduceCmd.MarkFlagRequired("reducer")
	functionReduceCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "select specific units by slug or UUID (can be repeated or comma-separated)")
	enableWhereFlag(functionReduceCmd)
	enableFilterFlag(functionReduceCmd)
	functionReduceCmd.Flags().StringVar(&resourceType, "resource-type", "", invokeResourceTypeFlagUsage)
	functionReduceCmd.Flags().StringVar(&whereData, "where-data", "", "where data filter")
	functionReduceCmd.Flags().StringVar(&whereResource, "where-resource", "", "filter which resources the map reads; the map runs only on the units holding such a resource, which makes a reduce over a few kinds of resource across many units much faster")
	functionReduceCmd.Flags().StringVar(&functionToolchainType, "toolchain", "Kubernetes/YAML", "ToolchainType of the units to run the map on")
	addStandardDisplayFlags(functionReduceCmd)
	functionCmd.AddCommand(functionReduceCmd)
}

func functionReduceCommandRun(_ *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	if len(unitIdentifiers) > 0 && (where != "" || filterID != "") {
		return errors.New("--unit, --where and --filter are mutually exclusive")
	}
	if where != "" && filterID != "" {
		return errors.New("--unit, --where and --filter are mutually exclusive")
	}
	effectiveWhere := where
	if len(unitIdentifiers) > 0 {
		effectiveWhere, err = buildWhereClauseFromUnits(unitIdentifiers)
		if err != nil {
			return err
		}
	}

	mapFunction := resolveFunctionNameForVerb(ModeNonMutating, args[0])
	response, err := reduceFunctions(canonicalFunctionInvocation(mapFunction, args[1:]),
		canonicalFunctionInvocation(reduceReducer, reduceReducerArgs), effectiveWhere, filterID)
	if err != nil {
		return err
	}
	if !renderPayload(response) && !quiet {
		displayReduceResponse(response)
	}
	return reduceFailuresError(response)
}

// reduceFunctions runs the reduce through cubapi, across the organization or in the selected
// space. In a space it selects that space's units and runs the reducer on that space's executor,
// which is what the space's own reduce endpoint does.
func reduceFunctions(mapInvocation, reducer api.FunctionInvocation, where, filterID string) (*goclientnew.FunctionReduceResponse, error) {
	sel := cubapi.Selector{
		Where:         where,
		Filter:        filterID,
		WhereData:     whereData,
		ResourceType:  resourceType,
		WhereResource: whereResource,
		ToolchainType: functionToolchainType,
		ExecutorSpace: executorSpace,
		IncludeHidden: includeHidden,
	}
	scope := "the organization"
	if selectedSpaceID != "" && selectedSpaceID != "*" {
		sel.Where = cubapi.NewWhere(where).Eq("SpaceID", selectedSpaceID).String()
		sel.ExecutorSpace = selectedSpaceID
		scope = "space " + selectedSpaceID
	}
	response, err := cubapi.Reduce(ctx, cubClient, sel, []api.FunctionInvocation{mapInvocation}, reduceGroupBy, reducer)
	if err != nil {
		return nil, errors.Wrap(err, "failed to reduce across "+scope)
	}
	return response, nil
}

// canonicalFunctionInvocation is a function and its command-line arguments as the SDK takes them,
// parsed as cub function do parses them: in order, or as --name=value, each with an optional cel:
// or template: evaluator.
func canonicalFunctionInvocation(functionName string, args []string) api.FunctionInvocation {
	invocation := api.FunctionInvocation{FunctionName: functionName}
	for _, arg := range parseFunctionArguments(args) {
		canonical := api.FunctionArgument{}
		if arg.ParameterName != nil {
			canonical.ParameterName = *arg.ParameterName
		}
		if arg.Evaluator != nil {
			canonical.Evaluator = *arg.Evaluator
		}
		if arg.Value != nil {
			if value, err := arg.Value.AsFunctionArgumentValue0(); err == nil {
				canonical.Value = value
			}
		}
		invocation.Arguments = append(invocation.Arguments, canonical)
	}
	return invocation
}

// displayReduceResponse lists each value the reducer returned for each group, and then why the
// map failed on any Unit and the reducer on any group.
func displayReduceResponse(response *goclientnew.FunctionReduceResponse) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Group", "Rows", "Units", "Path", "Value"})
	}
	for _, group := range response.Groups {
		rows := fmt.Sprintf("%d", group.NumRows)
		units := fmt.Sprintf("%d", group.NumUnits)
		if !group.Success {
			table.Append([]string{group.Key, rows, units, "", "reducer failed"})
			continue
		}
		values, ok := reduceOutputValues(group)
		if !ok {
			table.Append([]string{group.Key, rows, units, "", jsonOneLine(group.Output)})
			continue
		}
		if len(values) == 0 {
			table.Append([]string{group.Key, rows, units, "", ""})
		}
		for _, value := range values {
			table.Append([]string{group.Key, rows, units, value.Path, jsonOneLine(value.Value)})
		}
	}
	table.Render()
	tprint("%d rows from %d units in %d groups", response.NumRows, response.NumUnits, len(response.Groups))

	for _, group := range response.Groups {
		if !group.Success {
			tprintErr("reducer failed on group %q: %s", group.Key, strings.Join(group.ErrorMessages, "; "))
		}
	}
	for _, unitError := range response.UnitErrors {
		tprintErr("unit %s/%s was left out: %s", unitError.SpaceSlug, unitError.UnitSlug,
			strings.Join(unitError.ErrorMessages, "; "))
	}
}

// reduceValue is the part of each value a reducer returns that the default output shows.
type reduceValue struct {
	Path  string
	Value any
}

// reduceOutputValues reads a group's output as the values of an AttributeValueList, which is
// what reduce-starlark returns. It reports false for output of another type.
func reduceOutputValues(group goclientnew.FunctionReduceGroup) ([]reduceValue, bool) {
	if group.OutputType != "AttributeValueList" || group.Output == nil {
		return nil, false
	}
	encoded, err := json.Marshal(group.Output)
	if err != nil {
		return nil, false
	}
	var values []reduceValue
	if err := json.Unmarshal(encoded, &values); err != nil {
		return nil, false
	}
	return values, true
}

// reduceFailuresError reports a reduce that left a Unit out, because its map or GroupBy failed on
// it, or whose reducer failed on a group, after the output has shown what did succeed.
func reduceFailuresError(response *goclientnew.FunctionReduceResponse) error {
	if response.Success {
		return nil
	}
	failedGroups := 0
	for _, group := range response.Groups {
		if !group.Success {
			failedGroups++
		}
	}
	return errors.Errorf("%d of %d units were left out and the reducer failed on %d of %d groups",
		len(response.UnitErrors), response.NumUnits, failedGroups, len(response.Groups))
}
