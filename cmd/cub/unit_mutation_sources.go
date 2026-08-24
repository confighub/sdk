// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// MutationSources is not a field of a Unit -- it is the largest column on the table, and is
// read from its own subresource. `cub unit get -o mutations` renders it as a table, which is
// what a person wants; this is the same thing as JSON, which is what a script wants. Without
// it the only way to get the structure out of the CLI is to parse a table, and the readers
// that used to reach it through `cub unit get -o jq=.Unit.MutationSources` have nowhere to go.
// See docs/design/large-field-access.md section 6.
var unitMutationSourcesCmd = &cobra.Command{
	Use:   "mutation-sources <unit>",
	Short: "Show what set each value in a unit's config data",
	Long: getCommandHelp(`Display a unit's MutationSources: for each resource and each path
within it, what set the value that is there.

MutationSources is not part of the Unit entity, so no form of 'cub unit get' returns it.
Use -o json / -o yaml / -o jq= to shape the output; the payload is the list of resources,
so a path is addressed as '.[0].PathMutationMap'.

For a single Unit's history, 'cub unit get -o mutations' shows the same information as a
table. Across many Units, read the bulk endpoint GET /unit-mutation-sources.`, ""),
	Args: cobra.ExactArgs(1),
	RunE: runUnitMutationSources,
}

func init() {
	addStandardGetFlags(unitMutationSourcesCmd)
	unitCmd.AddCommand(unitMutationSourcesCmd)
}

func runUnitMutationSources(cmd *cobra.Command, args []string) error {
	unit, err := apiGetUnitFromSlugInSpace(args[0], selectedSpaceID, "UnitID,SpaceID,Slug")
	if err != nil {
		return err
	}
	mutationSources, err := fetchUnitMutationSources(unit.SpaceID, unit.UnitID)
	if err != nil {
		return err
	}
	// An empty list is the honest answer for a Unit nothing has written to yet, so it is
	// rendered rather than reported as a failure the way a missing configuration is.
	payload := goclientnew.ResourceMutationList{}
	if mutationSources != nil {
		payload = *mutationSources
	}
	if renderPayload(payload) {
		return nil
	}
	displayJSON(payload)
	return nil
}
