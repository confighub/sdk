// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changesetDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a changeset or multiple changesets",
	Long: getCommandHelp(`Delete a changeset or multiple changesets using bulk operations.

Single changeset delete:
`+"```"+`
  cub changeset delete my-changeset
`+"```"+`

Bulk delete with --where:

Delete multiple changesets at once based on search criteria.

Examples:
`+"```"+`
  # Delete all changesets created before a specific date
  cub changeset delete --where "CreatedAt < '2024-01-01'"

  # Delete changesets with specific descriptions
  cub changeset delete --where "Description LIKE '%deprecated%'"

  # Delete changesets across all spaces (requires --space "*")
  cub changeset delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific changesets by slug
  cub changeset delete --changeset old-changeset,deprecated-changeset
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        changesetDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	changesetDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(changesetDeleteCmd)
	enableWhereFlag(changesetDeleteCmd)
	enableFilterFlag(changesetDeleteCmd)
	changesetDeleteCmd.Flags().StringSliceVar(&changesetDeleteIdentifiers, "changeset", []string{}, "target specific changesets by slug or UUID for bulk delete (can be repeated or comma-separated)")
	changesetCmd.AddCommand(changesetDeleteCmd)
}

func checkChangeSetDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode: no positional args
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --changeset and --where flags
		if len(changesetDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--changeset and --where flags are mutually exclusive"))
		}

	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single changeset delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(changesetDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --changeset can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkChangeSetDelete() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from changeset identifiers or use provided where clause
	var effectiveWhere string
	if len(changesetDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromChangeSets(changesetDeleteIdentifiers)
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
	include := "SpaceID,FilterID,StartTagID,EndTagID"
	params := &goclientnew.BulkDeleteChangeSetsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteChangeSetsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkChangeSetDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func changesetDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkChangeSetDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkChangeSetDelete()
	}

	// Single changeset delete logic
	changesetDetails, err := apiGetChangeSetFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteChangeSetWithResponse(ctx, uuid.MustParse(selectedSpaceID), changesetDetails.ChangeSetID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("changeset", args[0], changesetDetails.ChangeSetID.String(), deleteRes)
	return nil
}

func handleBulkChangeSetDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "changeset", operationName, contextInfo)
}
