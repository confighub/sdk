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

var invocationDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete an invocation or multiple invocations",
	Long: getCommandHelp(`Delete an invocation or multiple invocations using bulk operations.

Single invocation delete:
`+"```"+`
  cub invocation delete my-invocation
`+"```"+`

Bulk delete with --where:

Delete multiple invocations at once based on search criteria.

Examples:
`+"```"+`
  # Delete all invocations for a specific function
  cub invocation delete --where "FunctionName = 'validate'"

  # Delete invocations for specific toolchain
  cub invocation delete --where "ToolchainType = 'Kubernetes/YAML'"

  # Delete invocations across all spaces (requires --space "*")
  cub invocation delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific invocations by slug
  cub invocation delete --invocation my-invocation,another-invocation
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        invocationDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	invocationDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(invocationDeleteCmd)
	enableWhereFlag(invocationDeleteCmd)
	enableFilterFlag(invocationDeleteCmd)
	invocationDeleteCmd.Flags().StringSliceVar(&invocationDeleteIdentifiers, "invocation", []string{}, "target specific invocations by slug or UUID for bulk delete (can be repeated or comma-separated)")
	invocationCmd.AddCommand(invocationDeleteCmd)
}

func checkInvocationDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode: no positional args
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --invocation and --where flags
		if len(invocationDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--invocation and --where flags are mutually exclusive"))
		}

	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single invocation delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(invocationDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --invocation can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkInvocationDelete() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from invocation identifiers or use provided where clause
	var effectiveWhere string
	if len(invocationDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromInvocations(invocationDeleteIdentifiers)
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
	params := &goclientnew.BulkDeleteInvocationsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteInvocationsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkInvocationDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func invocationDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkInvocationDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkInvocationDelete()
	}

	// Single invocation delete logic
	invocationDetails, err := apiGetInvocationFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteInvocationWithResponse(ctx, uuid.MustParse(selectedSpaceID), invocationDetails.InvocationID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("invocation", args[0], invocationDetails.InvocationID.String(), deleteRes)
	return nil
}

func handleBulkInvocationDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "invocation", operationName, contextInfo)
}
