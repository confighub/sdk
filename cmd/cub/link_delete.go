// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var linkDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a link or multiple links",
	Long: getCommandHelp(`Delete a link or multiple links using bulk operations.

Single link delete:
`+"```"+`
  cub link delete my-link
`+"```"+`

Bulk delete with --where:

Delete multiple links at once based on search criteria.

Examples:
`+"```"+`
  # Delete all links to a specific unit
  cub link delete --where "ToUnitID = 'unit-uuid'"

  # Delete cross-space links
  cub link delete --where "ToSpaceID != SpaceID"

  # Delete links across all spaces (requires --space "*")
  cub link delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific links by slug
  cub link delete --link my-link,another-link
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        linkDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	linkDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(linkDeleteCmd)
	enableWaitFlag(linkDeleteCmd)
	enableWhereFlag(linkDeleteCmd)
	enableFilterFlag(linkDeleteCmd)
	linkDeleteCmd.Flags().StringSliceVar(&linkDeleteIdentifiers, "link", []string{}, "target specific links by slug or UUID for bulk delete (can be repeated or comma-separated)")
	linkCmd.AddCommand(linkDeleteCmd)
}

func handleBulkLinkDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "link", operationName, contextInfo)
}

func checkLinkDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --invocation and --where flags
		if len(linkDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--link and --where flags are mutually exclusive"))
		}

	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single invocation delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(linkDeleteIdentifiers) > 0 {
			failOnError(errors.New("--filter, --where, and --link flags can only be used in bulk mode (without positional arguments)"))
		}
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkLinkDelete() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	var effectiveWhere string
	if len(linkDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromLinks(linkDeleteIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build bulk delete parameters
	include := "SpaceID,FromUnitID,ToUnitID,ToSpaceID"
	params := &goclientnew.BulkDeleteLinksParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}
	if contains != "" {
		params.Contains = &contains
	}

	// If wait is requested, we need to fetch the links first to get their FromUnitIDs and SpaceIDs
	var fromUnits []struct {
		UnitID  string
		SpaceID string
	}
	if wait {
		// Use apiSearchLinks to fetch links that will be deleted
		links, err := apiSearchLinks(effectiveWhere, "*", filterID)
		if err != nil {
			return err
		}

		// Extract unique FromUnitID/SpaceID pairs
		uniqueUnits := make(map[string]bool)
		for _, link := range links {
			unitID := link.Link.FromUnitID.String()
			spaceID := link.Link.SpaceID.String()
			key := unitID + ":" + spaceID
			if !uniqueUnits[key] {
				uniqueUnits[key] = true
				fromUnits = append(fromUnits, struct {
					UnitID  string
					SpaceID string
				}{
					UnitID:  unitID,
					SpaceID: spaceID,
				})
			}
		}
	}

	res, err := cubClientNew.BulkDeleteLinksWithResponse(ctx, params)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}

	// Wait for triggers BEFORE displaying results (for successful deletes)
	if wait && len(fromUnits) > 0 && res.StatusCode() == 200 {
		hasAlternativeOutput := jsonOutput || jq != ""
		if !quiet && !hasAlternativeOutput {
			tprintRaw("Awaiting triggers...")
		}
		// Await triggers on each affected from unit
		for _, unit := range fromUnits {
			unitDetails, err := apiGetUnitInSpace(unit.UnitID, unit.SpaceID, "*") // get all fields for now
			if err != nil {
				// Continue with other units if one fails
				continue
			}
			_ = awaitTriggersRemoval(unitDetails)
		}
	}

	// Handle the response
	return handleBulkLinkDeleteResponse(res.JSON200, res.JSON207, res.StatusCode(), "delete", effectiveWhere)
}

func linkDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkLinkDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkLinkDelete()
	}

	// Single link delete logic
	if len(args) != 1 {
		return fmt.Errorf("specify a link slug/id for single delete, or use --where/--link for bulk delete")
	}

	linkDetails, err := apiGetLinkFromSlug(args[0], "") // default select is fine
	if err != nil {
		return err
	}

	deleteRes, err := cubClientNew.DeleteLinkWithResponse(ctx, uuid.MustParse(selectedSpaceID), linkDetails.LinkID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("link", args[0], linkDetails.LinkID.String(), deleteRes)
	if wait {
		if !quiet {
			tprint("Awaiting triggers...")
		}
		fromUnitID := linkDetails.FromUnitID
		unitDetails, err := apiGetUnitInSpace(fromUnitID.String(), linkDetails.SpaceID.String(), "*") // get all fields for now
		if err != nil {
			return err
		}
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	return nil
}
