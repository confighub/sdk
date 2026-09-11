// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var changeworkflowDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a change workflow or multiple change workflows",
	Long: getCommandHelp(`Delete a change workflow or multiple change workflows using bulk operations.

A change order that named the workflow keeps the annotation naming it, so deleting a workflow a
rollout is still moving under leaves that rollout with nothing to advance through. Abort the change
order, or keep the workflow until it is done.

Single change workflow delete:
`+"```"+`
  cub changeworkflow delete --space workflows myapp-main-line
`+"```"+`

Bulk delete with --where:

Delete multiple change workflows at once based on search criteria.

Examples:
`+"```"+`
  # Delete the workflows of a retired component's spaces
  cub changeworkflow delete --space "*" --where "Labels.Component = 'legacy-app'"

  # Delete specific change workflows by slug
  cub changeworkflow delete --changeworkflow old-main-line,deprecated-main-line
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        changeworkflowDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	changeworkflowDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(changeworkflowDeleteCmd)
	enableWhereFlag(changeworkflowDeleteCmd)
	enableFilterFlag(changeworkflowDeleteCmd)
	changeworkflowDeleteCmd.Flags().StringSliceVar(&changeworkflowDeleteIdentifiers, "changeworkflow", []string{}, "target specific change workflows by slug or UUID for bulk delete (can be repeated or comma-separated)")
	changeworkflowCmd.AddCommand(changeworkflowDeleteCmd)
}

func checkChangeWorkflowDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args)
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --changeworkflow and --where flags
		if len(changeworkflowDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--changeworkflow and --where flags are mutually exclusive"))
		}
	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single change workflow delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(changeworkflowDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --changeworkflow can only be specified with no positional arguments"))
		}
	}

	return isBulkDeleteMode
}

func runBulkChangeWorkflowDelete() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from changeworkflow identifiers or use provided where clause
	var effectiveWhere string
	if len(changeworkflowDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromChangeWorkflows(changeworkflowDeleteIdentifiers)
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
	params := &goclientnew.BulkDeleteChangeWorkflowsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteChangeWorkflowsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkChangeWorkflowDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func changeworkflowDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkChangeWorkflowDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkChangeWorkflowDelete()
	}

	// Single change workflow delete logic
	changeWorkflowDetails, err := resolveChangeWorkflow(args[0], selectedSpaceID, "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteChangeWorkflowWithResponse(ctx,
		changeWorkflowDetails.ChangeWorkflow.SpaceID, changeWorkflowDetails.ChangeWorkflow.ChangeWorkflowID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("changeworkflow", args[0],
		changeWorkflowDetails.ChangeWorkflow.ChangeWorkflowID.String(), deleteRes)
	return nil
}

func handleBulkChangeWorkflowDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "changeworkflow", operationName, contextInfo)
}
