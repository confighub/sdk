// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var byUnitID bool

var mutationListCmd = &cobra.Command{
	Use:   "list <unit>",
	Short: "List mutations",
	Long: getCommandHelp(`List mutations for a unit in a space. Mutations track the history of detailed mutations made to a unit's configuration.

The default output identifies each mutation and shows what produced it: the Link, Trigger or
Invocation behind it and the function it ran. -o wide adds the timestamp, the Subgroup, and the
replay outcome -- what a merge did with the mutation, which is what says whether a function
re-ran downstream or the merge patched its result across instead.

Examples:
`+"```"+`
  # List all mutations for a unit
  cub mutation list --space my-space my-ns

  # List mutations without headers
  cub mutation list --space my-space --no-headers my-ns

  # List mutations in JSON format
  cub mutation list --space my-space -o json my-ns

  # List mutations using unit ID instead of slug
  cub mutation list --space my-space --by-unit-id 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c

  # List mutations with custom JQ filter
  cub mutation list --space my-space -o jq='.[].MutationNum' my-ns

  # List mutations with specific criteria
  cub mutation list --space my-space --where 'MutationNum > 1' my-ns
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: mutationListCmdRun,
}

// Default columns to display when no custom columns are specified. This is also what drives the
// select list, so it names every field either layout shows -- the wide-only ones included.
var defaultMutationColumns = []string{"Mutation.MutationNum", "Mutation.RevisionNum", "Link.Slug", "Mutation.ProvidedResource.ResourceName", "Mutation.ProvidedPath", "Trigger.Slug", "Invocation.Slug", "Mutation.FunctionInvocation.FunctionName", "Mutation.CreatedAt", "Mutation.Subgroup", "Mutation.ReplayOutcome"}

// Mutation-specific aliases
var mutationAliases = map[string]string{
	"Name": "Mutation.MutationNum",
	"ID":   "Mutation.MutationID",
}

// Mutation custom column dependencies
var mutationCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(mutationListCmd)
	mutationCmd.AddCommand(mutationListCmd)
}

func mutationListCmdRun(cmd *cobra.Command, args []string) error {
	var unit *goclientnew.Unit
	var err error

	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	unit, err = apiGetUnitFromSlugInSpace(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}
	mutations, err := apiListMutations(selectedSpaceID, unit.UnitID.String(), where, selectFields, filterID)
	if err != nil {
		return err
	}
	displayListResults(mutations, getMutationSlugFromExtended, displayMutationList)
	return nil
}

func getMutationSlugFromExtended(mutationDetails *goclientnew.ExtendedMutation) string {
	// Use number
	return fmt.Sprintf("%d", mutationDetails.Mutation.MutationNum)
}

func displayMutationList(extendedMutations []*goclientnew.ExtendedMutation) {
	wide := effectiveOutput().Kind == OutputWide
	table := tableView()
	if !noheader {
		header := []string{"Num", "RevisionNum"}
		if wide {
			header = append(header, "Time")
		}
		header = append(header, "MergeSource", "Link", "ProvidedResource", "ProvidedPath", "Trigger", "Invocation", "FunctionName")
		if wide {
			header = append(header, "Subgroup", "Replay")
		}
		table.SetHeader(header)
	}
	for _, extendedMutation := range extendedMutations {
		mutationDetails := extendedMutation.Mutation
		var mergeSourceSlug, linkSlug, triggerSlug, invocationSlug string
		if extendedMutation.MergeSource != nil {
			mergeSourceSlug = extendedMutation.MergeSource.Slug
		} else if isSetUUID(mutationDetails.MergeSourceID) {
			mergeSourceSlug = mutationDetails.MergeSourceID.String()
		}
		if extendedMutation.Link != nil {
			linkSlug = extendedMutation.Link.Slug
		} else if isSetUUID(mutationDetails.LinkID) {
			linkSlug = mutationDetails.LinkID.String()
		}
		if extendedMutation.Trigger != nil {
			triggerSlug = extendedMutation.Trigger.Slug
		} else if isSetUUID(mutationDetails.TriggerID) {
			triggerSlug = mutationDetails.TriggerID.String()
		}
		if extendedMutation.Invocation != nil {
			invocationSlug = extendedMutation.Invocation.Slug
		} else if isSetUUID(mutationDetails.InvocationID) {
			invocationSlug = mutationDetails.InvocationID.String()
		}
		providedResource := ""
		if mutationDetails.ProvidedResource != nil {
			providedResource = mutationDetails.ProvidedResource.ResourceName
		}

		row := []string{
			fmt.Sprintf("%d", mutationDetails.MutationNum),
			fmt.Sprintf("%d", mutationDetails.RevisionNum),
		}
		if wide {
			// The same short form the other list commands use; time.Time.String() carries
			// microseconds and a zone name, which is most of a column for no gain.
			row = append(row, mutationDetails.CreatedAt.Format("2006-01-02 15:04:05"))
		}
		row = append(row,
			mergeSourceSlug,
			linkSlug,
			providedResource,
			// A resolved path names a resource and a location within it, so it is the column
			// that runs long enough to push the rest of the row off the screen.
			truncateWithEllipsis(mutationDetails.ProvidedPath, maxMutationProvidedPath),
			triggerSlug,
			invocationSlug,
			functionInvocationName(mutationDetails.FunctionInvocation),
		)
		if wide {
			row = append(row, mutationDetails.Subgroup, mutationDetails.ReplayOutcome)
		}
		table.Append(row)
	}
	table.Render()
}

// maxMutationProvidedPath is where the ProvidedPath column is cut. `cub mutation get` and -o json
// show the whole path.
const maxMutationProvidedPath = 40

// functionInvocationName is the function a Mutation ran, if it ran one.
func functionInvocationName(invocation *goclientnew.FunctionInvocation) string {
	if invocation == nil {
		return ""
	}
	return invocation.FunctionName
}

func apiListMutations(spaceID string, unitID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedMutation, error) {
	newParams := &goclientnew.ListExtendedMutationsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	include := "SpaceID,RevisionID,MergeSourceID,LinkID,TriggerID,InvocationID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"MutationNum", "MutationID", "UnitID", "SpaceID", "OrganizationID"}
		return buildSelectList("Mutation", nil, include, defaultMutationColumns, mutationAliases, mutationCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	muteRes, err := cubClientNew.ListExtendedMutationsWithResponse(ctx, uuid.MustParse(spaceID), uuid.MustParse(unitID), newParams)
	if cubapi.IsAPIError(err, muteRes) {
		return nil, cubapi.InterpretErrorGeneric(err, muteRes)
	}

	muteSlice := make([]*goclientnew.ExtendedMutation, len(*muteRes.JSON200))
	for i, mutation := range *muteRes.JSON200 {
		muteSlice[i] = &mutation
	}

	// Sort by MutationNum descending
	sort.Slice(muteSlice, func(i, j int) bool {
		return muteSlice[i].Mutation.MutationNum > muteSlice[j].Mutation.MutationNum
	})

	return muteSlice, nil
}
