// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a target or multiple targets",
	Long: getCommandHelp(`Delete a target or multiple targets using bulk operations.

Single target delete:
`+"```"+`
  cub target delete my-target
`+"```"+`

Bulk delete with --where:

Delete multiple targets at once based on search criteria.

Examples:
`+"```"+`
  # Delete targets for specific toolchain
  cub target delete --where "ToolchainType = 'Kubernetes/YAML'"

  # Delete targets across all spaces (requires --space "*")
  cub target delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific targets by slug
  cub target delete --target my-target,another-target
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        targetDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	targetDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(targetDeleteCmd)
	enableWhereFlag(targetDeleteCmd)
	enableFilterFlag(targetDeleteCmd)
	targetDeleteCmd.Flags().StringSliceVar(&targetDeleteIdentifiers, "target", []string{}, "target specific targets by slug or UUID for bulk delete (can be repeated or comma-separated)")
	targetCmd.AddCommand(targetDeleteCmd)
}

func checkTargetDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode: no positional args
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --target and --where flags
		if len(targetDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--target and --where flags are mutually exclusive"))
		}

	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single target delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(targetDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --target can only be specified with no positional arguments"))
		}
	}

	return isBulkDeleteMode
}

func buildWhereClauseFromTargets(targetIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(targetIds, "TargetID", "Slug")
}

func runBulkTargetDelete() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from target identifiers or use provided where clause
	var effectiveWhere string
	if len(targetDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromTargets(targetDeleteIdentifiers)
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
	include := "SpaceID,BridgeWorkerID"
	params := &goclientnew.BulkDeleteTargetsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteTargetsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkTargetDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func targetDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkTargetDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkTargetDelete()
	}

	// Single target delete logic
	targetDetails, err := apiGetTargetFromSlug(args[0], selectedSpaceID, "*") // get all fields for now
	if err != nil {
		return err
	}

	deleteRes, err := cubClientNew.DeleteTargetWithResponse(ctx, uuid.MustParse(selectedSpaceID), targetDetails.Target.TargetID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("target", args[0], targetDetails.Target.TargetID.String(), deleteRes)
	return nil
}

func handleBulkTargetDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "target", operationName, contextInfo)
}
