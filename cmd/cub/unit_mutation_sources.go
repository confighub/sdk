// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/cockroachdb/errors"
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
table.

With --where, reads every unit the clause selects in one request rather than one per unit.
Each row carries the unit it belongs to, so the payload is addressed as
'.[0].MutationSources' rather than '.[0]'. Scoped to --space unless that is "*". Examples:

  cub unit mutation-sources --space my-space my-unit
  cub unit mutation-sources --space my-space --where "Slug LIKE 'app-%'"`, ""),
	Args: cobra.MaximumNArgs(1),
	RunE: runUnitMutationSources,
}

func init() {
	addStandardGetFlags(unitMutationSourcesCmd)
	// One request for the whole selection rather than one per Unit. The rows carry the Unit
	// each belongs to, which a bare list of ResourceMutationLists could not.
	enableWhereFlag(unitMutationSourcesCmd)
	unitCmd.AddCommand(unitMutationSourcesCmd)
}

func runUnitMutationSources(cmd *cobra.Command, args []string) error {
	if where != "" {
		if len(args) > 0 {
			return errors.New("name a unit or pass --where, not both")
		}
		rows, err := searchUnitMutationSources(where)
		if err != nil {
			return err
		}
		if renderPayload(rows) {
			return nil
		}
		displayJSON(rows)
		return nil
	}
	if len(args) == 0 {
		return errors.New("name a unit, or pass --where to read many")
	}
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
