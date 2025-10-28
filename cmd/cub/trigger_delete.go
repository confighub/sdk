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

var triggerDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a trigger or multiple triggers",
	Long: getCommandHelp(`Delete a trigger or multiple triggers using bulk operations.

Single trigger delete:
`+"```"+`
  cub trigger delete my-trigger
`+"```"+`

Bulk delete with --where:

Delete multiple triggers at once based on search criteria.

Examples:
`+"```"+`
  # Delete all disabled triggers
  cub trigger delete --where "Disabled = true"

  # Delete triggers for specific function
  cub trigger delete --where "FunctionName = 'validate'"

  # Delete triggers across all spaces (requires --space "*")
  cub trigger delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific triggers by slug
  cub trigger delete --trigger my-trigger,another-trigger
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        triggerDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	triggerDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(triggerDeleteCmd)
	enableWhereFlag(triggerDeleteCmd)
	enableFilterFlag(triggerDeleteCmd)
	triggerDeleteCmd.Flags().StringSliceVar(&triggerDeleteIdentifiers, "trigger", []string{}, "target specific triggers by slug or UUID for bulk delete (can be repeated or comma-separated)")
	triggerCmd.AddCommand(triggerDeleteCmd)
}

func checkTriggerDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode: no positional args
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --trigger and --where flags
		if len(triggerDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--trigger and --where flags are mutually exclusive"))
		}

	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single trigger delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(triggerDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --trigger can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkTriggerDelete() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from trigger identifiers or use provided where clause
	var effectiveWhere string
	if len(triggerDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromTriggers(triggerDeleteIdentifiers)
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
	params := &goclientnew.BulkDeleteTriggersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteTriggersWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkTriggerDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func triggerDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkTriggerDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkTriggerDelete()
	}

	// Single trigger delete logic
	triggerDetails, err := apiGetTriggerFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteTriggerWithResponse(ctx, uuid.MustParse(selectedSpaceID), triggerDetails.Trigger.TriggerID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("trigger", args[0], triggerDetails.Trigger.TriggerID.String(), deleteRes)
	return nil
}

func handleBulkTriggerDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "trigger", operationName, contextInfo)
}
