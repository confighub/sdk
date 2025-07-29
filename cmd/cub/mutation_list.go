// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var mutationListCmd = &cobra.Command{
	Use:   "list <unit>",
	Short: "List mutations",
	Long: `List mutations for a unit in a space. Mutations track the history of detailed mutations made to a unit's configuration.

Examples:
  # List all mutations for a unit
  cub mutation list --space my-space my-ns

  # List mutations without headers
  cub mutation list --space my-space --no-header my-ns

  # List mutations in JSON format
  cub mutation list --space my-space --json my-ns

  # List mutations using unit ID instead of slug
  cub mutation list --space my-space --by-unit-id 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c

  # List mutations with custom JQ filter
  cub mutation list --space my-space --jq '.[].MutationNum' my-ns

  # List mutations with specific criteria
  cub mutation list --space my-space --where 'MutationNum > 1' my-ns

  # List mutations with extended information
  cub mutation list --space my-space --json --extended my-ns`,
	Args: cobra.ExactArgs(1),
	RunE: mutationListCmdRun,
}

func init() {
	addStandardListFlags(mutationListCmd)
	mutationListCmd.Flags().BoolVar(&byUnitID, "by-unit-id", false, "use unit id instead of slug")
	mutationCmd.AddCommand(mutationListCmd)
}

func mutationListCmdRun(cmd *cobra.Command, args []string) error {
	var unit *goclientnew.Unit
	var err error
	if byUnitID {
		unit, err = apiGetUnit(args[0])
	} else {
		unit, err = apiGetUnitFromSlug(args[0])
	}
	if err != nil {
		return err
	}
	mutations, err := apiListMutations(selectedSpaceID, unit.UnitID.String(), where)
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
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Num", "RevisionNum", "Link", "ProvidedResource", "ProvidedPath", "Trigger", "FunctionName"})
	}
	for _, extendedMutation := range extendedMutations {
		mutationDetails := extendedMutation.Mutation
		var linkSlug, triggerSlug string
		if extendedMutation.Link != nil {
			linkSlug = extendedMutation.Link.Slug
		}
		if extendedMutation.Trigger != nil {
			triggerSlug = extendedMutation.Trigger.Slug
		}
		table.Append([]string{
			fmt.Sprintf("%d", mutationDetails.MutationNum),
			fmt.Sprintf("%d", mutationDetails.RevisionNum),
			linkSlug,
			mutationDetails.ProvidedResource.ResourceName,
			mutationDetails.ProvidedPath,
			triggerSlug,
			mutationDetails.FunctionInvocation.FunctionName,
		})
	}
	table.Render()
}

func apiListMutations(spaceID string, unitID string, whereFilter string) ([]*goclientnew.ExtendedMutation, error) {
	newParams := &goclientnew.ListExtendedMutationsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	include := "RevisionID,LinkID,TargetID"
	newParams.Include = &include
	muteRes, err := cubClientNew.ListExtendedMutationsWithResponse(ctx, uuid.MustParse(spaceID), uuid.MustParse(unitID), newParams)
	if IsAPIError(err, muteRes) {
		return nil, InterpretErrorGeneric(err, muteRes)
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
