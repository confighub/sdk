// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var triggerDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a trigger or multiple triggers",
	Long: getCommandHelp(`Delete a trigger or multiple triggers using bulk operations.

Deleting a trigger takes back the guards it set on the paths it last wrote, in every unit it
wrote, since nothing maintains them any more. What it wrote stays.

--archive keeps the trigger rather than deleting it: it stays, hidden with the reason Archived and
named by a slug made from its ID, with its backing unit archived with it, and runs nowhere until it
is unarchived with cub trigger unarchive. Its guards are taken back, as for a delete. Delete an
archived trigger to delete it for good.

Single trigger delete:
`+"```"+`
  cub trigger delete my-trigger
`+"```"+`

Bulk delete with --where:

Delete multiple triggers at once based on search criteria.

Examples:
`+"```"+`
  # Delete all disabled triggers
  cub trigger delete --space my-space --where "Disabled = true"

  # Delete triggers for specific function
  cub trigger delete --space my-space --where "FunctionName = 'cel-validate'"

  # Delete triggers across the organization: without --space, --where spans every space
  cub trigger delete --where "Labels.cleanup = 'true'"

  # Delete the disabled triggers of a pack in a policy space
  cub trigger delete --space confighub-system --pack scheduling --where "Disabled = true"

  # Delete specific triggers by slug
  cub trigger delete --space my-space --trigger my-trigger,another-trigger
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
	addPruneFlag(triggerDeleteCmd, "Trigger")
	addArchiveFlag(triggerDeleteCmd)
	enableWhereFlag(triggerDeleteCmd)
	enableFilterFlag(triggerDeleteCmd)
	addTriggerPackFlag(triggerDeleteCmd)
	triggerDeleteCmd.Flags().StringSliceVar(&triggerDeleteIdentifiers, "trigger", []string{}, "target specific triggers by slug or UUID for bulk delete (can be repeated or comma-separated)")
	triggerCmd.AddCommand(triggerDeleteCmd)
}

func checkTriggerDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode: no positional args
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --trigger and --where flags
		if len(triggerDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--trigger is mutually exclusive with --where and --pack"))
		}

	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single trigger delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(triggerDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, --pack, or --trigger can only be specified with no positional arguments"))
		}
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
	include := "SpaceID"
	params := &goclientnew.BulkDeleteTriggersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	params.IncludeHidden = includeHiddenParam()
	params.FromBackingUnits = fromBackingUnitsParam()
	params.Archive = archiveParam()
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteTriggersWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkTriggerDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), deleteOperationName(), effectiveWhere)
}

func triggerDeleteCmdRun(cmd *cobra.Command, args []string) error {
	if err := applyTriggerPackFlag(); err != nil {
		return err
	}
	isBulkDeleteMode := checkTriggerDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkTriggerDelete()
	}

	// Single trigger delete logic
	triggerDetails, err := resolveTrigger(args[0], selectedSpaceID, "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteTriggerWithResponse(ctx, triggerDetails.Trigger.SpaceID, triggerDetails.Trigger.TriggerID,
		&goclientnew.DeleteTriggerParams{Archive: archiveParam()})
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteOrArchiveResults("trigger", args[0], triggerDetails.Trigger.TriggerID.String(), deleteRes)
	return nil
}

func handleBulkTriggerDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "trigger", operationName, contextInfo)
}
