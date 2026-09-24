// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitMoveCmd = &cobra.Command{
	Use:   "move [<unit-slug>] <to-space>",
	Short: "Move units to another space",
	Long: getCommandHelp(`Move units, and the links from them, to another space.

A unit keeps its identity: its revisions, mutations, events, links, clones and approvals
come with it. Moving requires Manage permission on each unit and CreateChildren permission
on the destination space.

Every check runs over the whole selection before anything moves, so a move that would
collide with a name in the destination, or that names a unit with an operation in flight,
moves nothing. Rename first if a name is taken; --dry-run reports what a move would do.

A unit on its space's release target takes the destination's, as it does when a space's
release target changes. The destination's triggers and attributes are applied afterwards,
and until that finishes the unit carries a temporary validation gate.

Examples:
`+"```"+`
  # Move one unit
  cub unit move --space my-space my-unit other-space

  # Move every unit of an app
  cub unit move --space my-space --where "Labels.App = 'web'" other-space

  # Preview a move across all spaces
  cub unit move --space "*" --where "Labels.App = 'web'" --dry-run other-space

  # Move specific units by slug
  cub unit move --space my-space --unit my-unit,another-unit other-space
`+"```"+`
`, ""),
	Args:        cobra.RangeArgs(1, 2),
	RunE:        unitMoveCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var unitMoveArgs struct {
	dryRun bool
}

func init() {
	enableWhereFlag(unitMoveCmd)
	enableFilterFlag(unitMoveCmd)
	enableQuietFlag(unitMoveCmd)
	enableOutputFlag(unitMoveCmd)
	unitMoveCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "move specific units by slug or UUID (can be repeated or comma-separated)")
	unitMoveCmd.Flags().BoolVar(&unitMoveArgs.dryRun, "dry-run", false, "report what the move would do without moving anything")
	unitCmd.AddCommand(unitMoveCmd)
}

// checkUnitMoveConflictingArgs returns the destination space reference and the unit reference, if
// one was given. The destination is always the last argument.
func checkUnitMoveConflictingArgs(args []string) (destination string, unitRef string) {
	destination = args[len(args)-1]
	isBulkMoveMode := len(args) == 1
	if isBulkMoveMode {
		if len(unitIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--unit and --where flags are mutually exclusive"))
		}
	} else {
		unitRef = args[0]
		if filter != "" || where != "" || len(unitIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --unit can only be specified without a unit argument"))
		}
	}
	if err := validateSpaceFlag(isBulkMoveMode); err != nil {
		failOnError(err)
	}
	return destination, unitRef
}

func unitMoveCmdRun(cmd *cobra.Command, args []string) error {
	destinationRef, unitRef := checkUnitMoveConflictingArgs(args)

	destination, err := resolveSpace(destinationRef, "SpaceID,Slug")
	if err != nil {
		return err
	}
	if destination == nil || destination.Space == nil {
		return fmt.Errorf("destination space %s not found", destinationRef)
	}
	destinationID, err := uuid.Parse(destination.Space.SpaceID.String())
	if err != nil {
		return err
	}

	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	var effectiveWhere string
	switch {
	case unitRef != "":
		effectiveWhere, err = buildWhereClauseFromUnits([]string{unitRef})
	case len(unitIdentifiers) > 0:
		effectiveWhere, err = buildWhereClauseFromUnits(unitIdentifiers)
	default:
		effectiveWhere = where
	}
	if err != nil {
		return err
	}
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	params := &goclientnew.BulkMoveUnitsParams{Where: &effectiveWhere}
	if filterID != "" {
		params.Filter = &filterID
	}
	if unitMoveArgs.dryRun {
		dryRun := true
		params.DryRun = &dryRun
	}

	resp, err := cubClientNew.BulkMoveUnitsWithResponse(ctx, params,
		goclientnew.BulkMoveUnitsJSONRequestBody{ToSpaceID: destinationID})
	// Every Unit refused for the same reason answers with that one error, rather than a result
	// per Unit; see PluggableHandleBulkDeleteRequest.
	if cubapi.IsAPIError(err, resp) {
		return cubapi.InterpretErrorGeneric(err, resp)
	}

	var responses *[]goclientnew.UnitMoveResponse
	if resp.JSON200 != nil {
		responses = resp.JSON200
	} else if resp.JSON207 != nil {
		responses = resp.JSON207
	} else {
		return errors.New("unexpected response from the unit move API")
	}

	return handleUnitMoveResponse(responses, destination.Space.Slug, unitMoveArgs.dryRun)
}

// handleUnitMoveResponse prints one line per Unit and fails if any of them did not move.
func handleUnitMoveResponse(results *[]goclientnew.UnitMoveResponse, destinationSlug string, isDryRun bool) error {
	if results == nil || len(*results) == 0 {
		if !quiet && !isAlternativeOutput() {
			tprint("No units found matching the filter")
		}
		renderPayload(results)
		return nil
	}
	if isAlternativeOutput() {
		renderPayload(results)
		return nil
	}

	failures := 0
	for _, result := range *results {
		if result.Error != nil {
			failures++
			tprint("%s: %s", result.Slug, result.Error.Message)
			continue
		}
		if quiet {
			continue
		}
		if isDryRun {
			tprint("%s can move to %s", result.Slug, destinationSlug)
		} else {
			tprint("%s moved to %s", result.Slug, destinationSlug)
		}
		if len(result.MovedLinkSlugs) > 0 {
			tprint("  links moved: %v", result.MovedLinkSlugs)
		}
		if result.Resolving {
			tprint("  re-validating against %s's triggers", destinationSlug)
		}
	}
	if failures > 0 {
		if isDryRun {
			return fmt.Errorf("%d unit(s) cannot move", failures)
		}
		return fmt.Errorf("%d unit(s) did not move", failures)
	}
	return nil
}
